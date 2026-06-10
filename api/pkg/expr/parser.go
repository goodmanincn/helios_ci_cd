// Package expr — parser.go: 递归下降语法分析。
//
// 优先级 (从低到高):
//   1. ternary:    cond ? a : b
//   2. logical-or:  ||
//   3. logical-and: &&
//   4. equality:   ==  !=
//   5. comparison: < > <= >=
//   6. add/sub:    + -
//   7. mul/div:    * / %
//   8. unary:      ! -
//   9. postfix:    .ident / [expr] / (args...) (member / index / call)
//  10. primary:    literal / ident / (expr)
//
// 与 GitHub Actions 表达式基本一致, 区别:
//   - 没有 ${} 嵌套 (Helios 表达式已经在 ${{ }} 里, 不允许再嵌)
//   - 没有 type coercion 函数 (toJSON/fromJSON 当作普通函数)
//
// 错误统一 *ParseError, 带 offset, 上层 (Render) 转换成行号。
package expr

import (
	"fmt"
)

// Parse 把表达式字符串编译成 AST。
//
// 输入: 已剥离 ${{ }} 的表达式正文 (e.g. "vars.IMAGE_TAG == 'main'")
// 输出: Node 根, 调用 Eval 求值
func Parse(src string) (*Node, error) {
	toks, err := Tokenize(src)
	if err != nil {
		return nil, err
	}
	p := &parser{src: src, tokens: toks}
	node, err := p.parseTernary()
	if err != nil {
		return nil, err
	}
	if p.peek().Kind != TokEOF {
		t := p.peek()
		return nil, &ParseError{
			Source: src, Pos: t.Pos,
			Message: fmt.Sprintf("unexpected trailing token %s", t),
		}
	}
	return node, nil
}

// parser 内部状态.
type parser struct {
	src    string
	tokens []Token
	pos    int // 当前 token 下标
}

func (p *parser) peek() Token         { return p.tokens[p.pos] }
func (p *parser) eat() Token          { t := p.tokens[p.pos]; p.pos++; return t }
func (p *parser) eof() bool           { return p.peek().Kind == TokEOF }
func (p *parser) errAt(t Token, m string) error {
	return &ParseError{Source: p.src, Pos: t.Pos, Message: m}
}

// 期望 punct, 不匹配返错; 匹配则吃掉。
func (p *parser) expectPunct(v string) error {
	t := p.peek()
	if t.Kind != TokPunct || t.Value != v {
		return p.errAt(t, fmt.Sprintf("expected %q, got %s", v, t))
	}
	p.eat()
	return nil
}

// matchPunct 不匹配返 false 不前进, 匹配返 true 并吃掉。
func (p *parser) matchPunct(v string) bool {
	t := p.peek()
	if t.Kind == TokPunct && t.Value == v {
		p.eat()
		return true
	}
	return false
}

// ---- 各级 (从低优先级到高) ----

// parseTernary: orExpr ('?' ternary ':' ternary)?
func (p *parser) parseTernary() (*Node, error) {
	cond, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if !p.matchPunct("?") {
		return cond, nil
	}
	thenN, err := p.parseTernary()
	if err != nil {
		return nil, err
	}
	if err := p.expectPunct(":"); err != nil {
		return nil, err
	}
	elseN, err := p.parseTernary()
	if err != nil {
		return nil, err
	}
	return &Node{Kind: NodeTernary, Cond: cond, Then: thenN, Else: elseN, Pos: cond.Pos}, nil
}

func (p *parser) parseOr() (*Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.matchPunct("||") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &Node{Kind: NodeBinary, Op: "||", Left: left, Right: right, Pos: left.Pos}
	}
	return left, nil
}

func (p *parser) parseAnd() (*Node, error) {
	left, err := p.parseEq()
	if err != nil {
		return nil, err
	}
	for p.matchPunct("&&") {
		right, err := p.parseEq()
		if err != nil {
			return nil, err
		}
		left = &Node{Kind: NodeBinary, Op: "&&", Left: left, Right: right, Pos: left.Pos}
	}
	return left, nil
}

func (p *parser) parseEq() (*Node, error) {
	left, err := p.parseCmp()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.Kind != TokPunct || (t.Value != "==" && t.Value != "!=") {
			break
		}
		p.eat()
		right, err := p.parseCmp()
		if err != nil {
			return nil, err
		}
		left = &Node{Kind: NodeBinary, Op: t.Value, Left: left, Right: right, Pos: left.Pos}
	}
	return left, nil
}

func (p *parser) parseCmp() (*Node, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.Kind != TokPunct ||
			(t.Value != "<" && t.Value != ">" && t.Value != "<=" && t.Value != ">=") {
			break
		}
		p.eat()
		right, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		left = &Node{Kind: NodeBinary, Op: t.Value, Left: left, Right: right, Pos: left.Pos}
	}
	return left, nil
}

func (p *parser) parseAdd() (*Node, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.Kind != TokPunct || (t.Value != "+" && t.Value != "-") {
			break
		}
		p.eat()
		right, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		left = &Node{Kind: NodeBinary, Op: t.Value, Left: left, Right: right, Pos: left.Pos}
	}
	return left, nil
}

func (p *parser) parseMul() (*Node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.Kind != TokPunct || (t.Value != "*" && t.Value != "/" && t.Value != "%") {
			break
		}
		p.eat()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &Node{Kind: NodeBinary, Op: t.Value, Left: left, Right: right, Pos: left.Pos}
	}
	return left, nil
}

func (p *parser) parseUnary() (*Node, error) {
	t := p.peek()
	if t.Kind == TokPunct && (t.Value == "!" || t.Value == "-") {
		p.eat()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &Node{Kind: NodeUnary, Op: t.Value, Right: right, Pos: t.Pos}, nil
	}
	return p.parsePostfix()
}

// parsePostfix: primary ('.' ident | '[' expr ']' | '(' args ')')*
func (p *parser) parsePostfix() (*Node, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.Kind != TokPunct {
			break
		}
		switch t.Value {
		case ".":
			p.eat()
			id := p.peek()
			if id.Kind != TokIdent {
				return nil, p.errAt(id, "expected identifier after '.'")
			}
			p.eat()
			left = &Node{Kind: NodeMember, Left: left, Name: id.Value, Pos: left.Pos}
		case "[":
			p.eat()
			idx, err := p.parseTernary()
			if err != nil {
				return nil, err
			}
			if err := p.expectPunct("]"); err != nil {
				return nil, err
			}
			left = &Node{Kind: NodeIndex, Left: left, Index: idx, Pos: left.Pos}
		case "(":
			// 只有 Ident 或 Member 可被 call (函数名)
			if left.Kind != NodeIdent && left.Kind != NodeMember {
				return nil, p.errAt(t, "only identifier or member can be called")
			}
			p.eat()
			var args []*Node
			if !p.matchPunct(")") {
				for {
					a, err := p.parseTernary()
					if err != nil {
						return nil, err
					}
					args = append(args, a)
					if p.matchPunct(",") {
						continue
					}
					if err := p.expectPunct(")"); err != nil {
						return nil, err
					}
					break
				}
			}
			// 函数名转 Call: 把 Ident/Member 摊平成 "a.b" 形式当函数名
			name := flattenName(left)
			left = &Node{Kind: NodeCall, Name: name, Args: args, Pos: left.Pos}
		default:
			return left, nil
		}
	}
	return left, nil
}

func (p *parser) parsePrimary() (*Node, error) {
	t := p.peek()
	switch t.Kind {
	case TokNumber:
		p.eat()
		return &Node{Kind: NodeLiteral, Value: t.Num, Pos: t.Pos}, nil
	case TokString:
		p.eat()
		return &Node{Kind: NodeLiteral, Value: t.Value, Pos: t.Pos}, nil
	case TokBool:
		p.eat()
		return &Node{Kind: NodeLiteral, Value: t.Bool, Pos: t.Pos}, nil
	case TokNull:
		p.eat()
		return &Node{Kind: NodeLiteral, Value: nil, Pos: t.Pos}, nil
	case TokIdent:
		p.eat()
		return &Node{Kind: NodeIdent, Name: t.Value, Pos: t.Pos}, nil
	case TokPunct:
		if t.Value == "(" {
			p.eat()
			n, err := p.parseTernary()
			if err != nil {
				return nil, err
			}
			if err := p.expectPunct(")"); err != nil {
				return nil, err
			}
			return n, nil
		}
	}
	return nil, p.errAt(t, fmt.Sprintf("unexpected token %s", t))
}

// flattenName 把 a.b.c 节点摊平成字符串当函数名.
func flattenName(n *Node) string {
	switch n.Kind {
	case NodeIdent:
		return n.Name
	case NodeMember:
		return flattenName(n.Left) + "." + n.Name
	}
	return ""
}
