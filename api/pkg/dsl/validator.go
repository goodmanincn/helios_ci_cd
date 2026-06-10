// Package dsl — validator.go: schema + 语义校验, 单遍累积错误, 带 yaml.Node 行号。
//
// 设计偏离 spec/04 注意:
//   - spec 要求引入 santhosh-tekuri/jsonschema/v5 做 JSON Schema 校验。
//   - 这里选择 *基于 yaml.Node + 手写规则* 的等价方案, 原因:
//       1) 已经持有 yaml.Node 完整 AST, 错位精确到 line:col 比 JSON Schema 反查更准
//       2) 语义错 (needs 引用不存在 / 表达式不合法) 必然要走第二遍手写, 与其拆两层不如合一
//       3) 少引入一个依赖 (用户在 CLAUDE.md 也提"新依赖添加前先确认必要性")
//   - 校验规则覆盖原 spec/04 § 4.3 校验级别 1-4 (语法/Schema/语义/DAG), 资源校验 (level 5)
//     需要 cluster/runner registry 留 M3.
//
// 校验级别 (ValidateRaw 串行执行):
//   1. PARSE - 走 Parse(), 失败立即返 (没法做后续)
//   2. STRUCT - 字段级强约束: version / name / stages 非空 / 每个 stage 有 id 等
//   3. CROSS - 跨字段: id 唯一 / needs 引用存在 / type=approval 必须有 approvers ...
//   4. EXPR  - 表达式语法粗扫: ${{ ... }} 配对 + 简单引用合法性 (不做完整 lexer, 留 T2.3.1)
//   5. DAG   - 拓扑能否完成 (无环 + 全部可达; 真正 DAG 算法在 engine 里)
//
// 所有规则错误累积, 不 fail-fast (除非 PARSE 整个 doc 死)。
package dsl

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ValidateRaw 是对外 API: yaml bytes → (pipeline, errors)。
// 即便有错误也尽量返回 pipeline (供 UI 显示已解析部分)。
//
// 调用方:
//   - HTTP /pipelines/validate (PipelineHandler.validate)
//   - 编辑器 debounce 后端实时校验
//   - CLI lint
func ValidateRaw(src []byte) (*Pipeline, Errors) {
	r, err := Parse(src)
	if err != nil {
		// 把单一 dsl.Error / yaml syntax 包成 Errors
		if de, ok := err.(*Error); ok {
			return nil, Errors{de}
		}
		return nil, Errors{{Kind: ErrSyntax, Message: err.Error()}}
	}
	return ValidateParsed(r)
}

// ValidateParsed 已经 Parse 过的 result 走后续 4 级校验。
func ValidateParsed(r *ParseResult) (*Pipeline, Errors) {
	if r == nil || r.Pipeline == nil {
		return nil, Errors{{Kind: ErrSyntax, Message: "empty parse result"}}
	}
	var es Errors
	es = append(es, validateStruct(r)...)
	es = append(es, validateCross(r)...)
	es = append(es, validateExpressions(r)...)
	es = append(es, validateDAG(r)...)
	return r.Pipeline, es
}

// ---- 2. STRUCT 字段约束 ----

var (
	idPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
)

func validateStruct(r *ParseResult) Errors {
	var es Errors
	p := r.Pipeline

	if strings.TrimSpace(p.Version) == "" {
		es = append(es, mkErr(ErrSchema, "version is required", "version",
			childMap(r.Root, "version")))
	} else if p.Version != "1" {
		es = append(es, mkErr(ErrSchema,
			fmt.Sprintf("unsupported version %q, only \"1\" is supported", p.Version),
			"version", childMap(r.Root, "version")))
	}

	if strings.TrimSpace(p.Name) == "" {
		es = append(es, mkErr(ErrSchema, "name is required", "name",
			childMap(r.Root, "name")))
	}

	if len(p.Stages) == 0 {
		es = append(es, mkErr(ErrSchema, "stages must have at least one item", "stages", r.StagesNode))
		return es // 没 stage 后续都没意义
	}

	for i, s := range p.Stages {
		stagePath := fmt.Sprintf("stages[%d]", i)
		stageNode := seqItem(r.StagesNode, i)

		// id 必填 + 命名规则
		if strings.TrimSpace(s.ID) == "" {
			es = append(es, mkErr(ErrSchema, "stage id is required",
				stagePath+".id", childMap(stageNode, "id")))
		} else if !idPattern.MatchString(s.ID) {
			es = append(es, mkErr(ErrSchema,
				fmt.Sprintf("stage id %q invalid (must match %s)", s.ID, idPattern.String()),
				stagePath+".id", childMap(stageNode, "id")))
		}

		// type 仅允许 "" 或 "approval"
		switch s.Type {
		case "", "approval":
			// ok
		default:
			es = append(es, mkErr(ErrSchema,
				fmt.Sprintf("stage type %q invalid (allowed: \"\", approval)", s.Type),
				stagePath+".type", childMap(stageNode, "type")))
		}

		if s.Type == "approval" {
			// approval 必带 approvers, 且不能有 steps/uses
			if len(s.Approvers) == 0 {
				es = append(es, mkErr(ErrSchema, "approval stage must declare approvers",
					stagePath+".approvers", childMap(stageNode, "approvers")))
			}
			if len(s.Steps) > 0 {
				es = append(es, mkErr(ErrSchema, "approval stage must not have steps",
					stagePath+".steps", childMap(stageNode, "steps")))
			}
			if s.Uses != "" {
				es = append(es, mkErr(ErrSchema, "approval stage must not have uses",
					stagePath+".uses", childMap(stageNode, "uses")))
			}
			if s.Mode != "" && !inSet(s.Mode, "any", "all", "quorum") {
				es = append(es, mkErr(ErrSchema,
					fmt.Sprintf("approval mode %q invalid (allowed: any, all, quorum)", s.Mode),
					stagePath+".mode", childMap(stageNode, "mode")))
			}
			// timeout 格式 (Go time.ParseDuration: 30s/5m/1h/24h). 空值合法 = 永不超时.
			if s.Timeout != "" {
				if _, perr := time.ParseDuration(s.Timeout); perr != nil {
					es = append(es, mkErr(ErrSchema,
						fmt.Sprintf("approval timeout %q invalid (expect Go duration like 30s, 5m, 1h): %v", s.Timeout, perr),
						stagePath+".timeout", childMap(stageNode, "timeout")))
				}
			}
			// on_timeout 枚举 (空值合法 = ApprovalService 跑时 fallback reject).
			if s.OnTimeout != "" && !inSet(s.OnTimeout, "reject", "approve", "pause") {
				es = append(es, mkErr(ErrSchema,
					fmt.Sprintf("approval on_timeout %q invalid (allowed: reject, approve, pause)", s.OnTimeout),
					stagePath+".on_timeout", childMap(stageNode, "on_timeout")))
			}
		} else {
			// 普通 stage: steps 或 uses 必有其一 (但不能同时)
			if len(s.Steps) == 0 && s.Uses == "" {
				es = append(es, mkErr(ErrSchema, "stage must have either steps or uses",
					stagePath, stageNode))
			}
			if len(s.Steps) > 0 && s.Uses != "" {
				es = append(es, mkErr(ErrSchema, "stage cannot have both steps and uses",
					stagePath, stageNode))
			}
		}

		// 每个 step: run xor uses
		stepsNode := childMap(stageNode, "steps")
		for j, st := range s.Steps {
			stepPath := fmt.Sprintf("%s.steps[%d]", stagePath, j)
			stepNode := seqItem(stepsNode, j)
			if st.Run == "" && st.Uses == "" {
				es = append(es, mkErr(ErrSchema, "step must have either run or uses",
					stepPath, stepNode))
			}
			if st.Run != "" && st.Uses != "" {
				es = append(es, mkErr(ErrSchema, "step cannot have both run and uses",
					stepPath, stepNode))
			}
		}
	}
	return es
}

// ---- 3. CROSS 跨字段: id 唯一, needs 引用存在 ----

func validateCross(r *ParseResult) Errors {
	var es Errors
	p := r.Pipeline
	if len(p.Stages) == 0 {
		return es
	}

	ids := make(map[string]int, len(p.Stages)) // id → 首次出现下标
	for i, s := range p.Stages {
		if s.ID == "" {
			continue
		}
		if prev, dup := ids[s.ID]; dup {
			node := seqItem(r.StagesNode, i)
			es = append(es, mkErr(ErrSemantic,
				fmt.Sprintf("duplicate stage id %q (also at stages[%d])", s.ID, prev),
				fmt.Sprintf("stages[%d].id", i), childMap(node, "id")))
			continue
		}
		ids[s.ID] = i
	}

	for i, s := range p.Stages {
		stagePath := fmt.Sprintf("stages[%d]", i)
		stageNode := seqItem(r.StagesNode, i)
		needsNode := childMap(stageNode, "needs")
		for k, nid := range s.Needs {
			if _, ok := ids[nid]; !ok {
				es = append(es, mkErr(ErrSemantic,
					fmt.Sprintf("needs references unknown stage %q", nid),
					fmt.Sprintf("%s.needs[%d]", stagePath, k), seqItem(needsNode, k)))
			}
			if nid == s.ID {
				es = append(es, mkErr(ErrSemantic,
					fmt.Sprintf("stage %q cannot need itself", s.ID),
					fmt.Sprintf("%s.needs[%d]", stagePath, k), seqItem(needsNode, k)))
			}
		}
	}
	return es
}

// ---- 4. EXPR ${{ ... }} 粗扫 ----

var (
	// 找出所有 ${{ ... }} (允许嵌套引号, 不允许跨行)
	exprPattern = regexp.MustCompile(`\$\{\{[^}]*\}\}`)
	// 允许的 context 根 (与 spec/04 § 4.2 一致)
	allowedRefRoots = map[string]bool{
		"vars": true, "env": true, "inputs": true, "secrets": true,
		"matrix": true, "needs": true, "github": true, "run": true, "steps": true,
	}
	// 找到 a.b / a.b.c 引用 (粗 lexer)
	refPattern = regexp.MustCompile(`\b([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_.-]*)`)
)

func validateExpressions(r *ParseResult) Errors {
	var es Errors
	p := r.Pipeline

	check := func(path string, s string, node *yaml.Node) {
		// 检查 ${{ 配对
		oc := strings.Count(s, "${{")
		cc := strings.Count(s, "}}")
		if oc != cc {
			es = append(es, mkErr(ErrSemantic,
				fmt.Sprintf("unbalanced ${{ }} delimiters (open=%d close=%d)", oc, cc),
				path, node))
			return
		}
		// 提取每个表达式扫引用
		for _, expr := range exprPattern.FindAllString(s, -1) {
			inner := strings.TrimSpace(expr[3 : len(expr)-2])
			if inner == "" {
				es = append(es, mkErr(ErrSemantic, "empty expression ${{ }}", path, node))
				continue
			}
			for _, m := range refPattern.FindAllStringSubmatch(inner, -1) {
				root := m[1]
				if !allowedRefRoots[root] {
					es = append(es, mkErr(ErrSemantic,
						fmt.Sprintf("unknown reference root %q (allowed: %s)",
							root, joinKeys(allowedRefRoots)),
						path, node))
				}
			}
		}
	}

	// 顶层 env / variables
	if envNode := childMap(r.Root, "env"); envNode != nil {
		for k, v := range p.Env {
			check("env."+k, v, childMap(envNode, k))
		}
	}
	if varsNode := childMap(r.Root, "variables"); varsNode != nil {
		for k, v := range p.Variables {
			check("variables."+k, v, childMap(varsNode, k))
		}
	}

	// stage 内各种字符串字段
	for i, s := range p.Stages {
		stagePath := fmt.Sprintf("stages[%d]", i)
		stageNode := seqItem(r.StagesNode, i)

		if s.If != "" {
			check(stagePath+".if", s.If, childMap(stageNode, "if"))
		}
		if s.RunsOn != nil && s.RunsOn.Image != "" {
			check(stagePath+".runs-on.image", s.RunsOn.Image, nil)
		}
		for k, v := range s.Env {
			check(stagePath+".env."+k, v, nil)
		}
		for k, v := range s.Outputs {
			check(stagePath+".outputs."+k, v, nil)
		}
		stepsNode := childMap(stageNode, "steps")
		for j, st := range s.Steps {
			stepPath := fmt.Sprintf("%s.steps[%d]", stagePath, j)
			stepNode := seqItem(stepsNode, j)
			if st.Run != "" {
				check(stepPath+".run", st.Run, childMap(stepNode, "run"))
			}
			if st.If != "" {
				check(stepPath+".if", st.If, childMap(stepNode, "if"))
			}
			for k, v := range st.Env {
				check(fmt.Sprintf("%s.env.%s", stepPath, k), v, nil)
			}
			for k, v := range st.With {
				if sv, ok := v.(string); ok {
					check(fmt.Sprintf("%s.with.%s", stepPath, k), sv, nil)
				}
			}
		}
		// stage-level with (uses 复合插件)
		for k, v := range s.With {
			if sv, ok := v.(string); ok {
				check(fmt.Sprintf("%s.with.%s", stagePath, k), sv, nil)
			}
		}
	}
	return es
}

// ---- 5. DAG 粗扫: 环 + 不可达 ----

func validateDAG(r *ParseResult) Errors {
	var es Errors
	p := r.Pipeline
	if len(p.Stages) == 0 {
		return es
	}

	// 构图 (仅引用已存在的 id, 防止 nil 节点污染)
	ids := make(map[string]bool, len(p.Stages))
	for _, s := range p.Stages {
		if s.ID != "" {
			ids[s.ID] = true
		}
	}
	adj := make(map[string][]string, len(p.Stages))
	for _, s := range p.Stages {
		for _, n := range s.Needs {
			if ids[n] {
				adj[s.ID] = append(adj[s.ID], n)
			}
		}
	}

	// 环检测: DFS 白/灰/黑
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(p.Stages))
	var stack []string

	var dfs func(id string) []string // 返环路径 (含 id) 或 nil
	dfs = func(id string) []string {
		color[id] = gray
		stack = append(stack, id)
		for _, dep := range adj[id] {
			switch color[dep] {
			case gray:
				// 找到环
				idx := -1
				for k, x := range stack {
					if x == dep {
						idx = k
						break
					}
				}
				if idx < 0 {
					return []string{dep, id}
				}
				return append([]string(nil), append(stack[idx:], dep)...)
			case white:
				if cycle := dfs(dep); cycle != nil {
					return cycle
				}
			}
		}
		color[id] = black
		stack = stack[:len(stack)-1]
		return nil
	}

	reported := make(map[string]bool) // 同一环只报一次
	for id := range ids {
		if color[id] == white {
			if cycle := dfs(id); cycle != nil {
				key := strings.Join(cycle, "→")
				if !reported[key] {
					reported[key] = true
					es = append(es, &Error{
						Kind: ErrSemantic,
						Message: fmt.Sprintf("cycle detected: %s",
							strings.Join(cycle, " → ")),
						Line: 1, // 环检测没有单点位置, 指向 doc 起始
					})
				}
			}
		}
	}
	return es
}

// ---- helpers ----

func mkErr(kind ErrKind, msg, path string, node *yaml.Node) *Error {
	line := nodeLine(node)
	col := nodeCol(node)
	// 找不到节点 (missing key 的常见情况): 回退到文档起始, 编辑器至少能高亮到顶部
	// 而不是 0 (没意义)。
	if line == 0 {
		line = 1
	}
	return &Error{
		Kind:    kind,
		Message: msg,
		Path:    path,
		Line:    line,
		Column:  col,
	}
}

func inSet(v string, opts ...string) bool {
	for _, o := range opts {
		if v == o {
			return true
		}
	}
	return false
}

func joinKeys(m map[string]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// 简单排序避免输出不稳定 (单测会断言这段)
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return strings.Join(keys, ", ")
}
