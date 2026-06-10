// Package engine — scheduler.go: DAG 状态机推进 (T2.2.3, 纯逻辑层)。
//
// 设计取舍:
//   - 本文件只做"状态机": 维护每个 node 的 Status (pending/ready/running/success/failed/skipped/canceled),
//     提供 NextReady() 拿当前可执行节点 + Complete() 报完成后推进下游。
//   - 不直接调 Asynq / 不写 DB; 由 caller (worker 端 scheduler 进程) 包一层做 IO。
//     好处: 这里全部纯函数化 + 大量单测, 真正 IO 故障留在 IO 层定位。
//   - if: always()/failure() 暂时只识别这两个具体字符串 (不接 expr 引擎); E2.3 接入后改成调
//     expr.Eval(node.Stage.If, ctx). 当前覆盖最常见场景, 不算完美但够推进 M2 端到端。
//
// 状态机:
//
//   pending ─(deps all success)─→ ready ─(NextReady picked)─→ running
//      │           │
//      │           └─(deps 含 failed 且 if!=always/failure)─→ skipped
//      │
//      └─(任意时刻 Cancel)─→ canceled
//
//   running ─(Complete OK)─→ success
//   running ─(Complete fail)─→ failed
//
// 终态: success / failed / skipped / canceled (不再变化)
//
// 失败传播: 一个节点 failed, 下游若 if=="always()" 仍然 ready (理解为强制运行),
//   若 if=="failure()" 也仍然 ready (理解为故障后才跑, 通知节点); 其它 → skipped。
package engine

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// NodeStatus 节点运行状态。
type NodeStatus string

const (
	StatusPending  NodeStatus = "pending"
	StatusReady    NodeStatus = "ready"
	StatusRunning  NodeStatus = "running"
	StatusSuccess  NodeStatus = "success"
	StatusFailed   NodeStatus = "failed"
	StatusSkipped  NodeStatus = "skipped"
	StatusCanceled NodeStatus = "canceled"
)

// IsTerminal 终态判定 (后续不再变化)。
func (s NodeStatus) IsTerminal() bool {
	switch s {
	case StatusSuccess, StatusFailed, StatusSkipped, StatusCanceled:
		return true
	}
	return false
}

// Scheduler 调度状态机。线程安全 (用 mu 保护内部状态)。
//
// 典型用法:
//
//   sch := engine.NewScheduler(dag)
//   for {
//     ready := sch.NextReady()
//     if len(ready) == 0 {
//       if sch.Done() { break }
//       // 阻塞等 Complete 被调用 (caller 用 channel/cond)
//       waitForCompletion()
//       continue
//     }
//     for _, id := range ready {
//       go runStageThenCallComplete(id)  // caller 的 IO 层
//     }
//   }
type Scheduler struct {
	mu     sync.Mutex
	dag    *DAG
	status map[NodeID]NodeStatus
	// 已 NextReady 摘出但还没 Complete 的节点 (避免重复派发)
	dispatched map[NodeID]bool
}

// NewScheduler 初始化。所有节点起始 pending; 没有依赖的进入候选。
func NewScheduler(dag *DAG) *Scheduler {
	st := make(map[NodeID]NodeStatus, len(dag.Nodes))
	for id := range dag.Nodes {
		st[id] = StatusPending
	}
	return &Scheduler{
		dag:        dag,
		status:     st,
		dispatched: map[NodeID]bool{},
	}
}

// Status 取节点当前状态 (未知节点返 StatusPending, 也算合理默认)。
func (s *Scheduler) Status(id NodeID) NodeStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status[id]
}

// Snapshot 全状态快照 (调试 / API 拉)。
func (s *Scheduler) Snapshot() map[NodeID]NodeStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[NodeID]NodeStatus, len(s.status))
	for k, v := range s.status {
		out[k] = v
	}
	return out
}

// NextReady 返回当前所有可派发的节点 (状态 pending + 依赖满足),
// 同时把它们置为 running 并记到 dispatched. 不会重复返同一个。
//
// "依赖满足" = 所有 needs 都是 success, 或 (needs 含 failed 且本节点 if=always()/failure()).
// 依赖含 canceled/skipped 时一律视为本节点 skipped (除非 if=always 也强制运行).
//
// 节点排序: 按 dag.Order (原始 yaml 顺序), 让 caller 看到的派发顺序稳定。
func (s *Scheduler) NextReady() []NodeID {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 在锁内, 重新计算每个 pending 节点的"应转入什么状态"
	var ready []NodeID
	for _, id := range s.dag.Order {
		if s.status[id] != StatusPending {
			continue
		}
		if s.dispatched[id] {
			continue
		}
		next := s.computeReadyOrSkip(id)
		switch next {
		case StatusReady:
			s.status[id] = StatusRunning
			s.dispatched[id] = true
			ready = append(ready, id)
		case StatusSkipped:
			s.status[id] = StatusSkipped
			// 链式推进: 这个 skipped 可能让别人也 skip; 不在本函数里循环,
			// 下一次 NextReady 调用时自然处理 (避免一次调用做太多)
		}
	}
	return ready
}

// computeReadyOrSkip 对 pending 节点判一次依赖, 返本次状态 (StatusReady / StatusSkipped / StatusPending)。
// 不持有锁的责任在 caller (NextReady 已经加了)。
func (s *Scheduler) computeReadyOrSkip(id NodeID) NodeStatus {
	n := s.dag.Nodes[id]
	if n == nil {
		return StatusPending
	}
	if len(n.In) == 0 {
		// 无依赖: 直接 ready (如果有 if=failure, 没有上游失败可言, 也算 skip)
		// 现实中 "if: failure()" 通常需要至少一个上游, 没上游就 skip
		if strings.Contains(n.Stage.If, "failure()") {
			return StatusSkipped
		}
		return StatusReady
	}

	var hasFailed, hasSkipped, hasCanceled, allDone bool
	allDone = true
	for _, dep := range n.In {
		ds := s.status[dep]
		switch ds {
		case StatusSuccess:
			// ok
		case StatusFailed:
			hasFailed = true
		case StatusSkipped:
			hasSkipped = true
		case StatusCanceled:
			hasCanceled = true
		default:
			// pending/ready/running → 依赖未完
			allDone = false
		}
	}
	if !allDone {
		return StatusPending
	}

	hasIf := n.Stage.If != ""
	always := hasIf && strings.Contains(n.Stage.If, "always()")
	failureOnly := hasIf && strings.Contains(n.Stage.If, "failure()")

	// 上游有 canceled: 默认 skip; if=always 才跑 (failure 不 cover canceled)
	if hasCanceled && !always {
		return StatusSkipped
	}
	// 上游有 failed: 默认 skip; if=always / failure 才跑
	if hasFailed {
		if always || failureOnly {
			return StatusReady
		}
		return StatusSkipped
	}
	// 上游有 skipped: 视为非成功, 等同 failure 路径 (skip 除非 always)
	if hasSkipped && !always {
		return StatusSkipped
	}
	// 全 success (可能含 skipped 但带 always) → ready;
	// 但 if=failure_only 且无 failed → skip (failure-only 节点没失败要救)
	if failureOnly && !hasFailed {
		return StatusSkipped
	}
	return StatusReady
}

// Complete 报告节点完成 (success / failed / canceled), caller 在 stage 跑完后调。
// 不允许已 terminal 的节点再 complete (返 error 但不 panic)。
//
// 完成后内部 *不* 立即递推下游 (那是 NextReady 的活); 这样接口边界清楚: caller
// 调 Complete 拿到 nil 表示状态机已吸收, 然后再调 NextReady 拿新一批 ready 节点。
func (s *Scheduler) Complete(id NodeID, result NodeStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.status[id]
	if !ok {
		return fmt.Errorf("unknown node %q", id)
	}
	if cur.IsTerminal() {
		return fmt.Errorf("node %q already terminal: %s", id, cur)
	}
	switch result {
	case StatusSuccess, StatusFailed, StatusCanceled:
		s.status[id] = result
		delete(s.dispatched, id)
		return nil
	default:
		return fmt.Errorf("invalid completion result %q (allowed: success/failed/canceled)", result)
	}
}

// CancelAll 把所有非终态节点置 canceled (用户取消整个 run / fatal 错误兜底)。
func (s *Scheduler) CancelAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, st := range s.status {
		if !st.IsTerminal() {
			s.status[id] = StatusCanceled
			delete(s.dispatched, id)
		}
	}
}

// Done 所有节点终态 → true。
func (s *Scheduler) Done() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.status {
		if !st.IsTerminal() {
			return false
		}
	}
	return true
}

// Outcome 整体结果汇总 (Done() 后调; 任意 failed → failed, 任意 canceled → canceled,
// 否则 success). 用于把 run.status 写回 DB。
func (s *Scheduler) Outcome() NodeStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	var canceled, failed, skipped bool
	for _, st := range s.status {
		switch st {
		case StatusFailed:
			failed = true
		case StatusCanceled:
			canceled = true
		case StatusSkipped:
			skipped = true
		}
	}
	switch {
	case failed:
		return StatusFailed
	case canceled:
		return StatusCanceled
	case skipped && len(s.status) == 1:
		// 单 stage 全 skip 也算 skip; 多 stage 时 skip 不抢
		return StatusSkipped
	default:
		return StatusSuccess
	}
}

// String 调试用快照字符串 (按 id 排序)。
func (s *Scheduler) String() string {
	snap := s.Snapshot()
	keys := make([]string, 0, len(snap))
	for k := range snap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("Scheduler{")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s=%s", k, snap[k])
	}
	b.WriteString("}")
	return b.String()
}
