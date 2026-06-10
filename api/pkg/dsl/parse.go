// Package dsl — parse.go: YAML → Pipeline 结构体, 保留 yaml.Node 以便后续 validator 找 line:col。
//
// 设计:
//   - Parse 一次 yaml.Unmarshal 到 Node + Decode 到 Pipeline, 失败按原生错误信息透传 (yaml.v3
//     已经带 line: 信息)
//   - ParseStrict 增加 KnownFields = true, 拒绝未知字段 (生产用; dev/编辑器接 Parse 允许容错)
//   - 返回的 Node 给 validator.go 用来定位"stage 3 needs[0] 引用不存在"这类错的具体位置
package dsl

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseResult 解析产物。
type ParseResult struct {
	Pipeline *Pipeline  // 解析后的结构 (Parse 失败时仍可能部分非空)
	Root     *yaml.Node // YAML AST 根节点, 用于 validator 反查位置
	// 顶层 stages 节点 (SequenceNode), 缓存方便 validator 索引到 stage[i]; 可能为 nil
	StagesNode *yaml.Node
}

// Parse 把 YAML 文本解析成 Pipeline + 保留 AST。
//
// 容错点:
//   - YAML 语法错误 → 返回 SyntaxError (含 line)
//   - 字段类型错配 (e.g. needs 给了 string) → 返回 TypeError (含 line)
//   - 未知字段不报错 (走 Extra 落地)
//
// Strict 模式见 ParseStrict。
func Parse(src []byte) (*ParseResult, error) {
	r := &ParseResult{}

	// 1) 第一遍: 解到 Node 拿 AST
	var root yaml.Node
	if err := yaml.Unmarshal(src, &root); err != nil {
		return nil, wrapYAMLErr(err, "syntax")
	}
	if root.Kind == 0 {
		// 空文档
		return nil, &Error{Kind: ErrSyntax, Message: "empty document", Line: 1}
	}
	r.Root = &root

	// 2) 第二遍: Node → Pipeline
	var p Pipeline
	if err := root.Decode(&p); err != nil {
		return nil, wrapYAMLErr(err, "decode")
	}
	r.Pipeline = &p

	// 3) 把 stages 节点找出来, validator 用
	r.StagesNode = childMap(&root, "stages")

	return r, nil
}

// ParseStrict 在 Parse 基础上拒绝未知字段。生产校验入口用这个。
func ParseStrict(src []byte) (*ParseResult, error) {
	r := &ParseResult{}
	var root yaml.Node
	if err := yaml.Unmarshal(src, &root); err != nil {
		return nil, wrapYAMLErr(err, "syntax")
	}
	r.Root = &root

	var p Pipeline
	// yaml.v3 默认 KnownFields(false), 用 Decoder + KnownFields(true) 才严格
	dec := yaml.NewDecoder(strings.NewReader(string(src)))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return nil, wrapYAMLErr(err, "strict-decode")
	}
	r.Pipeline = &p
	r.StagesNode = childMap(&root, "stages")
	return r, nil
}

// ---- yaml node 辅助 ----

// childMap 在 MappingNode 上找指定 key 对应的 value Node。找不到返 nil。
// (yaml.v3 MappingNode Content 是 [k1,v1,k2,v2,...])
func childMap(n *yaml.Node, key string) *yaml.Node {
	if n == nil {
		return nil
	}
	// 文档节点要剥一层
	target := n
	if target.Kind == yaml.DocumentNode && len(target.Content) > 0 {
		target = target.Content[0]
	}
	if target.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(target.Content); i += 2 {
		if target.Content[i].Value == key {
			return target.Content[i+1]
		}
	}
	return nil
}

// seqItem 在 SequenceNode 上取 index i 的 item。
func seqItem(n *yaml.Node, i int) *yaml.Node {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	if i < 0 || i >= len(n.Content) {
		return nil
	}
	return n.Content[i]
}

// nodeLine 取节点起始行号, nil 返 0。
func nodeLine(n *yaml.Node) int {
	if n == nil {
		return 0
	}
	return n.Line
}

// nodeCol 列号。
func nodeCol(n *yaml.Node) int {
	if n == nil {
		return 0
	}
	return n.Column
}

// ---- 错误类型 ----

// ErrKind 错误大类。
type ErrKind string

const (
	ErrSyntax   ErrKind = "syntax"
	ErrSchema   ErrKind = "schema"
	ErrSemantic ErrKind = "semantic"
)

// Error 统一错误结构, JSON 序列化给前端编辑器高亮。
type Error struct {
	Kind    ErrKind `json:"kind"`
	Message string  `json:"message"`
	Path    string  `json:"path,omitempty"` // e.g. "stages[2].needs[0]"
	Line    int     `json:"line,omitempty"`
	Column  int     `json:"column,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil dsl error>"
	}
	loc := ""
	if e.Line > 0 {
		loc = fmt.Sprintf(" (line %d", e.Line)
		if e.Column > 0 {
			loc += fmt.Sprintf(":%d", e.Column)
		}
		loc += ")"
	}
	if e.Path != "" {
		return fmt.Sprintf("[%s] %s at %s%s", e.Kind, e.Message, e.Path, loc)
	}
	return fmt.Sprintf("[%s] %s%s", e.Kind, e.Message, loc)
}

// Errors 集合, 同时实现 error 接口 (返第一个 message 便于日志)。
type Errors []*Error

func (es Errors) Error() string {
	if len(es) == 0 {
		return "no errors"
	}
	if len(es) == 1 {
		return es[0].Error()
	}
	return fmt.Sprintf("%d errors; first: %s", len(es), es[0].Error())
}

// wrapYAMLErr 把 yaml.v3 错误包成 dsl.Error, 抽出 line 信息 (yaml.v3 错误格式: "yaml: line N: ...")
func wrapYAMLErr(err error, phase string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	line := 0
	// 抠 "yaml: line N: ..." 这种格式
	if i := strings.Index(msg, "line "); i >= 0 {
		rest := msg[i+5:]
		for j, c := range rest {
			if c < '0' || c > '9' {
				if j > 0 {
					_, scanErr := fmt.Sscanf(rest[:j], "%d", &line)
					if scanErr != nil {
						line = 0
					}
				}
				break
			}
		}
	}

	// 多错误 (yaml.TypeError)
	var te *yaml.TypeError
	if errors.As(err, &te) {
		// 把每条错误都包成 Error 列出 (但当前签名返单错; 把首条带上, 全文塞到 message)
		return &Error{
			Kind:    ErrSchema,
			Message: phase + ": " + strings.Join(te.Errors, "; "),
			Line:    line,
		}
	}
	return &Error{
		Kind:    ErrSyntax,
		Message: phase + ": " + msg,
		Line:    line,
	}
}
