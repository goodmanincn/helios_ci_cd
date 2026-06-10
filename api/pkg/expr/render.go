// Package expr — render.go: 模板渲染 (T2.3.3)。
//
// 给一个字符串扫所有 ${{ ... }} 表达式, 求值替换。
// 也提供 RenderPipeline 对 dsl.Pipeline 整树递归 (string 字段全渲染), 用于调度器在
// 每个 stage 开跑前用 fresh context 求出实际值 (e.g. matrix.go-version, needs.X.outputs.Y).
//
// 渲染策略 (T2.3.3 验收要求):
//   - 单纯 "${{ ... }}" 作为完整字符串 → 求值结果原类型透传 (e.g. needs map → map)
//   - 字符串里嵌入 "prefix ${{ ... }} suffix" → 求值结果 toString 后拼回字符串
//   - 一行多 ${{ }} 块都替换
//   - 求值错误不中断, 累积到 RenderResult.Errors 让 caller 决定 (e.g. expr 报错某 stage skip)
//
// 不会读 / 改 yaml 原文; 只工作在 dsl.Pipeline 副本上。
package expr

import (
	"fmt"
	"regexp"
	"strings"
)

// 提取 ${{ ... }} 块. 不允许换行 (\}\} 必须在同一行); 内容用最短匹配避免吃过头。
var tplPattern = regexp.MustCompile(`\$\{\{\s*([\s\S]*?)\s*\}\}`)

// 完整匹配某字符串就是单个表达式 (前后无其它字符)
var wholeTplPattern = regexp.MustCompile(`^\$\{\{\s*([\s\S]*?)\s*\}\}$`)

// RenderError 一处渲染错误.
type RenderError struct {
	Path   string // e.g. "stages[2].steps[0].run"
	Expr   string // 出错的 ${{ ... }} 原文 (含分隔符)
	Err    error
}

func (e *RenderError) Error() string {
	return fmt.Sprintf("render error at %s: %s [in %q]", e.Path, e.Err, e.Expr)
}

// RenderString 渲染单个字符串。
//
// 返回:
//   - 如果整个字符串就是 "${{ ... }}", 返求值结果 + 替换后的字符串 (toString 形式)
//     caller 可以用 value 再 type-assert (e.g. needs.x.outputs 拿 map)
//   - 否则返 (toString 拼回的字符串, nil 错误) 或带错误
//
// 错误累积到 errs (slice), caller 自行决定 abort 还是 skip.
func RenderString(s string, ctx *Context, path string) (rendered string, raw any, errs []*RenderError) {
	// 完整单表达式: 走"原类型"语义 (caller 可能依赖类型)
	if m := wholeTplPattern.FindStringSubmatch(s); m != nil {
		exprSrc := m[1]
		node, err := Parse(exprSrc)
		if err != nil {
			errs = append(errs, &RenderError{Path: path, Expr: s, Err: err})
			return "", nil, errs
		}
		v, err := Eval(node, ctx)
		if err != nil {
			errs = append(errs, &RenderError{Path: path, Expr: s, Err: err})
			return "", nil, errs
		}
		return toString(v), v, nil
	}

	// 混合: 多块替换, 全 toString 拼回
	var rebuilt strings.Builder
	last := 0
	for _, idx := range tplPattern.FindAllStringSubmatchIndex(s, -1) {
		// idx = [matchStart, matchEnd, groupStart, groupEnd]
		mStart, mEnd, gStart, gEnd := idx[0], idx[1], idx[2], idx[3]
		rebuilt.WriteString(s[last:mStart])
		exprSrc := s[gStart:gEnd]
		node, err := Parse(exprSrc)
		if err != nil {
			errs = append(errs, &RenderError{
				Path: path, Expr: s[mStart:mEnd], Err: err,
			})
			rebuilt.WriteString(s[mStart:mEnd]) // 保留原文方便定位
			last = mEnd
			continue
		}
		v, err := Eval(node, ctx)
		if err != nil {
			errs = append(errs, &RenderError{
				Path: path, Expr: s[mStart:mEnd], Err: err,
			})
			rebuilt.WriteString(s[mStart:mEnd])
			last = mEnd
			continue
		}
		rebuilt.WriteString(toString(v))
		last = mEnd
	}
	rebuilt.WriteString(s[last:])
	return rebuilt.String(), rebuilt.String(), errs
}
