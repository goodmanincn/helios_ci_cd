// Package expr — Helios pipeline 表达式引擎 (M2 E2.3)。
//
// 处理 `${{ ... }}` 模板内的内容. 输入是字符串 (内部表达式), 输出 AST + 可求值结果.
//
// 文件分工:
//   - ast.go     本文件: AST 节点定义 + String() 调试输出
//   - lexer.go   词法分析 (token 流)
//   - parser.go  递归下降语法分析 (token → AST)
//   - eval.go    求值器 (AST + Context → value)
//   - render.go  模板渲染 (扫字符串中所有 ${{ }} 并渲染, T2.3.3)
//
// 语法目标 (spec/04 § 4.2):
//   - 字面量: string / number / bool / null
//   - 引用: a.b.c (vars/env/inputs/secrets/matrix/needs/github/run/steps 等)
//   - 运算符: ! * / + - == != < > <= >= && || ?:
//   - 函数: contains / startsWith / endsWith / format / fromJSON / toJSON
//          + 上下文函数 success() / failure() / always() / cancelled()
//   - 括号 + 嵌套
//
// 不支持 (有意为之):
//   - 用户自定义函数 (插件可后续注册)
//   - 数组/对象字面量 (用 fromJSON 字符串代替)
//   - 赋值 / 副作用
package expr

import (
	"fmt"
	"strings"
)

// NodeKind AST 节点类型枚举。
type NodeKind int

const (
	NodeLiteral NodeKind = iota
	NodeIdent           // 单个标识符 (matrix / github 之类的根)
	NodeMember           // a.b 或 a.b.c (member access chain)
	NodeUnary            // !x / -x
	NodeBinary           // x OP y
	NodeTernary          // cond ? a : b
	NodeCall             // f(args)
	NodeIndex            // a[b]
)

// Node AST 通用结构。
//
// 不同 Kind 使用不同字段:
//   Literal: Value (string/float64/bool/nil)
//   Ident:   Name
//   Member:  Left + Name (递归: Left 可以是 Member / Ident / Call / Index)
//   Index:   Left + Index (Index 是表达式, 通常 string 或 number)
//   Unary:   Op + Right
//   Binary:  Op + Left + Right
//   Ternary: Cond + Then + Else
//   Call:    Name + Args
type Node struct {
	Kind    NodeKind
	Value   any      // Literal
	Name    string   // Ident / Member / Call
	Op      string   // Unary / Binary
	Left    *Node    // Member / Binary / Index
	Right   *Node    // Unary / Binary
	Index   *Node    // Index
	Cond    *Node    // Ternary
	Then    *Node    // Ternary
	Else    *Node    // Ternary
	Args    []*Node  // Call
	Pos     int      // 原始字符串中的偏移 (debug 用)
}

// String 调试输出 (近似 Lisp s-expr)。
func (n *Node) String() string {
	if n == nil {
		return "<nil>"
	}
	switch n.Kind {
	case NodeLiteral:
		switch v := n.Value.(type) {
		case string:
			return fmt.Sprintf("%q", v)
		case nil:
			return "null"
		default:
			return fmt.Sprintf("%v", v)
		}
	case NodeIdent:
		return n.Name
	case NodeMember:
		return n.Left.String() + "." + n.Name
	case NodeIndex:
		return fmt.Sprintf("%s[%s]", n.Left, n.Index)
	case NodeUnary:
		return fmt.Sprintf("(%s %s)", n.Op, n.Right)
	case NodeBinary:
		return fmt.Sprintf("(%s %s %s)", n.Op, n.Left, n.Right)
	case NodeTernary:
		return fmt.Sprintf("(? %s %s %s)", n.Cond, n.Then, n.Else)
	case NodeCall:
		args := make([]string, len(n.Args))
		for i, a := range n.Args {
			args[i] = a.String()
		}
		return fmt.Sprintf("(%s %s)", n.Name, strings.Join(args, " "))
	}
	return "<?>"
}

// ParseError 解析错误带原文 + 偏移定位。
type ParseError struct {
	Source  string
	Pos     int
	Message string
}

func (e *ParseError) Error() string {
	// 输出形如:  parse error at 12: unexpected token 'X' (source: "a.b X c")
	src := e.Source
	if len(src) > 80 {
		src = src[:80] + "..."
	}
	return fmt.Sprintf("expr parse error at offset %d: %s (in %q)", e.Pos, e.Message, src)
}
