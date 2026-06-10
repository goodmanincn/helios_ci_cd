// scheduler_test.go — Scheduler 状态机 + 失败传播 + 取消 + if always/failure 覆盖。
package engine

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/helios-cicd/helios/api/pkg/dsl"
)

// ---- 基本推进 ----

func TestScheduler_LinearSuccess(t *testing.T) {
	// a → b → c, 全部 success
	p := mkPipeline(mkStage("a"), mkStage("b", "a"), mkStage("c", "b"))
	d := BuildDAG(p)
	sch := NewScheduler(d)

	// 第一轮: 只 a ready
	r := sch.NextReady()
	require.Equal(t, []NodeID{"a"}, r)
	require.Equal(t, StatusRunning, sch.Status("a"))
	require.Equal(t, StatusPending, sch.Status("b"))

	// a 完成 → b ready
	require.NoError(t, sch.Complete("a", StatusSuccess))
	require.Equal(t, []NodeID{"b"}, sch.NextReady())

	// b 完成 → c ready
	require.NoError(t, sch.Complete("b", StatusSuccess))
	require.Equal(t, []NodeID{"c"}, sch.NextReady())

	// c 完成 → done
	require.NoError(t, sch.Complete("c", StatusSuccess))
	require.True(t, sch.Done())
	require.Equal(t, StatusSuccess, sch.Outcome())
}

func TestScheduler_ParallelFanOut(t *testing.T) {
	// a → {b, c}: 第一轮 a, 第二轮 b 和 c 并行 (同一次 NextReady 返回俩)
	p := mkPipeline(mkStage("a"), mkStage("b", "a"), mkStage("c", "a"))
	d := BuildDAG(p)
	sch := NewScheduler(d)

	sch.NextReady()
	sch.Complete("a", StatusSuccess)
	r := sch.NextReady()
	sort.Strings(r)
	require.Equal(t, []NodeID{"b", "c"}, r, "fan-out 并行")
}

// ---- 失败传播 ----

func TestScheduler_FailPropagatesSkip(t *testing.T) {
	// a → b → c, b 失败 → c 被 skip
	p := mkPipeline(mkStage("a"), mkStage("b", "a"), mkStage("c", "b"))
	d := BuildDAG(p)
	sch := NewScheduler(d)

	sch.NextReady() // a
	sch.Complete("a", StatusSuccess)
	sch.NextReady() // b
	sch.Complete("b", StatusFailed)

	// c 应该 skip, 而不是 ready
	r := sch.NextReady()
	require.Empty(t, r, "失败传播后 c 没有 ready 节点")
	require.Equal(t, StatusSkipped, sch.Status("c"))
	require.True(t, sch.Done())
	require.Equal(t, StatusFailed, sch.Outcome())
}

func TestScheduler_IfAlways_RunsAfterFailure(t *testing.T) {
	// a 失败, b 带 if: always() → 仍然跑
	p := &dsl.Pipeline{
		Version: "1", Name: "x",
		Stages: []dsl.Stage{
			mkStage("a"),
			{ID: "notify", Needs: []string{"a"}, If: "always()",
				Steps: []dsl.Step{{Run: "ding"}}},
		},
	}
	d := BuildDAG(p)
	sch := NewScheduler(d)

	sch.NextReady()                      // a
	sch.Complete("a", StatusFailed)      // 上游失败
	r := sch.NextReady()
	require.Equal(t, []NodeID{"notify"}, r, "if: always() 上游失败仍 ready")
	sch.Complete("notify", StatusSuccess)
	require.True(t, sch.Done())
	require.Equal(t, StatusFailed, sch.Outcome(), "有节点 failed → outcome=failed (即便 always 节点 success)")
}

func TestScheduler_IfFailure_OnlyAfterFailure(t *testing.T) {
	// notify 仅在 a 失败时跑: a success → notify skip; a failed → notify ready
	mk := func(aResult NodeStatus) NodeStatus {
		p := &dsl.Pipeline{
			Version: "1", Name: "x",
			Stages: []dsl.Stage{
				mkStage("a"),
				{ID: "notify", Needs: []string{"a"}, If: "failure()",
					Steps: []dsl.Step{{Run: "alert"}}},
			},
		}
		d := BuildDAG(p)
		sch := NewScheduler(d)
		sch.NextReady()
		sch.Complete("a", aResult)
		sch.NextReady()
		// 如果是 fail 路径, notify 是 ready running, 还要 complete 它
		if sch.Status("notify") == StatusRunning {
			sch.Complete("notify", StatusSuccess)
		}
		return sch.Status("notify")
	}
	require.Equal(t, StatusSkipped, mk(StatusSuccess), "a success → notify skip")
	require.Equal(t, StatusSuccess, mk(StatusFailed), "a failed → notify 跑且 success")
}

// ---- Cancel ----

func TestScheduler_CancelAll(t *testing.T) {
	p := mkPipeline(mkStage("a"), mkStage("b", "a"), mkStage("c", "b"))
	d := BuildDAG(p)
	sch := NewScheduler(d)

	sch.NextReady() // a running
	sch.CancelAll()
	require.True(t, sch.Done())
	require.Equal(t, StatusCanceled, sch.Status("a"))
	require.Equal(t, StatusCanceled, sch.Status("b"))
	require.Equal(t, StatusCanceled, sch.Status("c"))
	require.Equal(t, StatusCanceled, sch.Outcome())
}

func TestScheduler_DownstreamSkipsCanceledUpstream(t *testing.T) {
	// 不调 CancelAll, 但手工 Complete a as canceled → b 应 skip
	p := mkPipeline(mkStage("a"), mkStage("b", "a"))
	d := BuildDAG(p)
	sch := NewScheduler(d)

	sch.NextReady()
	require.NoError(t, sch.Complete("a", StatusCanceled))
	sch.NextReady()
	require.Equal(t, StatusSkipped, sch.Status("b"), "上游 canceled → 下游 skip")
	require.True(t, sch.Done())
}

// ---- 不重复派发 + 终态防误更 ----

func TestScheduler_NextReady_Idempotent(t *testing.T) {
	p := mkPipeline(mkStage("a"))
	d := BuildDAG(p)
	sch := NewScheduler(d)

	r1 := sch.NextReady()
	require.Equal(t, []NodeID{"a"}, r1)
	r2 := sch.NextReady()
	require.Empty(t, r2, "同一 running 不会被再派一遍")
}

func TestScheduler_Complete_RejectsTerminal(t *testing.T) {
	p := mkPipeline(mkStage("a"))
	d := BuildDAG(p)
	sch := NewScheduler(d)
	sch.NextReady()
	require.NoError(t, sch.Complete("a", StatusSuccess))
	err := sch.Complete("a", StatusFailed)
	require.Error(t, err, "已 terminal 节点拒绝再 complete")
	require.Contains(t, err.Error(), "already terminal")
}

func TestScheduler_Complete_RejectsBadResult(t *testing.T) {
	p := mkPipeline(mkStage("a"))
	d := BuildDAG(p)
	sch := NewScheduler(d)
	sch.NextReady()
	err := sch.Complete("a", StatusReady) // 非允许的 result
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid completion")
}

func TestScheduler_Complete_UnknownNode(t *testing.T) {
	p := mkPipeline(mkStage("a"))
	d := BuildDAG(p)
	sch := NewScheduler(d)
	err := sch.Complete("ghost", StatusSuccess)
	require.Error(t, err)
}

// ---- 5 stage 综合 (含并行 + 串行 + matrix) ----

func TestScheduler_Integration_5Stage_Matrix(t *testing.T) {
	// 模拟 spec/04: checkout → test[matrix x3] → build → security-scan → deploy
	p := &dsl.Pipeline{
		Version: "1", Name: "x",
		Stages: []dsl.Stage{
			mkStage("checkout"),
			{
				ID: "test", Needs: []string{"checkout"},
				Matrix: &dsl.Matrix{Dimensions: map[string][]any{"go": {"1.21", "1.22", "1.23"}}},
				Steps:  []dsl.Step{{Run: "go test"}},
			},
			mkStage("build", "test"),
			mkStage("security-scan", "build"),
			mkStage("deploy", "security-scan"),
		},
	}

	expanded, err := ExpandPipeline(p)
	require.NoError(t, err)

	d := BuildDAG(expanded)
	sch := NewScheduler(d)

	// 轮 1: checkout
	r := sch.NextReady()
	require.Equal(t, []NodeID{"checkout"}, r)
	sch.Complete("checkout", StatusSuccess)

	// 轮 2: 3 个 test-* 并行
	r = sch.NextReady()
	sort.Strings(r)
	require.Equal(t, []NodeID{"test-0", "test-1", "test-2"}, r)
	for _, id := range r {
		sch.Complete(id, StatusSuccess)
	}

	// 轮 3: build
	r = sch.NextReady()
	require.Equal(t, []NodeID{"build"}, r)
	sch.Complete("build", StatusSuccess)

	// 轮 4: security-scan
	r = sch.NextReady()
	require.Equal(t, []NodeID{"security-scan"}, r)
	sch.Complete("security-scan", StatusSuccess)

	// 轮 5: deploy
	r = sch.NextReady()
	require.Equal(t, []NodeID{"deploy"}, r)
	sch.Complete("deploy", StatusSuccess)

	require.True(t, sch.Done())
	require.Equal(t, StatusSuccess, sch.Outcome())
}

// ---- E2.6: approval 节点 NextReady 转 waiting_approval, Complete 推进 ----

func TestScheduler_ApprovalNode_WaitsForCompletion(t *testing.T) {
	// build → approve(type=approval) → deploy
	p := &dsl.Pipeline{
		Version: "1", Name: "x",
		Stages: []dsl.Stage{
			mkStage("build"),
			{
				ID: "approve", Needs: []string{"build"},
				Type: "approval", Approvers: []string{"alice"},
			},
			mkStage("deploy", "approve"),
		},
	}
	d := BuildDAG(p)
	sch := NewScheduler(d)

	// 轮 1: build
	require.Equal(t, []NodeID{"build"}, sch.NextReady())
	require.NoError(t, sch.Complete("build", StatusSuccess))

	// 轮 2: approve 被派发, 但状态是 waiting_approval (不是 running)
	r := sch.NextReady()
	require.Equal(t, []NodeID{"approve"}, r)
	require.Equal(t, StatusWaitingApproval, sch.Status("approve"))

	// 同时 deploy 还在 pending, 也不会出现在 ready
	require.Equal(t, StatusPending, sch.Status("deploy"))
	require.False(t, sch.Done())
	require.Empty(t, sch.NextReady(), "approve 还没结果时不能再返")

	// 模拟审批通过
	require.NoError(t, sch.Complete("approve", StatusSuccess))

	// 轮 3: deploy ready
	require.Equal(t, []NodeID{"deploy"}, sch.NextReady())
	require.NoError(t, sch.Complete("deploy", StatusSuccess))
	require.True(t, sch.Done())
	require.Equal(t, StatusSuccess, sch.Outcome())
}

func TestScheduler_ApprovalNode_Rejected_DownstreamSkip(t *testing.T) {
	// approve 节点 Complete failed (即被 reject), 下游 deploy 应 skip
	p := &dsl.Pipeline{
		Version: "1", Name: "x",
		Stages: []dsl.Stage{
			{ID: "approve", Type: "approval", Approvers: []string{"alice"}},
			mkStage("deploy", "approve"),
		},
	}
	d := BuildDAG(p)
	sch := NewScheduler(d)

	r := sch.NextReady()
	require.Equal(t, []NodeID{"approve"}, r)
	require.Equal(t, StatusWaitingApproval, sch.Status("approve"))

	require.NoError(t, sch.Complete("approve", StatusFailed))
	sch.NextReady()
	require.Equal(t, StatusSkipped, sch.Status("deploy"))
	require.True(t, sch.Done())
	require.Equal(t, StatusFailed, sch.Outcome())
}
