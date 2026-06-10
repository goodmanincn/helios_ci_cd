// Package engine — DAG 调度引擎核心 (M2 E2.2)。
//
// 本文件 (dag.go) 只做"图论":
//   - BuildDAG: 把 dsl.Pipeline 的 stages + needs 变成有向图 (节点 = stage id)
//   - DetectCycle: 找环 (DFS 染色, 同一环只报一次, 错误带链路提示)
//   - TopologicalSort: 卡恩算法分层 → [][]NodeID, 每一层内部可并行
//
// 输入: 一个已经经过 dsl.ValidateRaw 的 Pipeline (假设 stage id 唯一 / needs 合法)。
// 若 caller 没校验先就上来 BuildDAG, 我们仍然兜底:
//   - 引用不存在的 needs 当成 dangling 边记录, 不让它污染拓扑结果
//   - DetectCycle 永远独立可调
//
// 这里没接 dsl 包 (除了 dsl.Pipeline 类型) 是有意的: engine 不关心 yaml, 只吃 struct,
// 便于 worker 端单测或 CLI dry-run 调用 (后两者也不会重新 unmarshal yaml)。
package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/helios-cicd/helios/api/pkg/dsl"
)

// NodeID 即 stage.ID (字符串, dsl 已经保证语法/唯一性)。
type NodeID = string

// Node DAG 节点元信息. Stage 指针指向原 Pipeline.Stages 中对应项 (浅引用),
// 不复制是为了 caller 修改 Stage (e.g. matrix 展开后回写) 还能看见。
type Node struct {
	ID    NodeID
	Stage *dsl.Stage
	// In/Out 邻接表 (NodeID 已经在 nodes map 里, 不会包 nil)
	In  []NodeID // 入边 (= stage.Needs, 但只包含存在的)
	Out []NodeID // 出边 (反向, 谁 needs 我)
}

// DAG 构建结果。Nodes 是 id → Node 的稳定 map; Order 是 stages 数组原始顺序。
//
// Dangling 是 BuildDAG 兜底: 当 needs 引用不存在的 stage 时, 边记下来给上层
// (虽然 dsl.ValidateRaw 已经拦, 但 caller 跳过校验时不能让 BuildDAG 静默丢边)。
type DAG struct {
	Nodes    map[NodeID]*Node
	Order    []NodeID                       // stages 数组原始顺序 (拓扑排序 tiebreak 用)
	Dangling map[NodeID][]NodeID            // stageID → 引用了但找不到的 needs 列表
}

// BuildDAG 从 pipeline 构图。不做任何环/拓扑判断, 那是 caller 单独调的事。
//
// 复杂度: O(stages + edges)
func BuildDAG(p *dsl.Pipeline) *DAG {
	if p == nil {
		return &DAG{Nodes: map[NodeID]*Node{}, Order: nil, Dangling: map[NodeID][]NodeID{}}
	}
	dag := &DAG{
		Nodes:    make(map[NodeID]*Node, len(p.Stages)),
		Order:    make([]NodeID, 0, len(p.Stages)),
		Dangling: map[NodeID][]NodeID{},
	}
	// 1) 建节点 (跳过空 id 防御性)
	for i := range p.Stages {
		s := &p.Stages[i]
		if s.ID == "" {
			continue
		}
		if _, dup := dag.Nodes[s.ID]; dup {
			// 已经在前一个位置存在; dsl.ValidateRaw 会单独报 dup, 这里保留首个
			continue
		}
		dag.Nodes[s.ID] = &Node{ID: s.ID, Stage: s}
		dag.Order = append(dag.Order, s.ID)
	}
	// 2) 连边: needs 是入边 (上游 → 我); 反向也存 Out
	for _, id := range dag.Order {
		n := dag.Nodes[id]
		for _, dep := range n.Stage.Needs {
			if dep == id {
				// 自环; dsl.ValidateRaw 已拦, 此处跳过不要把自己加 In
				continue
			}
			up, ok := dag.Nodes[dep]
			if !ok {
				dag.Dangling[id] = append(dag.Dangling[id], dep)
				continue
			}
			n.In = append(n.In, dep)
			up.Out = append(up.Out, id)
		}
	}
	return dag
}

// CycleError 描述一个具体的环及参与节点 (按出现顺序排好, 末尾重复首节点表明闭合)。
//
//   Path: [a, b, c, a]  →  含义: a → b → c → a
//
// 只持有 stage id; caller 想关联 line:col 自行从 dsl.ParseResult 反查。
type CycleError struct {
	Path []NodeID
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("cycle: %s", strings.Join(e.Path, " → "))
}

// DetectCycles DFS 染色法, 返所有最小环 (同一环只报一次, 互不相同的环各报一次)。
//
// 返回空 slice 表示无环 (符合 DAG 定义)。
// 实现细节: 经典 white/gray/black + recursion stack 重建路径, 报到第一个 gray 命中
// 的节点为止 (避免把环外的 prefix 也带上)。
func (d *DAG) DetectCycles() []*CycleError {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[NodeID]int, len(d.Nodes))
	var stack []NodeID
	var cycles []*CycleError
	reported := map[string]bool{}

	// 用排序后的 id 起 DFS, 保证多次运行结果稳定
	roots := make([]NodeID, 0, len(d.Nodes))
	for id := range d.Nodes {
		roots = append(roots, id)
	}
	sort.Strings(roots)

	var dfs func(id NodeID)
	dfs = func(id NodeID) {
		color[id] = gray
		stack = append(stack, id)
		// 出边 (n.Out 指向下游)
		nexts := append([]NodeID(nil), d.Nodes[id].Out...)
		sort.Strings(nexts)
		for _, nxt := range nexts {
			switch color[nxt] {
			case gray:
				// 闭合: 找 stack 里 nxt 的位置, 切片 + 末尾重复
				idx := -1
				for k, x := range stack {
					if x == nxt {
						idx = k
						break
					}
				}
				if idx < 0 {
					// 防御: 不应发生 (gray 必然在 stack)
					idx = len(stack) - 1
				}
				path := append([]NodeID(nil), stack[idx:]...)
				path = append(path, nxt)
				key := strings.Join(canonicalCycle(path[:len(path)-1]), ">")
				if !reported[key] {
					reported[key] = true
					cycles = append(cycles, &CycleError{Path: path})
				}
			case white:
				dfs(nxt)
			}
		}
		color[id] = black
		stack = stack[:len(stack)-1]
	}

	for _, r := range roots {
		if color[r] == white {
			dfs(r)
		}
	}
	return cycles
}

// canonicalCycle 把环列表旋转到字典最小元素起头, 便于去重
//   [b, c, a]  →  [a, b, c]
//   [c, a, b]  →  [a, b, c]
func canonicalCycle(path []NodeID) []NodeID {
	if len(path) == 0 {
		return path
	}
	minIdx := 0
	for i := 1; i < len(path); i++ {
		if path[i] < path[minIdx] {
			minIdx = i
		}
	}
	out := make([]NodeID, len(path))
	for i := range path {
		out[i] = path[(minIdx+i)%len(path)]
	}
	return out
}

// TopologicalSort 卡恩 (Kahn) 算法分层。
//
// 返回:
//   - layers: [[a, b], [c, d], [e]] — 同一层内部可并行 (依赖都已 ready)
//   - err: 若图含环, 返第一个发现的 *CycleError (调度器不应继续)
//
// 稳定性: 同一层内按 dag.Order (即 stages 数组原始顺序) 排序, 不按 id 字母,
// 这样 caller 视觉上能跟原 YAML 对得上。
func (d *DAG) TopologicalSort() ([][]NodeID, error) {
	if cycles := d.DetectCycles(); len(cycles) > 0 {
		return nil, cycles[0]
	}

	inDeg := make(map[NodeID]int, len(d.Nodes))
	for id, n := range d.Nodes {
		inDeg[id] = len(n.In)
	}

	// 当前 ready (入度为 0) 的节点, 按 Order 顺序
	pickReady := func() []NodeID {
		var ready []NodeID
		for _, id := range d.Order {
			if d := inDeg[id]; d == 0 {
				ready = append(ready, id)
			}
		}
		return ready
	}

	var layers [][]NodeID
	visited := make(map[NodeID]bool, len(d.Nodes))
	for {
		ready := pickReady()
		// 过滤已访问 (入度 0 但已经被吃了)
		fresh := ready[:0]
		for _, id := range ready {
			if !visited[id] {
				fresh = append(fresh, id)
			}
		}
		if len(fresh) == 0 {
			break
		}
		layers = append(layers, fresh)
		// 吃掉这一层, 削减下游入度
		for _, id := range fresh {
			visited[id] = true
			for _, dn := range d.Nodes[id].Out {
				inDeg[dn]--
			}
		}
	}

	// 若 visited < nodes, 说明有环但 DetectCycles 漏了 (理论不应发生; 防御性返错)
	if len(visited) != len(d.Nodes) {
		var leftover []NodeID
		for id := range d.Nodes {
			if !visited[id] {
				leftover = append(leftover, id)
			}
		}
		sort.Strings(leftover)
		return layers, fmt.Errorf("topological sort incomplete; %d unvisited (likely cycle): %v",
			len(leftover), leftover)
	}
	return layers, nil
}
