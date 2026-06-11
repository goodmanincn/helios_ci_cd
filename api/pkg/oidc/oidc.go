package oidc

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Config holds OIDC configuration for an organization
type Config struct {
	Issuer       string   `json:"issuer"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	RedirectURL  string   `json:"redirect_url"`
	Scopes       []string `json:"scopes"`
}

// Provider represents an OIDC provider instance
type Provider struct {
	config     Config
	provider   *oidc.Provider
	oauth2Cfg  oauth2.Config
	verifier   *oidc.IDTokenVerifier
	httpClient *http.Client
}

// Manager manages OIDC providers for multiple organizations
type Manager struct {
	providers map[string]*Provider
	mu        sync.RWMutex
}

// NewManager creates a new OIDC manager
func NewManager() *Manager {
	return &Manager{
		providers: make(map[string]*Provider),
	}
}

// RegisterProvider registers an OIDC provider for an organization
func (m *Manager) RegisterProvider(orgID string, cfg Config) (*Provider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ctx := context.Background()

	// Create OIDC provider
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider: %w", err)
	}

	// Set up scopes
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}

	// Configure OAuth2
	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes,
	}

	// Create ID token verifier
	verifier := provider.Verifier(&oidc.Config{
		ClientID: cfg.ClientID,
	})

	p := &Provider{
		config:     cfg,
		provider:   provider,
		oauth2Cfg:  oauth2Cfg,
		verifier:   verifier,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}

	m.providers[orgID] = p
	return p, nil
}

// GetProvider gets an OIDC provider for an organization
func (m *Manager) GetProvider(orgID string) (*Provider, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, ok := m.providers[orgID]
	return p, ok
}

// RemoveProvider removes an OIDC provider for an organization
func (m *Manager) RemoveProvider(orgID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.providers, orgID)
}

// AuthCodeURL generates the authorization code URL
func (p *Provider) AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string {
	return p.oauth2Cfg.AuthCodeURL(state, opts...)
}

// Exchange exchanges authorization code for tokens
func (p *Provider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, p.httpClient)
	return p.oauth2Cfg.Exchange(ctx, code)
}

// VerifyIDToken verifies an ID token
func (p *Provider) VerifyIDToken(ctx context.Context, token *oauth2.Token) (*oidc.IDToken, error) {
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in token response")
	}

	ctx = context.WithValue(ctx, oauth2.HTTPClient, p.httpClient)
	return p.verifier.Verify(ctx, rawIDToken)
}

// UserInfo represents user information from OIDC
type UserInfo struct {
	Sub           string   `json:"sub"`
	Name          string   `json:"name,omitempty"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified,omitempty"`
	Groups        []string `json:"groups,omitempty"`
}

// GetUserInfo retrieves user information from OIDC provider
func (p *Provider) GetUserInfo(ctx context.Context, token *oauth2.Token) (*UserInfo, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, p.httpClient)

	// Get user info from provider
	userInfo, err := p.provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	// Parse user info
	var info UserInfo
	if err := userInfo.Claims(&info); err != nil {
		return nil, fmt.Errorf("failed to parse user claims: %w", err)
	}

	return &info, nil
}

// ClaimsFromIDToken extracts custom claims from an ID token
func ClaimsFromIDToken(token *oidc.IDToken, out interface{}) error {
	return token.Claims(out)
}

// GenerateState generates a random state parameter for CSRF protection
func GenerateState() string {
	// In production, use a cryptographically secure random string
	// For now, use a simple timestamp-based approach
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// ValidateRedirectURL validates a redirect URL against the configured one
func (p *Provider) ValidateRedirectURL(redirectURL string) error {
	expected, err := url.Parse(p.config.RedirectURL)
	if err != nil {
		return err
	}

	actual, err := url.Parse(redirectURL)
	if err != nil {
		return err
	}

	if expected.Scheme != actual.Scheme || expected.Host != actual.Host || expected.Path != actual.Path {
		return fmt.Errorf("redirect URL mismatch")
	}

	return nil
}
