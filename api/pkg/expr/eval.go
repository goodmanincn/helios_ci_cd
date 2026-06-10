// Package expr — eval.go: AST 求值器 + 内置函数 (T2.3.2)。
//
// 输入:
//   - 一个 Parse 得到的 *Node
//   - Context (上下文 map): vars / env / inputs / secrets / matrix / needs / github / run / steps
//   - 可选: RunStatus (用于 success/failure/always/cancelled 上下文函数)
//
// 输出: 单个 value (string / float64 / bool / nil / map / slice — 取决于引用结果)
//
// 类型语义 (尽量贴 GitHub Actions, 简化版):
//   - 数字 + 数字 = 数字; 字符串 + 任意 = 字符串拼接
//   - == / != 跨类型自动 stringify 比较 (e.g. "1" == 1 → true)
//   - && / || 短路求值, 返"决定结果"的值而非 bool 强转 (与 JS 行为一致, expression 的常见用法)
//   - !x 用 truthy 判断 (nil/0/""/false → false, 其它 true)
//
// 错误处理: EvalError 带原表达式位置, 上层渲染时合并到 RenderError 报。
package expr

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Context 求值上下文。
//
// Values 是顶层 root → any 的 map; 通常 root 是 "vars"/"env"/"inputs"/"secrets"/"matrix"/
// "needs"/"github"/"run"/"steps". value 自身可以是 map/struct/slice/string/任何东西。
//
// RunStatus 给 success/failure/always/cancelled 函数用. 字符串值之一:
//   "" (默认 success) / "success" / "failure" / "cancelled" / "skipped"
type Context struct {
	Values    map[string]any
	RunStatus string
}

// Eval AST → value. 不可变 ctx.
func Eval(n *Node, ctx *Context) (any, error) {
	if n == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = &Context{}
	}
	return evalNode(n, ctx)
}

// EvalError 求值错误.
type EvalError struct {
	Pos     int
	Message string
}

func (e *EvalError) Error() string {
	return fmt.Sprintf("expr eval error at offset %d: %s", e.Pos, e.Message)
}

func evalErr(n *Node, msg string) error {
	pos := 0
	if n != nil {
		pos = n.Pos
	}
	return &EvalError{Pos: pos, Message: msg}
}

// ---- core ----

func evalNode(n *Node, ctx *Context) (any, error) {
	switch n.Kind {
	case NodeLiteral:
		return n.Value, nil
	case NodeIdent:
		// 顶层标识符 → ctx.Values[name]; 找不到返 nil 而非报错 (与 Actions 一致)
		if v, ok := ctx.Values[n.Name]; ok {
			return v, nil
		}
		return nil, nil
	case NodeMember:
		left, err := evalNode(n.Left, ctx)
		if err != nil {
			return nil, err
		}
		return memberAccess(left, n.Name), nil
	case NodeIndex:
		left, err := evalNode(n.Left, ctx)
		if err != nil {
			return nil, err
		}
		idx, err := evalNode(n.Index, ctx)
		if err != nil {
			return nil, err
		}
		return indexAccess(left, idx), nil
	case NodeUnary:
		return evalUnary(n, ctx)
	case NodeBinary:
		return evalBinary(n, ctx)
	case NodeTernary:
		c, err := evalNode(n.Cond, ctx)
		if err != nil {
			return nil, err
		}
		if truthy(c) {
			return evalNode(n.Then, ctx)
		}
		return evalNode(n.Else, ctx)
	case NodeCall:
		return evalCall(n, ctx)
	}
	return nil, evalErr(n, "unknown node kind")
}

func evalUnary(n *Node, ctx *Context) (any, error) {
	r, err := evalNode(n.Right, ctx)
	if err != nil {
		return nil, err
	}
	switch n.Op {
	case "!":
		return !truthy(r), nil
	case "-":
		f, ok := toNumber(r)
		if !ok {
			return nil, evalErr(n, fmt.Sprintf("cannot negate %T", r))
		}
		return -f, nil
	}
	return nil, evalErr(n, "unknown unary op "+n.Op)
}

func evalBinary(n *Node, ctx *Context) (any, error) {
	// 短路 && ||
	if n.Op == "&&" || n.Op == "||" {
		left, err := evalNode(n.Left, ctx)
		if err != nil {
			return nil, err
		}
		if n.Op == "&&" {
			if !truthy(left) {
				return left, nil
			}
			return evalNode(n.Right, ctx)
		}
		// ||
		if truthy(left) {
			return left, nil
		}
		return evalNode(n.Right, ctx)
	}

	left, err := evalNode(n.Left, ctx)
	if err != nil {
		return nil, err
	}
	right, err := evalNode(n.Right, ctx)
	if err != nil {
		return nil, err
	}

	switch n.Op {
	case "==":
		return looseEq(left, right), nil
	case "!=":
		return !looseEq(left, right), nil
	case "<", ">", "<=", ">=":
		return cmpNumOrStr(n.Op, left, right), nil
	case "+":
		// 字符串拼接优先
		if isStr(left) || isStr(right) {
			return toString(left) + toString(right), nil
		}
		la, ok1 := toNumber(left)
		ra, ok2 := toNumber(right)
		if !ok1 || !ok2 {
			return nil, evalErr(n, fmt.Sprintf("cannot + %T and %T", left, right))
		}
		return la + ra, nil
	case "-", "*", "/", "%":
		la, ok1 := toNumber(left)
		ra, ok2 := toNumber(right)
		if !ok1 || !ok2 {
			return nil, evalErr(n, fmt.Sprintf("cannot %s %T and %T", n.Op, left, right))
		}
		switch n.Op {
		case "-":
			return la - ra, nil
		case "*":
			return la * ra, nil
		case "/":
			if ra == 0 {
				return nil, evalErr(n, "division by zero")
			}
			return la / ra, nil
		case "%":
			if ra == 0 {
				return nil, evalErr(n, "modulo by zero")
			}
			return float64(int(la) % int(ra)), nil
		}
	}
	return nil, evalErr(n, "unknown binary op "+n.Op)
}

// ---- 函数调用 ----

func evalCall(n *Node, ctx *Context) (any, error) {
	// 函数名约定: 小写, 单个 token. 上下文函数 success/failure/always/cancelled 无参或忽略参数.
	switch n.Name {
	case "success":
		s := ctx.RunStatus
		return s == "" || s == "success", nil
	case "failure":
		return ctx.RunStatus == "failure" || ctx.RunStatus == "failed", nil
	case "always":
		return true, nil
	case "cancelled", "canceled":
		return ctx.RunStatus == "cancelled" || ctx.RunStatus == "canceled", nil

	case "contains":
		return call2(n, ctx, func(a, b any) (any, error) {
			s := toString(a)
			needle := toString(b)
			return strings.Contains(s, needle), nil
		})
	case "startsWith":
		return call2(n, ctx, func(a, b any) (any, error) {
			return strings.HasPrefix(toString(a), toString(b)), nil
		})
	case "endsWith":
		return call2(n, ctx, func(a, b any) (any, error) {
			return strings.HasSuffix(toString(a), toString(b)), nil
		})
	case "format":
		// format("hello {0} {1}", x, y) → 简单 {idx} 替换 (避免 fmt.Sprintf 的 %s 怪坑)
		if len(n.Args) == 0 {
			return nil, evalErr(n, "format() needs at least format string")
		}
		fmtArg, err := evalNode(n.Args[0], ctx)
		if err != nil {
			return nil, err
		}
		tpl := toString(fmtArg)
		args := make([]any, 0, len(n.Args)-1)
		for _, a := range n.Args[1:] {
			v, err := evalNode(a, ctx)
			if err != nil {
				return nil, err
			}
			args = append(args, v)
		}
		out := tpl
		for i, a := range args {
			out = strings.ReplaceAll(out, fmt.Sprintf("{%d}", i), toString(a))
		}
		return out, nil
	case "fromJSON":
		if len(n.Args) != 1 {
			return nil, evalErr(n, "fromJSON() needs 1 arg")
		}
		a, err := evalNode(n.Args[0], ctx)
		if err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal([]byte(toString(a)), &v); err != nil {
			return nil, evalErr(n, "fromJSON: "+err.Error())
		}
		return v, nil
	case "toJSON":
		if len(n.Args) != 1 {
			return nil, evalErr(n, "toJSON() needs 1 arg")
		}
		a, err := evalNode(n.Args[0], ctx)
		if err != nil {
			return nil, err
		}
		b, err := json.Marshal(a)
		if err != nil {
			return nil, evalErr(n, "toJSON: "+err.Error())
		}
		return string(b), nil
	case "join":
		// join(arr, sep) 返字符串
		if len(n.Args) != 2 {
			return nil, evalErr(n, "join() needs 2 args (slice, sep)")
		}
		a, err := evalNode(n.Args[0], ctx)
		if err != nil {
			return nil, err
		}
		sep, err := evalNode(n.Args[1], ctx)
		if err != nil {
			return nil, err
		}
		arr, ok := a.([]any)
		if !ok {
			// 兼容 []string
			if ss, ok := a.([]string); ok {
				return strings.Join(ss, toString(sep)), nil
			}
			return nil, evalErr(n, "join() first arg must be array")
		}
		parts := make([]string, len(arr))
		for i, v := range arr {
			parts[i] = toString(v)
		}
		return strings.Join(parts, toString(sep)), nil
	}
	return nil, evalErr(n, "unknown function "+n.Name)
}

// call2 帮 contains/startsWith/endsWith 节省样板.
func call2(n *Node, ctx *Context, f func(a, b any) (any, error)) (any, error) {
	if len(n.Args) != 2 {
		return nil, evalErr(n, n.Name+"() needs 2 args")
	}
	a, err := evalNode(n.Args[0], ctx)
	if err != nil {
		return nil, err
	}
	b, err := evalNode(n.Args[1], ctx)
	if err != nil {
		return nil, err
	}
	return f(a, b)
}

// ---- helpers ----

func memberAccess(v any, name string) any {
	if v == nil {
		return nil
	}
	switch m := v.(type) {
	case map[string]any:
		return m[name]
	case map[string]string:
		if s, ok := m[name]; ok {
			return s
		}
		return nil
	case map[any]any:
		return m[name]
	}
	return nil
}

func indexAccess(v, idx any) any {
	if v == nil {
		return nil
	}
	switch m := v.(type) {
	case map[string]any:
		return m[toString(idx)]
	case map[string]string:
		if s, ok := m[toString(idx)]; ok {
			return s
		}
		return nil
	case []any:
		i, ok := toNumber(idx)
		if !ok {
			return nil
		}
		ii := int(i)
		if ii < 0 || ii >= len(m) {
			return nil
		}
		return m[ii]
	case []string:
		i, ok := toNumber(idx)
		if !ok {
			return nil
		}
		ii := int(i)
		if ii < 0 || ii >= len(m) {
			return nil
		}
		return m[ii]
	}
	return nil
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case int:
		return x != 0
	case string:
		return x != ""
	}
	return true
}

func toNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

func toString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		// 整数显示为整数 (避免 1.000000)
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case int, int64:
		return fmt.Sprintf("%d", x)
	}
	return fmt.Sprintf("%v", v)
}

func isStr(v any) bool {
	_, ok := v.(string)
	return ok
}

func looseEq(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	// 同类型直接比
	if a == b {
		return true
	}
	// 数字 vs 字符串: 都试着 stringify
	if isStr(a) || isStr(b) {
		// 字符串场景, 把另一边也字符串化
		return toString(a) == toString(b)
	}
	// 数字间
	fa, ok1 := toNumber(a)
	fb, ok2 := toNumber(b)
	if ok1 && ok2 {
		return fa == fb
	}
	return false
}

func cmpNumOrStr(op string, a, b any) bool {
	// 优先数字比较
	fa, ok1 := toNumber(a)
	fb, ok2 := toNumber(b)
	if ok1 && ok2 {
		switch op {
		case "<":
			return fa < fb
		case ">":
			return fa > fb
		case "<=":
			return fa <= fb
		case ">=":
			return fa >= fb
		}
	}
	// 字符串比较
	sa, sb := toString(a), toString(b)
	switch op {
	case "<":
		return sa < sb
	case ">":
		return sa > sb
	case "<=":
		return sa <= sb
	case ">=":
		return sa >= sb
	}
	return false
}
