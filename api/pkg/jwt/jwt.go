// Package jwt 封装 helios 的 access/refresh token 签发与校验。
// 算法: RS256 (RSA-SHA256)。
package jwt

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
)

const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

var (
	ErrTokenExpired  = errors.New("token expired")
	ErrTokenInvalid  = errors.New("token invalid")
	ErrUnexpectedAlg = errors.New("unexpected signing algorithm")
)

// Claims 是 helios 自定义 claim 集合。
type Claims struct {
	jwtv5.RegisteredClaims
	UserID    int64    `json:"uid"`
	Username  string   `json:"uname,omitempty"`
	OrgIDs    []int64  `json:"orgs,omitempty"` // 简化:用户所属 org 列表
	Roles     []string `json:"roles,omitempty"`
	TokenType string   `json:"typ"` // access / refresh
}

// Config 描述 issuer 配置。
type Config struct {
	PrivateKey *rsa.PrivateKey
	PublicKey  *rsa.PublicKey
	Issuer     string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

// Issuer 负责签发 / 解析 token。线程安全。
type Issuer struct {
	cfg Config
}

func NewIssuer(cfg Config) (*Issuer, error) {
	if cfg.PrivateKey == nil || cfg.PublicKey == nil {
		return nil, errors.New("jwt: private/public key required")
	}
	if cfg.Issuer == "" {
		cfg.Issuer = "helios"
	}
	if cfg.AccessTTL == 0 {
		cfg.AccessTTL = 30 * time.Minute
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = 7 * 24 * time.Hour
	}
	return &Issuer{cfg: cfg}, nil
}

// IssueAccess 签发 access token,返回 (token, jti, expiresAt, err)。
func (i *Issuer) IssueAccess(userID int64, username string, orgs []int64, roles []string) (string, string, time.Time, error) {
	return i.issue(TokenTypeAccess, i.cfg.AccessTTL, userID, username, orgs, roles)
}

// IssueRefresh 签发 refresh token。
func (i *Issuer) IssueRefresh(userID int64, username string) (string, string, time.Time, error) {
	return i.issue(TokenTypeRefresh, i.cfg.RefreshTTL, userID, username, nil, nil)
}

func (i *Issuer) issue(typ string, ttl time.Duration, userID int64, username string, orgs []int64, roles []string) (string, string, time.Time, error) {
	jti, err := randJTI()
	if err != nil {
		return "", "", time.Time{}, err
	}
	now := time.Now().UTC()
	exp := now.Add(ttl)
	claims := Claims{
		RegisteredClaims: jwtv5.RegisteredClaims{
			Issuer:    i.cfg.Issuer,
			Subject:   fmt.Sprintf("%d", userID),
			IssuedAt:  jwtv5.NewNumericDate(now),
			NotBefore: jwtv5.NewNumericDate(now),
			ExpiresAt: jwtv5.NewNumericDate(exp),
			ID:        jti,
		},
		UserID:    userID,
		Username:  username,
		OrgIDs:    orgs,
		Roles:     roles,
		TokenType: typ,
	}
	t := jwtv5.NewWithClaims(jwtv5.SigningMethodRS256, claims)
	s, err := t.SignedString(i.cfg.PrivateKey)
	if err != nil {
		return "", "", time.Time{}, err
	}
	return s, jti, exp, nil
}

// Parse 解析并校验 token。返回 claims (含 jti / exp / type)。
func (i *Issuer) Parse(tokenStr string) (*Claims, error) {
	parsed, err := jwtv5.ParseWithClaims(tokenStr, &Claims{}, func(t *jwtv5.Token) (any, error) {
		if _, ok := t.Method.(*jwtv5.SigningMethodRSA); !ok {
			return nil, ErrUnexpectedAlg
		}
		return i.cfg.PublicKey, nil
	}, jwtv5.WithIssuer(i.cfg.Issuer), jwtv5.WithValidMethods([]string{"RS256"}))

	if err != nil {
		if errors.Is(err, jwtv5.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: %v", ErrTokenInvalid, err)
	}
	c, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, ErrTokenInvalid
	}
	return c, nil
}

func randJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ===== PEM 加载工具 =====

// LoadPrivateKeyPEM 从文件读 PKCS#1 或 PKCS#8 私钥。
func LoadPrivateKeyPEM(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("jwt: invalid PEM (private)")
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	any, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("jwt: parse private key: %w", err)
	}
	k, ok := any.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("jwt: private key not RSA")
	}
	return k, nil
}

// LoadPublicKeyPEM 从文件读公钥 (PKIX)。
func LoadPublicKeyPEM(path string) (*rsa.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("jwt: invalid PEM (public)")
	}
	any, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("jwt: parse public key: %w", err)
	}
	k, ok := any.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("jwt: public key not RSA")
	}
	return k, nil
}
