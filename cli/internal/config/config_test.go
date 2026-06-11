package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HELIOS_HOME", dir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Profiles) != 0 {
		t.Fatalf("expected empty profiles, got %v", cfg.Profiles)
	}

	cfg.DefaultProfile = "prod"
	cfg.Profiles["prod"] = Profile{Server: "https://x", OrgID: 7}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	// 文件权限必须 0600 (敏感)
	info, err := os.Stat(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("config.yaml permissions: want 0600 got %o", mode)
	}

	cfg2, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.DefaultProfile != "prod" || cfg2.Profiles["prod"].Server != "https://x" || cfg2.Profiles["prod"].OrgID != 7 {
		t.Fatalf("round-trip mismatch: %+v", cfg2)
	}
}

func TestCredentialsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HELIOS_HOME", dir)

	creds, _ := LoadCredentials()
	creds.Tokens["prod"] = Token{AccessToken: "abc.def", Username: "alice"}
	if err := SaveCredentials(creds); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(dir, "credentials.yaml"))
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("credentials.yaml permissions: want 0600 got %o", mode)
	}

	c2, _ := LoadCredentials()
	if c2.Tokens["prod"].AccessToken != "abc.def" || c2.Tokens["prod"].Username != "alice" {
		t.Fatalf("creds round-trip mismatch: %+v", c2)
	}
}

func TestActive_ResolveOrder(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HELIOS_HOME", dir)
	t.Setenv("HELIOS_PROFILE", "")

	// 没 profile 时应报错
	if _, _, _, err := Active(""); err == nil {
		t.Fatal("expected error when no active profile")
	}

	cfg := &Config{
		DefaultProfile: "default",
		Profiles: map[string]Profile{
			"default": {Server: "https://default"},
			"staging": {Server: "https://staging"},
		},
	}
	_ = SaveConfig(cfg)
	_ = SaveCredentials(&Credentials{Tokens: map[string]Token{
		"default": {AccessToken: "t-default"},
		"staging": {AccessToken: "t-staging"},
	}})

	// 1) 默认 → default
	name, prof, tok, err := Active("")
	if err != nil {
		t.Fatal(err)
	}
	if name != "default" || prof.Server != "https://default" || tok.AccessToken != "t-default" {
		t.Errorf("default resolve mismatch: name=%s prof=%+v tok=%+v", name, prof, tok)
	}

	// 2) env 覆盖
	t.Setenv("HELIOS_PROFILE", "staging")
	name, prof, _, _ = Active("")
	if name != "staging" || prof.Server != "https://staging" {
		t.Errorf("env override failed: name=%s prof=%+v", name, prof)
	}

	// 3) flag 优先 env
	name, prof, _, _ = Active("default")
	if name != "default" || prof.Server != "https://default" {
		t.Errorf("flag override failed: name=%s prof=%+v", name, prof)
	}

	// 4) 不存在的 profile
	if _, _, _, err := Active("nosuch"); err == nil {
		t.Fatal("expected error for missing profile")
	}
}
