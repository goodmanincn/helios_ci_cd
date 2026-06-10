// render_test.go — dsl.RenderPipeline / RenderStage (T2.3.3) 集成测试
package dsl

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/helios-cicd/helios/api/pkg/expr"
)

func TestRenderPipeline_EnvAndVars(t *testing.T) {
	p := &Pipeline{
		Version: "1", Name: "test",
		Env: map[string]string{
			"REG": "ccr.example.com",
		},
		Variables: map[string]string{
			"TAG": "${{ github.sha }}",
		},
	}
	ctx := &expr.Context{
		Values: map[string]any{
			"github": map[string]any{"sha": "abc123"},
		},
	}
	r := RenderPipeline(p, ctx)
	require.Empty(t, r.Errors)
	require.Equal(t, "ccr.example.com", r.Pipeline.Env["REG"], "无模板原样")
	require.Equal(t, "abc123", r.Pipeline.Variables["TAG"], "${{ }} 渲染")
}

func TestRenderPipeline_StageStepRun(t *testing.T) {
	p := &Pipeline{
		Version: "1", Name: "x",
		Env: map[string]string{"REG": "r.io"},
		Variables: map[string]string{
			"IMG": "myapi:${{ vars.TAG }}", // TODO M2 后续: env→vars 链式渲染留 caller 自己 ctx 装
		},
		Stages: []Stage{
			{
				ID: "build",
				Steps: []Step{
					{Run: "docker build -t ${{ env.REG }}/${{ vars.IMG }} ."},
				},
			},
		},
	}
	ctx := &expr.Context{
		Values: map[string]any{
			"env":  map[string]any{"REG": "r.io"},
			"vars": map[string]any{"TAG": "v1", "IMG": "myapi:v1"},
		},
	}
	r := RenderPipeline(p, ctx)
	require.Empty(t, r.Errors)
	require.Equal(t,
		"docker build -t r.io/myapi:v1 .",
		r.Pipeline.Stages[0].Steps[0].Run)
}

func TestRenderStage_DownstreamNeedsOutputs(t *testing.T) {
	// 模拟 build stage 完成, deploy stage 引用 needs.build.outputs.image
	deploy := &Stage{
		ID: "deploy", Needs: []string{"build"},
		Steps: []Step{
			{
				Run: "kubectl set image deploy/api api=${{ needs.build.outputs.image }}",
				With: map[string]any{
					"image": "${{ needs.build.outputs.image }}",
					"keep":  "static",
				},
			},
		},
	}
	ctx := &expr.Context{
		Values: map[string]any{
			"needs": map[string]any{
				"build": map[string]any{
					"outputs": map[string]any{"image": "myapi:abc123"},
				},
			},
		},
	}
	out, errs := RenderStage(deploy, ctx, "stages[1]")
	require.Empty(t, errs)
	require.Equal(t,
		"kubectl set image deploy/api api=myapi:abc123",
		out.Steps[0].Run)
	// with.image 整段模板, 应保留原类型 (这里是 string)
	require.Equal(t, "myapi:abc123", out.Steps[0].With["image"])
	require.Equal(t, "static", out.Steps[0].With["keep"])
}

func TestRenderStage_RunsOnImageMatrix(t *testing.T) {
	// matrix 实例: 这条 stage 的 image 引用 matrix.go-version
	s := &Stage{
		ID: "test",
		RunsOn: &RunsOn{
			Type:  "container",
			Image: "golang:${{ matrix.go-version }}",
		},
		Steps: []Step{{Run: "go test"}},
	}
	ctx := &expr.Context{
		Values: map[string]any{
			"matrix": map[string]any{"go-version": "1.22"},
		},
	}
	out, errs := RenderStage(s, ctx, "stages[0]")
	require.Empty(t, errs)
	require.Equal(t, "golang:1.22", out.RunsOn.Image)
}

func TestRenderPipeline_AccumulatesErrors_NoAbort(t *testing.T) {
	// 故意两个表达式坏掉, 期待都被报但 pipeline 仍然返回
	p := &Pipeline{
		Version: "1", Name: "x",
		Variables: map[string]string{"BAD": "${{ 1 / 0 }}"},
		Stages: []Stage{
			{ID: "a", Steps: []Step{{Run: "echo ${{ 1 + }}"}}},
		},
	}
	r := RenderPipeline(p, nil)
	require.NotNil(t, r.Pipeline)
	require.GreaterOrEqual(t, len(r.Errors), 2)
}
