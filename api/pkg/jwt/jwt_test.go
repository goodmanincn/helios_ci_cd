package jwt_test

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	heliosjwt "github.com/helios-cicd/helios/api/pkg/jwt"
)

func newIssuerT(t *testing.T, accessTTL time.Duration) *heliosjwt.Issuer {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	iss, err := heliosjwt.NewIssuer(heliosjwt.Config{
		PrivateKey: priv,
		PublicKey:  &priv.PublicKey,
		Issuer:     "helios-test",
		AccessTTL:  accessTTL,
		RefreshTTL: 24 * time.Hour,
	})
	require.NoError(t, err)
	return iss
}

func TestIssueAndParseAccess(t *testing.T) {
	iss := newIssuerT(t, 30*time.Minute)
	tok, jti, exp, err := iss.IssueAccess(42, "alice", []int64{1, 2}, []string{"owner"})
	require.NoError(t, err)
	require.NotEmpty(t, tok)
	require.NotEmpty(t, jti)
	require.True(t, exp.After(time.Now()))

	c, err := iss.Parse(tok)
	require.NoError(t, err)
	require.Equal(t, int64(42), c.UserID)
	require.Equal(t, "alice", c.Username)
	require.Equal(t, []int64{1, 2}, c.OrgIDs)
	require.Equal(t, []string{"owner"}, c.Roles)
	require.Equal(t, heliosjwt.TokenTypeAccess, c.TokenType)
	require.Equal(t, jti, c.ID)
}

func TestIssueAndParseRefresh(t *testing.T) {
	iss := newIssuerT(t, 30*time.Minute)
	tok, _, _, err := iss.IssueRefresh(7, "bob")
	require.NoError(t, err)
	c, err := iss.Parse(tok)
	require.NoError(t, err)
	require.Equal(t, heliosjwt.TokenTypeRefresh, c.TokenType)
}

func TestExpiredToken(t *testing.T) {
	// TTL=0 不行 (会用默认),用 1ns
	iss := newIssuerT(t, time.Nanosecond)
	tok, _, _, err := iss.IssueAccess(1, "x", nil, nil)
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)
	_, err = iss.Parse(tok)
	require.ErrorIs(t, err, heliosjwt.ErrTokenExpired)
}

func TestTamperedToken(t *testing.T) {
	iss := newIssuerT(t, 30*time.Minute)
	tok, _, _, err := iss.IssueAccess(1, "x", nil, nil)
	require.NoError(t, err)
	// 修改 payload 末尾一个字符 (header.payload.sig)
	parts := strings.Split(tok, ".")
	require.Len(t, parts, 3)
	// 翻转 payload 第一个字母,签名校验必失败
	b := []byte(parts[1])
	b[0] = 'X'
	parts[1] = string(b)
	bad := strings.Join(parts, ".")
	_, err = iss.Parse(bad)
	require.Error(t, err)
	require.True(t, errors.Is(err, heliosjwt.ErrTokenInvalid))
}

func TestWrongIssuer(t *testing.T) {
	iss := newIssuerT(t, 30*time.Minute)
	tok, _, _, err := iss.IssueAccess(1, "x", nil, nil)
	require.NoError(t, err)

	// 用另一个 issuer 名解析
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	other, _ := heliosjwt.NewIssuer(heliosjwt.Config{
		PrivateKey: priv, PublicKey: &priv.PublicKey, Issuer: "helios-test",
		AccessTTL: time.Minute,
	})
	// other 使用不同公钥 — 签名应该校验失败
	_, err = other.Parse(tok)
	require.Error(t, err)
}

func TestLoadPEM(t *testing.T) {
	// 用项目自带的 .helios/ 密钥 (gen-jwt-keys.sh 已生成)
	priv, err := heliosjwt.LoadPrivateKeyPEM("../../../.helios/jwt-private.pem")
	if err != nil {
		t.Skipf("dev key 缺失,跳过 PEM 加载用例: %v", err)
	}
	pub, err := heliosjwt.LoadPublicKeyPEM("../../../.helios/jwt-public.pem")
	require.NoError(t, err)

	iss, err := heliosjwt.NewIssuer(heliosjwt.Config{
		PrivateKey: priv, PublicKey: pub, Issuer: "helios-dev",
	})
	require.NoError(t, err)
	tok, _, _, err := iss.IssueAccess(1, "loader", nil, nil)
	require.NoError(t, err)
	c, err := iss.Parse(tok)
	require.NoError(t, err)
	require.Equal(t, "loader", c.Username)
}
