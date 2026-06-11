package builtintmpl

import (
	"testing"

	"github.com/helios-cicd/helios/api/pkg/dsl"
)

// TestBuiltinTemplatesAreValid — 所有内置模板的 YAML 必须能通过 DSL 校验.
// 这是启动时 seed 的硬性前置, 失败 = 启动直接 fail-fast.
func TestBuiltinTemplatesAreValid(t *testing.T) {
	for _, tmpl := range builtins() {
		t.Run(tmpl.Slug, func(t *testing.T) {
			_, errs := dsl.ValidateRaw([]byte(tmpl.SpecRaw))
			if len(errs) > 0 {
				t.Fatalf("builtin %q failed DSL validation: %v", tmpl.Slug, errs)
			}
		})
	}
}

func TestBuiltinsHaveUniqueSlugs(t *testing.T) {
	seen := map[string]bool{}
	for _, tmpl := range builtins() {
		if seen[tmpl.Slug] {
			t.Errorf("duplicate slug: %s", tmpl.Slug)
		}
		seen[tmpl.Slug] = true
	}
}
