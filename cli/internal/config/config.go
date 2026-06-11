// Package config — helios CLI 配置文件 + 凭据持久化。
//
// 配置文件: $HELIOS_CONFIG 或 ~/.helios/config.yaml
// 凭据(token)和 server URL 单独保存到 $HELIOS_HOME 或 ~/.helios/credentials.yaml
// 分两个文件是因为 credentials 权限更敏感 (0600),
// 而 config.yaml 用户可能想 commit 到 dotfiles 仓库。
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config 用户全局偏好。
type Config struct {
	DefaultProfile string             `yaml:"default_profile,omitempty"`
	Profiles       map[string]Profile `yaml:"profiles,omitempty"`
}

// Profile 一个 server 端点 + 标识 (不含 token)。
type Profile struct {
	Server string `yaml:"server"`
	OrgID  int64  `yaml:"org_id,omitempty"`
}

// Credentials 与 Config 同结构, 但存 token (永远 0600 权限)。
type Credentials struct {
	Tokens map[string]Token `yaml:"tokens,omitempty"`
}

// Token 一个 profile 的访问凭据.
type Token struct {
	AccessToken  string `yaml:"access_token"`
	RefreshToken string `yaml:"refresh_token,omitempty"`
	Username     string `yaml:"username,omitempty"`
}

// Dir 返回 helios CLI 配置目录, 自动创建。
func Dir() (string, error) {
	if v := os.Getenv("HELIOS_HOME"); v != "" {
		return ensureDir(v)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(home, ".helios"))
}

func ensureDir(d string) (string, error) {
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}

// LoadConfig 读 ~/.helios/config.yaml, 不存在返回零值 Config。
func LoadConfig() (*Config, error) {
	d, err := Dir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(d, "config.yaml")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{Profiles: map[string]Profile{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	return &c, nil
}

// SaveConfig 写入, 文件模式 0600 (含 org_id 等可能敏感)。
func SaveConfig(c *Config) error {
	d, err := Dir()
	if err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(d, "config.yaml"), b, 0o600)
}

// LoadCredentials 同 LoadConfig, 但读 credentials.yaml。
func LoadCredentials() (*Credentials, error) {
	d, err := Dir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(d, "credentials.yaml")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Credentials{Tokens: map[string]Token{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var creds Credentials
	if err := yaml.Unmarshal(b, &creds); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if creds.Tokens == nil {
		creds.Tokens = map[string]Token{}
	}
	return &creds, nil
}

func SaveCredentials(c *Credentials) error {
	d, err := Dir()
	if err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(d, "credentials.yaml"), b, 0o600)
}

// Active 返回当前生效的 profile + token。优先级:
//
//	1. --profile flag (CLI 层处理, 转成参数传入)
//	2. $HELIOS_PROFILE 环境变量
//	3. config.default_profile
//
// 任一找不到返 error, CLI 提示用户跑 `helios login`.
func Active(profileOverride string) (string, Profile, Token, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return "", Profile{}, Token{}, err
	}
	creds, err := LoadCredentials()
	if err != nil {
		return "", Profile{}, Token{}, err
	}

	name := profileOverride
	if name == "" {
		name = os.Getenv("HELIOS_PROFILE")
	}
	if name == "" {
		name = cfg.DefaultProfile
	}
	if name == "" {
		return "", Profile{}, Token{}, fmt.Errorf("no active profile; run `helios login` first or set HELIOS_PROFILE")
	}

	prof, ok := cfg.Profiles[name]
	if !ok {
		return name, Profile{}, Token{}, fmt.Errorf("profile %q not found in %s/config.yaml", name, mustDir())
	}
	tok := creds.Tokens[name] // 可能为零值, 让调用方决定怎么提示
	return name, prof, tok, nil
}

func mustDir() string {
	d, _ := Dir()
	return d
}
