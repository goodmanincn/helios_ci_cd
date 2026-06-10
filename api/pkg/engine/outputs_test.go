// outputs_test.go — stage outputs 求值 + RunOutputs 累积器 + 下游 needs 注入。
package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/helios-cicd/helios/api/pkg/dsl"
	"github.com/helios-cicd/helios/api/pkg/expr"
)

func TestResolveStageOutputs_Basic(t *testing.T) {
	s := &dsl.Stage{
		ID: "build",
		Outputs: map[string]string{
			"image_tag": "${{ steps.upload.outputs.id }}",
			"region":    "us-east-1", // 无模板, 静态字符串
		},
	}
	stepOutputs := map[string]map[string]any{
		"upload": {"id": "500/dist"},
	}
	out, errs := ResolveStageOutputs(s, stepOutputs, nil)
	require.Empty(t, errs)
	require.Equal(t, "500/dist", out["image_tag"])
	require.Equal(t, "us-east-1", out["region"])
}

func TestResolveStageOutputs_PreservesType(t *testing.T) {
	// 整段 ${{ }} 引用一个 number/map, 应该保留类型
	s := &dsl.Stage{
		ID: "x",
		Outputs: map[string]string{
			"count": "${{ steps.s.outputs.n }}",
		},
	}
	stepOutputs := map[string]map[string]any{
		"s": {"n": float64(42)},
	}
	out, errs := ResolveStageOutputs(s, stepOutputs, nil)
	require.Empty(t, errs)
	require.Equal(t, float64(42), out["count"], "数字类型应保留")
}

func TestResolveStageOutputs_NoOutputs_Nil(t *testing.T) {
	s := &dsl.Stage{ID: "x"}
	out, errs := ResolveStageOutputs(s, nil, nil)
	require.Nil(t, out)
	require.Empty(t, errs)
}

func TestResolveStageOutputs_ErrAccumulates(t *testing.T) {
	// 一个键 OK, 一个键 expr 错; 期望 OK 的进结果, 错的进 errs
	s := &dsl.Stage{
		ID: "x",
		Outputs: map[string]string{
			"good": "${{ steps.a.outputs.x }}",
			"bad":  "${{ 1 + }}",
		},
	}
	out, errs := ResolveStageOutputs(s,
		map[string]map[string]any{"a": {"x": "yes"}}, nil)
	require.Equal(t, "yes", out["good"])
	require.NotContains(t, out, "bad", "求值失败的 key 不进 outputs")
	require.NotEmpty(t, errs)
}

func TestRunOutputs_SetSnapshot(t *testing.T) {
	r := NewRunOutputs()
	r.Set("build", map[string]any{"image_tag": "abc"})
	r.Set("test", map[string]any{"coverage": 0.85})

	snap := r.Snapshot()
	require.Len(t, snap, 2)
	require.Equal(t, "abc", snap["build"]["image_tag"])
	require.Equal(t, 0.85, snap["test"]["coverage"])

	// 改 snap 不影响内部 (depth=1)
	snap["build"]["image_tag"] = "MUT"
	snap2 := r.Snapshot()
	require.Equal(t, "abc", snap2["build"]["image_tag"], "Snapshot 应该是副本")
}

func TestRunOutputs_BuildNeedsContext(t *testing.T) {
	r := NewRunOutputs()
	r.Set("build", map[string]any{"image_tag": "v1"})

	needs := r.BuildNeedsContext()
	// 结构: needs.build.outputs.image_tag
	require.Contains(t, needs, "build")
	build := needs["build"].(map[string]any)
	require.Contains(t, build, "outputs")
	outs := build["outputs"].(map[string]any)
	require.Equal(t, "v1", outs["image_tag"])
}

func TestEndToEnd_OutputsFlowToDownstream(t *testing.T) {
	// 模拟完整流: build stage 完成 → 求 outputs → 注入 ctx → deploy stage 渲染时
	// 看到 needs.build.outputs.image_tag
	build := &dsl.Stage{
		ID: "build",
		Outputs: map[string]string{
			"image_tag": "${{ steps.upload.outputs.id }}",
		},
	}
	stepOutputs := map[string]map[string]any{
		"upload": {"id": "500/dist"},
	}
	buildOuts, errs := ResolveStageOutputs(build, stepOutputs, nil)
	require.Empty(t, errs)

	runOuts := NewRunOutputs()
	runOuts.Set("build", buildOuts)

	// 下游 deploy 渲染
	deploy := &dsl.Stage{
		ID:    "deploy",
		Needs: []string{"build"},
		Steps: []dsl.Step{
			{Run: "deploy --image=${{ needs.build.outputs.image_tag }}"},
		},
	}
	ctx := &expr.Context{
		Values: map[string]any{
			"needs": runOuts.BuildNeedsContext(),
		},
	}
	rendered, errs := dsl.RenderStage(deploy, ctx, "stages[1]")
	require.Empty(t, errs)
	require.Equal(t, "deploy --image=500/dist", rendered.Steps[0].Run)
}
