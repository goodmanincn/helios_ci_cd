// Package expr tests — lexer + parser + eval + render 覆盖。
//
// 目标:
//   - 30+ case 覆盖运算符 / 函数 / 嵌套 (T2.3.2 验收)
//   - 不依赖 dsl 包, 独立可跑
package expr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ---- helpers ----

func evalStr(t *testing.T, src string, ctx *Context) any {
	t.Helper()
	n, err := Parse(src)
	require.NoError(t, err, "parse %q", src)
	v, err := Eval(n, ctx)
	require.NoError(t, err, "eval %q", src)
	return v
}

func evalStrErr(t *testing.T, src string, ctx *Context) error {
	t.Helper()
	n, err := Parse(src)
	if err != nil {
		return err
	}
	_, err = Eval(n, ctx)
	return err
}

// ---- Lexer / Parser 基础 ----

func TestParse_Literals(t *testing.T) {
	require.Equal(t, float64(42), evalStr(t, "42", nil))
	require.Equal(t, 3.14, evalStr(t, "3.14", nil))
	require.Equal(t, "hi", evalStr(t, "'hi'", nil))
	require.Equal(t, "hi", evalStr(t, `"hi"`, nil))
	require.Equal(t, true, evalStr(t, "true", nil))
	require.Equal(t, false, evalStr(t, "false", nil))
	require.Nil(t, evalStr(t, "null", nil))
}

func TestParse_Arithmetic(t *testing.T) {
	require.Equal(t, float64(7), evalStr(t, "3 + 4", nil))
	require.Equal(t, float64(-1), evalStr(t, "3 - 4", nil))
	require.Equal(t, float64(20), evalStr(t, "4 * 5", nil))
	require.Equal(t, float64(2.5), evalStr(t, "5 / 2", nil))
	require.Equal(t, float64(1), evalStr(t, "5 % 2", nil))
	require.Equal(t, float64(14), evalStr(t, "2 + 3 * 4", nil))         // 优先级
	require.Equal(t, float64(20), evalStr(t, "(2 + 3) * 4", nil))       // 括号
	require.Equal(t, float64(-5), evalStr(t, "-5", nil))                 // unary
	require.Equal(t, float64(-1), evalStr(t, "-(2-1)", nil))             // unary + 括号
}

func TestParse_DivZero(t *testing.T) {
	err := evalStrErr(t, "1 / 0", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "division by zero")
}

func TestParse_Compare(t *testing.T) {
	require.Equal(t, true, evalStr(t, "1 < 2", nil))
	require.Equal(t, false, evalStr(t, "1 > 2", nil))
	require.Equal(t, true, evalStr(t, "2 >= 2", nil))
	require.Equal(t, true, evalStr(t, "2 <= 2", nil))
	require.Equal(t, true, evalStr(t, "1 == 1", nil))
	require.Equal(t, true, evalStr(t, "1 != 2", nil))
	require.Equal(t, true, evalStr(t, "'a' < 'b'", nil))
	require.Equal(t, true, evalStr(t, "'1' == 1", nil), "loose eq 跨类型")
}

func TestParse_Logical(t *testing.T) {
	require.Equal(t, true, evalStr(t, "true && true", nil))
	require.Equal(t, false, evalStr(t, "true && false", nil))
	require.Equal(t, true, evalStr(t, "false || true", nil))
	require.Equal(t, false, evalStr(t, "false || false", nil))
	require.Equal(t, !false, evalStr(t, "!false", nil))
	require.Equal(t, !true, evalStr(t, "!true", nil))
	// 短路: || 返回 left 真值, 与 GitHub Actions / JS 一致
	require.Equal(t, "default", evalStr(t, "'' || 'default'", nil))
	require.Equal(t, "left", evalStr(t, "'left' || 'right'", nil))
}

func TestParse_StringConcat(t *testing.T) {
	require.Equal(t, "hello world", evalStr(t, "'hello ' + 'world'", nil))
	require.Equal(t, "v1.2", evalStr(t, "'v' + 1.2", nil))
}

func TestParse_Ternary(t *testing.T) {
	require.Equal(t, "yes", evalStr(t, "1 == 1 ? 'yes' : 'no'", nil))
	require.Equal(t, "no", evalStr(t, "1 == 2 ? 'yes' : 'no'", nil))
	// 嵌套
	require.Equal(t, "c", evalStr(t, "false ? 'a' : (false ? 'b' : 'c')", nil))
}

// ---- 引用 + 上下文 ----

func TestParse_References(t *testing.T) {
	ctx := &Context{
		Values: map[string]any{
			"vars": map[string]any{
				"IMAGE_TAG": "abc123",
				"COUNT":     5,
			},
			"env": map[string]any{
				"REGISTRY": "ccr.example.com",
			},
			"matrix": map[string]any{
				"go-version": "1.22",
			},
			"needs": map[string]any{
				"build": map[string]any{
					"outputs": map[string]any{
						"image": "myimg:abc",
					},
				},
			},
			"github": map[string]any{
				"sha":  "deadbeef",
				"repo_url": "https://github.com/acme/api",
			},
		},
	}
	require.Equal(t, "abc123", evalStr(t, "vars.IMAGE_TAG", ctx))
	require.Equal(t, 5, evalStr(t, "vars.COUNT", ctx))
	require.Equal(t, "ccr.example.com/api:abc123",
		evalStr(t, "env.REGISTRY + '/api:' + vars.IMAGE_TAG", ctx))
	require.Equal(t, "myimg:abc", evalStr(t, "needs.build.outputs.image", ctx))
	require.Equal(t, "1.22", evalStr(t, "matrix['go-version']", ctx))
	require.Equal(t, "deadbeef", evalStr(t, "github.sha", ctx))
}

func TestParse_NilSafe(t *testing.T) {
	// 未定义 → nil, 不报错
	require.Nil(t, evalStr(t, "unknown.foo.bar", nil))
	require.Equal(t, false, evalStr(t, "unknown == 'x'", nil))
}

// ---- 函数 ----

func TestFn_Contains(t *testing.T) {
	require.Equal(t, true, evalStr(t, "contains('hello world', 'world')", nil))
	require.Equal(t, false, evalStr(t, "contains('abc', 'd')", nil))
}

func TestFn_StartsEndsWith(t *testing.T) {
	require.Equal(t, true, evalStr(t, "startsWith('refs/heads/main', 'refs/')", nil))
	require.Equal(t, false, evalStr(t, "startsWith('main', 'refs/')", nil))
	require.Equal(t, true, evalStr(t, "endsWith('foo.md', '.md')", nil))
}

func TestFn_Format(t *testing.T) {
	require.Equal(t, "image: api v1.0",
		evalStr(t, "format('image: {0} v{1}', 'api', '1.0')", nil))
	require.Equal(t, "no args", evalStr(t, "format('no args')", nil))
}

func TestFn_FromJSON_ToJSON(t *testing.T) {
	require.Equal(t, "x", evalStr(t, "fromJSON('{\"a\":\"x\"}').a", nil))
	require.Equal(t, "[1,2,3]", evalStr(t, "toJSON(fromJSON('[1,2,3]'))", nil))
}

func TestFn_Join(t *testing.T) {
	// 从 fromJSON 取数组再 join
	require.Equal(t, "a,b,c", evalStr(t, "join(fromJSON('[\"a\",\"b\",\"c\"]'), ',')", nil))
}

func TestFn_StatusFns(t *testing.T) {
	// success() / failure() / always() / cancelled()
	check := func(rs string, sfn, ffn, afn, cfn bool) {
		ctx := &Context{RunStatus: rs}
		require.Equal(t, sfn, evalStr(t, "success()", ctx), "rs=%s success", rs)
		require.Equal(t, ffn, evalStr(t, "failure()", ctx), "rs=%s failure", rs)
		require.Equal(t, afn, evalStr(t, "always()", ctx), "rs=%s always", rs)
		require.Equal(t, cfn, evalStr(t, "cancelled()", ctx), "rs=%s cancelled", rs)
	}
	check("", true, false, true, false)
	check("success", true, false, true, false)
	check("failure", false, true, true, false)
	check("cancelled", false, false, true, true)
}

func TestFn_UnknownErr(t *testing.T) {
	err := evalStrErr(t, "missingFn()", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown function")
}

// ---- 复杂综合 ----

func TestParse_ComplexExpr(t *testing.T) {
	ctx := &Context{
		Values: map[string]any{
			"branch": "main",
			"vars":   map[string]any{"production": true},
		},
		RunStatus: "success",
	}
	// branch == 'main' && success() && vars.production
	require.Equal(t, true,
		evalStr(t, "branch == 'main' && success() && vars.production", ctx))
}

// ---- Parse 错误 ----

func TestParse_SyntaxErrors(t *testing.T) {
	cases := []string{
		"1 +",        // 缺右值
		"(1 + 2",     // 缺括号
		"1 ? 2",      // 三元缺 :
		"vars.",       // 缺标识符
		"1 & 2",       // & 单字符无效
		"'unterminated", // 字符串未闭
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			_, err := Parse(src)
			require.Error(t, err, "应报错: %s", src)
		})
	}
}

// ---- RenderString ----

func TestRender_WholeExpr_PreservesType(t *testing.T) {
	ctx := &Context{
		Values: map[string]any{
			"needs": map[string]any{
				"build": map[string]any{
					"outputs": map[string]any{"image": "myimg:abc"},
				},
			},
		},
	}
	rendered, raw, errs := RenderString("${{ needs.build.outputs.image }}", ctx, "x")
	require.Empty(t, errs)
	require.Equal(t, "myimg:abc", rendered)
	require.Equal(t, "myimg:abc", raw)

	// 整段拿对象 → raw 是 map (不被 toString)
	rendered, raw, errs = RenderString("${{ needs.build.outputs }}", ctx, "x")
	require.Empty(t, errs)
	// rendered 是 map 的 stringify, raw 是 map 本体
	_, ok := raw.(map[string]any)
	require.True(t, ok, "raw 应该是 map, 得 %T", raw)
	_ = rendered
}

func TestRender_MixedString(t *testing.T) {
	ctx := &Context{
		Values: map[string]any{
			"vars": map[string]any{"TAG": "v1"},
			"env":  map[string]any{"REG": "r.io"},
		},
	}
	r, _, errs := RenderString("image=${{ env.REG }}/api:${{ vars.TAG }}", ctx, "x")
	require.Empty(t, errs)
	require.Equal(t, "image=r.io/api:v1", r)
}

func TestRender_AccumulatesErrors(t *testing.T) {
	r, _, errs := RenderString("a=${{ 1/0 }} b=${{ 1+ }}", nil, "x")
	require.Len(t, errs, 2, "两个表达式都错, 累积 2")
	// rebuilt 字符串保留原表达式作为视觉占位
	require.Contains(t, r, "${{ 1/0 }}")
}

func TestRender_NoTemplate_Passthrough(t *testing.T) {
	r, _, errs := RenderString("plain string", nil, "x")
	require.Empty(t, errs)
	require.Equal(t, "plain string", r)
}
