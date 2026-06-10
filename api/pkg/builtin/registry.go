// Package builtin — Helios 内置 step 注册表 (M2 E2.4)。
//
// 模型:
//   - DSL 中 `uses: helios/upload-artifact@v1` 等 ref → 注册表查到 BuiltinStep
//   - BuiltinStep 是一个纯函数式接口: Run(ctx, ExecContext, inputs) → (outputs, err)
//   - 调度器执行 step 时分两路:
//        - step.Uses 命中注册表 → builtin step
//        - 否则走外部插件解析 (M3) / Docker registry pull
//
// 设计取舍:
//   - 不绑定 Asynq / DB; ExecContext 是 caller (worker) 准备的 IO bundle (storage + workspace dir + logger)
//   - 输入参数 inputs 来自 step.With (已经 expr 渲染过), 类型 map[string]any
//   - outputs 是返回给上游的 map (会进入 needs.X.outputs 上下文)
//   - 错误带 step ref, 让日志清晰
package builtin

import (
	"context"
	"fmt"
	"io"

	"github.com/helios-cicd/helios/api/pkg/artifact"
)

// ExecContext 执行环境, caller (worker) 装配。
//
// 字段约定:
//   - Ctx       请求级 context (取消传播)
//   - RunID     当前 run id, 用于 storage key
//   - StageID   当前 stage id (含 matrix suffix)
//   - WorkDir   workspace 根目录 (一般是 /tmp/helios/runs/<run>/src)
//   - Storage   artifact storage (LocalFS / S3 等)
//   - Log       日志输出 (writer; 写到 step.log + 推 SSE)
type ExecContext struct {
	Ctx     context.Context
	RunID   int64
	StageID string
	WorkDir string
	Storage artifact.Storage
	Log     io.Writer
}

// Step 一个内置 step 的执行器。
type Step interface {
	Name() string
	// Run 执行; inputs 是 step.With 渲染后的 map; 返回 step outputs (会进 needs.X.outputs).
	Run(ec *ExecContext, inputs map[string]any) (outputs map[string]any, err error)
}

// Registry 内置 step 注册表 (单例 + Register, 调用方 init() 注册).
//
// 注册名约定: "helios/<name>@v<N>"  与 DSL `uses:` 字面对齐.
// 同名重复注册会 panic (开发期就发现, 别静默覆盖).
var registry = map[string]Step{}

// Register 加一个 step. 通常在 init() 调.
func Register(s Step) {
	if _, dup := registry[s.Name()]; dup {
		panic("builtin: duplicate step name " + s.Name())
	}
	registry[s.Name()] = s
}

// Lookup 查表; 返回 (step, true) 命中, (nil, false) 未命中.
func Lookup(ref string) (Step, bool) {
	s, ok := registry[ref]
	return s, ok
}

// List 当前注册的所有 step 名 (排序留 caller).
func List() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}

// ---- 通用辅助 ----

// MustString 从 inputs 取 string 字段, 缺失/类型错时返 error (带 step + field 名).
func MustString(stepName string, inputs map[string]any, field string) (string, error) {
	v, ok := inputs[field]
	if !ok {
		return "", fmt.Errorf("%s: missing input %q", stepName, field)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s: input %q must be string, got %T", stepName, field, v)
	}
	if s == "" {
		return "", fmt.Errorf("%s: input %q is empty", stepName, field)
	}
	return s, nil
}

// OptString 取可选字符串, 不存在返默认值 dflt.
func OptString(inputs map[string]any, field, dflt string) string {
	if v, ok := inputs[field]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return dflt
}

// StringList 把 input 解为 []string. 支持 yaml 的 []any / []string / 单字符串 (自动包成 1 元素).
func StringList(inputs map[string]any, field string) ([]string, error) {
	v, ok := inputs[field]
	if !ok {
		return nil, nil
	}
	switch x := v.(type) {
	case []string:
		return x, nil
	case []any:
		out := make([]string, len(x))
		for i, item := range x {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("input %q[%d]: not a string (%T)", field, i, item)
			}
			out[i] = s
		}
		return out, nil
	case string:
		return []string{x}, nil
	}
	return nil, fmt.Errorf("input %q: expected string or list, got %T", field, v)
}
