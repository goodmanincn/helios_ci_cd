// Package plugin — parse.go: action.yml → Action 结构.
package plugin

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseActionYAML 解析 action.yml 字节流.
//
// 返回:
//   - action: 解析后结构 (即使 errs 非空, 字段也尽量填上, 让 UI 能部分预览)
//   - errs:   YAML 语法错 + Validate() 全部语义错
//
// 不严格拒绝未知字段 — 留给将来扩展 (Action 协议是用户可见契约).
func ParseActionYAML(src []byte) (*Action, []error) {
	if len(src) == 0 {
		return nil, []error{errf("empty action.yml")}
	}
	var a Action
	if err := yaml.Unmarshal(src, &a); err != nil {
		return nil, []error{wrapYAMLErr(err)}
	}
	if errs := a.Validate(); len(errs) > 0 {
		return &a, errs
	}
	return &a, nil
}

// errf 短包装.
func errf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

// wrapYAMLErr 把 yaml.v3 的 TypeError / 普通错统一成单个 error, 行号挂在 message 里.
func wrapYAMLErr(err error) error {
	if err == nil {
		return nil
	}
	var te *yaml.TypeError
	if errors.As(err, &te) {
		return errf("yaml: %s", strings.Join(te.Errors, "; "))
	}
	return errf("yaml: %s", err.Error())
}

// ----- uses 引用解析 -----

// Ref 解析后的 uses 引用.
type Ref struct {
	Namespace string // helios / acme / github.com (host 前缀也走这)
	Name      string
	Version   string // v1 / 1.2.3 / latest
	Local     bool   // ./local-action 时 true; Namespace/Name 不填, Path 存 Version 字段对应路径
	Path      string // Local=true 时的本地路径
}

// ParseRef 把 `uses:` 字面解析成 Ref.
//
// 支持:
//   - helios/echo@v1
//   - acme/foo@1.2.3
//   - acme/foo@latest
//   - ./local-action       (Local=true)
//
// 显式不支持本轮:
//   - github.com/foo/bar@v1 — 返 error (留接入点)
//
// 错误时返 (nil, error), caller 负责提示用户.
func ParseRef(uses string) (*Ref, error) {
	uses = strings.TrimSpace(uses)
	if uses == "" {
		return nil, errf("uses: empty ref")
	}
	if strings.HasPrefix(uses, "./") || strings.HasPrefix(uses, "../") {
		return &Ref{Local: true, Path: uses}, nil
	}
	// 本轮 github.com/x/y@v1 拒绝, 但显式留下友好错
	if strings.HasPrefix(uses, "github.com/") {
		return nil, errf("uses: third-party github.com/<repo> not yet supported (MVP)")
	}
	atIdx := strings.LastIndex(uses, "@")
	if atIdx < 0 {
		return nil, errf("uses: missing @version (got %q)", uses)
	}
	left := uses[:atIdx]
	version := uses[atIdx+1:]
	if version == "" {
		return nil, errf("uses: empty version after @ in %q", uses)
	}
	parts := strings.SplitN(left, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, errf("uses: expected <namespace>/<name>@<version> (got %q)", uses)
	}
	return &Ref{
		Namespace: parts[0],
		Name:      parts[1],
		Version:   version,
	}, nil
}

// Slug 返回 namespace/name (无 version), 用于查 plugins 表.
func (r *Ref) Slug() string {
	if r == nil || r.Local {
		return ""
	}
	return r.Namespace + "/" + r.Name
}
