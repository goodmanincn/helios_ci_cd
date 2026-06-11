// client_test.go — SSH 客户端单元测试。
package sshrunner

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func generateTestKey(t *testing.T) string {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	b, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: b}
	return string(pem.EncodeToMemory(block))
}

func TestBuildAuthMethods_PasswordOnly(t *testing.T) {
	methods, err := buildAuthMethods(AuthConfig{Password: "secret"})
	if err != nil {
		t.Fatalf("buildAuthMethods: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(methods))
	}
}

func TestBuildAuthMethods_KeyOnly(t *testing.T) {
	key := generateTestKey(t)
	methods, err := buildAuthMethods(AuthConfig{PrivateKey: key})
	if err != nil {
		t.Fatalf("buildAuthMethods: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(methods))
	}
}

func TestBuildAuthMethods_NoAuth(t *testing.T) {
	_, err := buildAuthMethods(AuthConfig{})
	if err == nil {
		t.Fatal("expected error for empty auth")
	}
}

func TestParsePrivateKey_WithPassphrase(t *testing.T) {
	key := generateTestKey(t)
	// 使用无 passphrase 的 key 测试 passphrase 路径会报错
	_, err := parsePrivateKey(key, "wrong")
	if err == nil {
		t.Fatal("expected error for wrong passphrase")
	}
}

func TestParsePrivateKey_WithoutPassphrase(t *testing.T) {
	key := generateTestKey(t)
	signer, err := parsePrivateKey(key, "")
	if err != nil {
		t.Fatalf("parsePrivateKey: %v", err)
	}
	if signer == nil {
		t.Fatal("expected non-nil signer")
	}
}
