// Package expr — lexer.go: 词法分析。
//
// 输入: 表达式字符串 (已经从 ${{ ... }} 剥出, 不含分隔符)
// 输出: token 流, 由 parser 消费
//
// 处理:
//   - 跳空白
//   - 数字 (整数/小数), 字符串 ('...' 或 "...", 不支持转义嵌套引号, 保持简洁; 真要嵌入 ' 用 fromJSON)
//   - 标识符 (字母+数字+_, 首字符字母/_), 保留字 true/false/null 单独 token
//   - 多字符运算符 == != <= >= && ||
//   - 单字符运算符 + - * / % ! < > , . ( ) [ ] ?  :
//
// 错误尽量带 offset (Pos), parser 把 offset → 行列让上层报。
package expr

import (
	"fmt"
	"unicode"
	"unicode/utf8"
)

// TokenKind 词法类型。
type TokenKind int

const (
	TokEOF TokenKind = iota
	TokNumber
	TokString
	TokIdent
	TokBool
	TokNull
	TokPunct // 运算符 / 标点 (具体内容在 Value)
)

// Token 一个词元。
type Token struct {
	Kind  TokenKind
	Value string  // 标识符名 / 运算符串 / 字符串明文 (已去引号)
	Num   float64 // Kind=Number 时填
	Bool  bool    // Kind=Bool 时填
	Pos   int     // 原始字符串偏移 (utf8 字节, 不是 rune index)
}

func (t Token) String() string {
	switch t.Kind {
	case TokEOF:
		return "EOF"
	case TokNumber:
		return fmt.Sprintf("Num(%v)", t.Num)
	case TokString:
		return fmt.Sprintf("Str(%q)", t.Value)
	case TokIdent:
		return "Id(" + t.Value + ")"
	case TokBool:
		return fmt.Sprintf("Bool(%v)", t.Bool)
	case TokNull:
		return "null"
	case TokPunct:
		return "'" + t.Value + "'"
	}
	return "<?>"
}

// Tokenize 把整个表达式扫成 token slice + EOF。
func Tokenize(src string) ([]Token, error) {
	var out []Token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c >= '0' && c <= '9':
			j := i
			dot := false
			for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '.') {
				if src[j] == '.' {
					if dot {
						break // 第二个 . 不吃
					}
					dot = true
				}
				j++
			}
			n := 0.0
			_, err := fmt.Sscanf(src[i:j], "%f", &n)
			if err != nil {
				return nil, &ParseError{Source: src, Pos: i, Message: "invalid number: " + err.Error()}
			}
			out = append(out, Token{Kind: TokNumber, Num: n, Pos: i, Value: src[i:j]})
			i = j
		case c == '\'' || c == '"':
			quote := c
			j := i + 1
			for j < len(src) && src[j] != quote {
				j++
			}
			if j >= len(src) {
				return nil, &ParseError{Source: src, Pos: i, Message: "unterminated string literal"}
			}
			out = append(out, Token{Kind: TokString, Value: src[i+1 : j], Pos: i})
			i = j + 1
		case isIdentStart(rune(c)):
			j := i + 1
			for j < len(src) {
				r, sz := utf8.DecodeRuneInString(src[j:])
				if !isIdentCont(r) {
					_ = sz
					break
				}
				j += sz
			}
			name := src[i:j]
			switch name {
			case "true":
				out = append(out, Token{Kind: TokBool, Bool: true, Value: name, Pos: i})
			case "false":
				out = append(out, Token{Kind: TokBool, Bool: false, Value: name, Pos: i})
			case "null":
				out = append(out, Token{Kind: TokNull, Value: name, Pos: i})
			default:
				out = append(out, Token{Kind: TokIdent, Value: name, Pos: i})
			}
			i = j
		case c == '=' || c == '!' || c == '<' || c == '>':
			if i+1 < len(src) && src[i+1] == '=' {
				out = append(out, Token{Kind: TokPunct, Value: string(c) + "=", Pos: i})
				i += 2
			} else {
				out = append(out, Token{Kind: TokPunct, Value: string(c), Pos: i})
				i++
			}
		case c == '&' || c == '|':
			if i+1 < len(src) && src[i+1] == c {
				out = append(out, Token{Kind: TokPunct, Value: string(c) + string(c), Pos: i})
				i += 2
			} else {
				return nil, &ParseError{Source: src, Pos: i, Message: fmt.Sprintf("unexpected '%c' (did you mean %c%c?)", c, c, c)}
			}
		case c == '+' || c == '-' || c == '*' || c == '/' || c == '%' ||
			c == '(' || c == ')' || c == '[' || c == ']' ||
			c == ',' || c == '.' || c == '?' || c == ':':
			out = append(out, Token{Kind: TokPunct, Value: string(c), Pos: i})
			i++
		default:
			return nil, &ParseError{Source: src, Pos: i, Message: fmt.Sprintf("unexpected character %q", string(c))}
		}
	}
	out = append(out, Token{Kind: TokEOF, Pos: i})
	return out, nil
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentCont(r rune) bool {
	return r == '_' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
