// Package dsl — render.go: 把 Pipeline 中所有字符串字段渲染 ${{ }} (T2.3.3)。
//
// 与 expr.RenderString 配合: 遍历 Pipeline 结构, 对每个 string 字段调用 expr.RenderString,
// 错误累积不中断, 返回的 Pipeline 是 *新* 副本, 原 p 不动.
//
// 渲染时机由调度器决定:
//   - "整 pipeline 一次性渲染" 适合开 run 前打全包 (vars/env 静态)
//   - "每个 stage 启动前局部渲染" 适合动态上下文 (matrix.x, needs.X.outputs.Y)
//   两者都可用本函数, 区别在 caller 传入 ctx 不同。
//
// 不渲染的字段:
//   - id (DAG key, 不允许动态)
//   - type (approval / 空)
//   - 数值 / bool 类字段
//   - triggers (静态)
package dsl

import (
	"fmt"

	"github.com/helios-cicd/helios/api/pkg/expr"
)

// RenderResult 渲染结果.
type RenderResult struct {
	Pipeline *Pipeline           // 渲染后的副本
	Errors   []*expr.RenderError // 累积错误, 空表示全成功
}

// RenderPipeline 整 pipeline 渲染。ctx 为 nil 时按空 context 走 (没引用就 ok)。
func RenderPipeline(p *Pipeline, ctx *expr.Context) *RenderResult {
	if p == nil {
		return &RenderResult{}
	}
	out := &Pipeline{
		Version:     p.Version,
		Name:        p.Name,
		Description: p.Description,
		Triggers:    p.Triggers, // triggers 暂不渲染 (静态)
	}
	res := &RenderResult{Pipeline: out}

	if p.Env != nil {
		out.Env = make(map[string]string, len(p.Env))
		for k, v := range p.Env {
			r, _, errs := expr.RenderString(v, ctx, "env."+k)
			res.Errors = append(res.Errors, errs...)
			out.Env[k] = r
		}
	}
	if p.Variables != nil {
		out.Variables = make(map[string]string, len(p.Variables))
		for k, v := range p.Variables {
			r, _, errs := expr.RenderString(v, ctx, "variables."+k)
			res.Errors = append(res.Errors, errs...)
			out.Variables[k] = r
		}
	}

	out.Stages = make([]Stage, len(p.Stages))
	for i, s := range p.Stages {
		out.Stages[i] = renderStage(&s, ctx, fmt.Sprintf("stages[%d]", i), &res.Errors)
	}
	return res
}

// RenderStage 单 stage 渲染. ctx 通常包含 matrix.* / needs.* / env / vars 等.
// 调度器在 stage 启动前调用 (拿到该 stage 的局部 ctx 后).
func RenderStage(s *Stage, ctx *expr.Context, path string) (Stage, []*expr.RenderError) {
	var errs []*expr.RenderError
	out := renderStage(s, ctx, path, &errs)
	return out, errs
}

func renderStage(s *Stage, ctx *expr.Context, path string, errs *[]*expr.RenderError) Stage {
	out := *s // 浅拷顶层
	if s.If != "" {
		out.If = renderStr(s.If, ctx, path+".if", errs)
	}
	if s.RunsOn != nil {
		ro := *s.RunsOn
		if ro.Image != "" {
			ro.Image = renderStr(ro.Image, ctx, path+".runs-on.image", errs)
		}
		out.RunsOn = &ro
	}
	if s.Env != nil {
		out.Env = renderStrMap(s.Env, ctx, path+".env", errs)
	}
	if s.Outputs != nil {
		out.Outputs = renderStrMap(s.Outputs, ctx, path+".outputs", errs)
	}
	if s.With != nil {
		out.With = renderAnyMap(s.With, ctx, path+".with", errs)
	}
	if len(s.Steps) > 0 {
		out.Steps = make([]Step, len(s.Steps))
		for i, st := range s.Steps {
			out.Steps[i] = renderStep(&st, ctx, fmt.Sprintf("%s.steps[%d]", path, i), errs)
		}
	}
	return out
}

func renderStep(st *Step, ctx *expr.Context, path string, errs *[]*expr.RenderError) Step {
	out := *st
	if st.Run != "" {
		out.Run = renderStr(st.Run, ctx, path+".run", errs)
	}
	if st.If != "" {
		out.If = renderStr(st.If, ctx, path+".if", errs)
	}
	if st.Env != nil {
		out.Env = renderStrMap(st.Env, ctx, path+".env", errs)
	}
	if st.With != nil {
		out.With = renderAnyMap(st.With, ctx, path+".with", errs)
	}
	return out
}

// ---- helpers ----

func renderStr(s string, ctx *expr.Context, path string, errs *[]*expr.RenderError) string {
	r, _, es := expr.RenderString(s, ctx, path)
	*errs = append(*errs, es...)
	return r
}

func renderStrMap(m map[string]string, ctx *expr.Context, path string, errs *[]*expr.RenderError) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = renderStr(v, ctx, path+"."+k, errs)
	}
	return out
}

// renderAnyMap 对 with: {...} 这种 map[string]any 渲染.
// 字符串字段: 全 ${{ }} 时保留原求值类型 (caller 可能依赖 e.g. number/array),
// 否则 toString 拼回.
// 非字符串字段直接透传 (yaml decode 出来的 number/bool/嵌套 map 不动).
func renderAnyMap(m map[string]any, ctx *expr.Context, path string, errs *[]*expr.RenderError) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			out[k] = v
			continue
		}
		rendered, raw, es := expr.RenderString(s, ctx, path+"."+k)
		*errs = append(*errs, es...)
		// raw != nil && raw 不是 string 时, 整段是单表达式且返回了非字符串原型 → 保留 raw
		if raw != nil {
			if _, isStr := raw.(string); !isStr {
				out[k] = raw
				continue
			}
		}
		out[k] = rendered
	}
	return out
}
