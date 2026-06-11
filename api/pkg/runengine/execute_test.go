package runengine

import (
	"reflect"
	"testing"
)

func TestCheckSecretsAuthorized_AllowedViaStageSecrets(t *testing.T) {
	missing := checkSecretsAuthorized(
		[]string{"DINGTALK_WEBHOOK"},
		[]string{"DINGTALK_WEBHOOK"}, // stage.secrets
		nil,
	)
	if len(missing) != 0 {
		t.Errorf("expected no missing, got %v", missing)
	}
}

func TestCheckSecretsAuthorized_AllowedViaStepWith(t *testing.T) {
	// 用户在 step.with 里把 secret 引上去 (key 名小写, expr 引用 ${{secrets.X}})
	// — 大小写无关命中
	missing := checkSecretsAuthorized(
		[]string{"API_TOKEN"},
		nil,
		map[string]any{"api_token": "${{ secrets.API_TOKEN }}"},
	)
	if len(missing) != 0 {
		t.Errorf("expected no missing, got %v", missing)
	}
}

func TestCheckSecretsAuthorized_MissingReported(t *testing.T) {
	missing := checkSecretsAuthorized(
		[]string{"A", "B", "C"},
		[]string{"A"},
		map[string]any{"b": "x"},
	)
	want := []string{"C"}
	if !reflect.DeepEqual(missing, want) {
		t.Errorf("got %v want %v", missing, want)
	}
}

func TestCheckSecretsAuthorized_NoNeedsNoMissing(t *testing.T) {
	if got := checkSecretsAuthorized(nil, nil, nil); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestParseSetOutputs_MultipleAndOverwrite(t *testing.T) {
	stdout := []byte(`
hello world
::set-output name=foo::bar
::set-output name=foo::baz
::set-output name=count::42
junk after
`)
	got := parseSetOutputs(stdout)
	if got["foo"] != "baz" {
		t.Errorf("foo overwrite failed: %v", got["foo"])
	}
	if got["count"] != "42" {
		t.Errorf("count: %v", got["count"])
	}
}

func TestParseSetOutputs_EmptyNameIgnored(t *testing.T) {
	got := parseSetOutputs([]byte("::set-output name=::ignored\n::set-output name=ok::yes\n"))
	if _, has := got[""]; has {
		t.Errorf("empty name should be ignored")
	}
	if got["ok"] != "yes" {
		t.Errorf("ok: %v", got["ok"])
	}
}

func TestParseSetOutputs_NoMarker(t *testing.T) {
	got := parseSetOutputs([]byte("plain line\nanother\n"))
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}
