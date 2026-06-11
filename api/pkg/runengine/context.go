package runengine

import (
	"github.com/helios-cicd/helios/api/pkg/dsl"
	"github.com/helios-cicd/helios/api/pkg/expr"
)

// BuildExprContext 组装 stage 执行前的表达式上下文。
func BuildExprContext(run *RunInfo, p *dsl.Pipeline, stageOutputs map[string]map[string]any) *expr.Context {
	values := map[string]any{
		"vars":   map[string]any{},
		"env":    map[string]any{},
		"github": map[string]any{
			"sha":    run.CommitSHA,
			"ref":    run.Branch,
			"branch": run.Branch,
		},
		"run": map[string]any{
			"id":     run.ID,
			"status": run.Status,
		},
	}
	if p != nil {
		if len(p.Variables) > 0 {
			vars := map[string]any{}
			for k, v := range p.Variables {
				vars[k] = v
			}
			values["vars"] = vars
		}
		if len(p.Env) > 0 {
			env := map[string]any{}
			for k, v := range p.Env {
				env[k] = v
			}
			values["env"] = env
		}
	}
	if len(stageOutputs) > 0 {
		needs := map[string]any{}
		for sid, outs := range stageOutputs {
			needs[sid] = map[string]any{"outputs": outs}
		}
		values["needs"] = needs
	}
	return &expr.Context{Values: values, RunStatus: run.Status}
}
