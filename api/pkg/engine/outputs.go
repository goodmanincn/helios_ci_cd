// Package engine — outputs.go: stage outputs 求值 + 下游 needs.X.outputs 注入 (T2.4.3)。
//
// 流程:
//   1. stage 跑完 (所有 steps), 各 step 产出 stepOutputs (e.g. upload-artifact 返 id/name)
//   2. ResolveStageOutputs 根据 stage.Outputs 模板 (e.g. `image_tag: "${{ steps.upload.outputs.id }}"`)
//      在 expr 上下文里查 steps.<id>.outputs.<key>, 求出该 stage 的 outputs map
//   3. RunOutputs (per-run 累积) 把每个 stage 的 outputs 收起来, 给下游 stage 渲染前注入到
//      ctx.Values["needs"][stage.id]["outputs"]
//
// 设计:
//   - 不绑定 scheduler IO; 纯函数, scheduler/worker 调用前后自己组装 context
//   - stepOutputs 的 key 是 step.ID (DSL 里写 `- id: upload  uses: helios/upload@v1`), 没 id 的 step 跳过
//   - outputs 模板字符串走 expr.RenderString, 整段 ${{ }} 保留原类型 (e.g. number, map)
package engine

import (
	"fmt"
	"sync"

	"github.com/helios-cicd/helios/api/pkg/dsl"
	"github.com/helios-cicd/helios/api/pkg/expr"
)

// StageRunResult 一个 stage 跑完后传给 engine 的数据。
//
// StepOutputs 是 step.ID → outputs map (e.g. {"upload": {"id": "123/dist"}}). 没 id 的 step 不进。
type StageRunResult struct {
	StageID     string
	Status      NodeStatus
	StepOutputs map[string]map[string]any
}

// ResolveStageOutputs 根据 stage.Outputs 模板 + 跑完的 stepOutputs, 求 stage outputs map。
//
// 用例 (典型):
//
//   stage:
//     id: build
//     outputs:
//       image_tag: "${{ steps.upload.outputs.id }}"
//
//   stepOutputs = {"upload": {"id": "123/dist"}}
//   → outputs = {"image_tag": "123/dist"}
//
// 求值失败的 key 跳过 (记到 errs) 而不是整个 abort, 让下游能拿到部分 outputs。
func ResolveStageOutputs(
	stage *dsl.Stage,
	stepOutputs map[string]map[string]any,
	baseCtx *expr.Context,
) (map[string]any, []*expr.RenderError) {
	if stage == nil || len(stage.Outputs) == 0 {
		return nil, nil
	}

	// 构造求值 ctx: 在 baseCtx 之上叠 steps.<id>.outputs.<key>
	values := map[string]any{}
	if baseCtx != nil {
		for k, v := range baseCtx.Values {
			values[k] = v
		}
	}
	steps := make(map[string]any, len(stepOutputs))
	for sid, outs := range stepOutputs {
		steps[sid] = map[string]any{"outputs": outs}
	}
	values["steps"] = steps

	ctx := &expr.Context{
		Values:    values,
		RunStatus: orDefault(baseCtx, "").RunStatus,
	}

	out := make(map[string]any, len(stage.Outputs))
	var errs []*expr.RenderError
	for key, tpl := range stage.Outputs {
		rendered, raw, es := expr.RenderString(tpl, ctx, fmt.Sprintf("stages.%s.outputs.%s", stage.ID, key))
		if len(es) > 0 {
			errs = append(errs, es...)
			continue
		}
		// 整段单 ${{ }} 时保留原类型, 否则用 string
		if raw != nil {
			if _, isStr := raw.(string); !isStr {
				out[key] = raw
				continue
			}
		}
		out[key] = rendered
	}
	return out, errs
}

// RunOutputs 整个 run 内所有 stage outputs 的累积器, 线程安全。
//
// 调度器在 stage 完成后调 Set, 下游 stage 启动前调 Snapshot 拿到完整 map,
// 注入到 ctx.Values["needs"][stage_id]["outputs"].
type RunOutputs struct {
	mu sync.RWMutex
	m  map[string]map[string]any // stage_id → outputs
}

func NewRunOutputs() *RunOutputs {
	return &RunOutputs{m: map[string]map[string]any{}}
}

// Set 写一个 stage 的 outputs (覆盖). 通常每个 stage 只 Set 一次.
func (r *RunOutputs) Set(stageID string, outputs map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if outputs == nil {
		// 显式置空也行
		r.m[stageID] = map[string]any{}
		return
	}
	cp := make(map[string]any, len(outputs))
	for k, v := range outputs {
		cp[k] = v
	}
	r.m[stageID] = cp
}

// Snapshot 返回当前所有 stage outputs 的副本 (深一层, value 仍是浅引).
func (r *RunOutputs) Snapshot() map[string]map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]map[string]any, len(r.m))
	for k, v := range r.m {
		cp := make(map[string]any, len(v))
		for kk, vv := range v {
			cp[kk] = vv
		}
		out[k] = cp
	}
	return out
}

// BuildNeedsContext 把 RunOutputs.Snapshot 包装成 needs.<stage_id>.outputs 形态,
// 给下游 expr.Context.Values["needs"] 用。
func (r *RunOutputs) BuildNeedsContext() map[string]any {
	snap := r.Snapshot()
	needs := make(map[string]any, len(snap))
	for sid, outs := range snap {
		needs[sid] = map[string]any{"outputs": outs}
	}
	return needs
}

// ---- 内部辅助 ----

func orDefault(c *expr.Context, _ string) *expr.Context {
	if c == nil {
		return &expr.Context{}
	}
	return c
}
