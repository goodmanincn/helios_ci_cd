package plugin

import "testing"

// 保证所有 seed 进去的官方插件 action.yml 都能合法 parse,
// 防止后续编辑 seed.go 时手抖把 action.yml 写坏.
func TestOfficialSeed_AllActionYMLValid(t *testing.T) {
	defs := officialBuiltins()
	if len(defs) == 0 {
		t.Fatal("expected at least one official plugin def")
	}
	for _, b := range defs {
		t.Run(b.Namespace+"/"+b.Name+"@"+b.Version, func(t *testing.T) {
			a, errs := ParseActionYAML([]byte(b.ActionYML))
			if len(errs) > 0 {
				t.Fatalf("parse failed: %v", errs)
			}
			if a.Name == "" {
				t.Errorf("name empty")
			}
			if a.Runs.Using == "" {
				t.Errorf("runs.using empty")
			}
		})
	}
}
