package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/repository"
)

// Service wraps an OIDC provider and OAuth2 config for admin SSO.
type Service struct {
	cfg      config.OIDCConfig
	provider *oidc.Provider
	oauth2   oauth2.Config
	client   *http.Client
	store    repository.Store
}

// NewService creates an OIDC service from config. It eagerly discovers provider endpoints.
func NewService(cfg config.OIDCConfig, store repository.Store) (*Service, error) {
	if !cfg.Enabled {
		return &Service{cfg: cfg, store: store, client: http.DefaultClient}, nil
	}
	if cfg.IssuerURL == "" || cfg.ClientID == "" || cfg.RedirectURL == "" {
		return nil, fmt.Errorf("oidc: issuerURL, clientID and redirectURL are required")
	}

	ctx := oidc.ClientContext(context.Background(), http.DefaultClient)
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "email", "profile"}
	}

	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes,
	}

	return &Service{
		cfg:      cfg,
		provider: provider,
		oauth2:   oauth2Cfg,
		client:   http.DefaultClient,
		store:    store,
	}, nil
}

// Enabled reports whether OIDC is configured.
func (s *Service) Enabled() bool {
	return s.cfg.Enabled
}

// PostLoginURL returns the SPA URL to redirect to after a successful OIDC callback.
func (s *Service) PostLoginURL() string {
	return s.cfg.PostLoginURL
}

// PKCE holds code verifier/challenge pair.
type PKCE struct {
	Verifier  string
	Challenge string
	Method    string
}

// GeneratePKCE creates a new PKCE pair.
func GeneratePKCE() (*PKCE, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate pkce: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return &PKCE{Verifier: verifier, Challenge: challenge, Method: "S256"}, nil
}

// GenerateState creates a random state parameter.
func GenerateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GenerateNonce creates a random nonce parameter.
func GenerateNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// AuthCodeURL returns the OIDC authorization URL and the state that was used.
func (s *Service) AuthCodeURL(pkce *PKCE, state, nonce string) string {
	if pkce == nil {
		pkce = &PKCE{}
	}
	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_challenge", pkce.Challenge),
		oauth2.SetAuthURLParam("code_challenge_method", pkce.Method),
	}
	if nonce != "" {
		opts = append(opts, oidc.Nonce(nonce))
	}
	return s.oauth2.AuthCodeURL(state, opts...)
}

// ExchangeResult holds the outcome of an OIDC code exchange.
type ExchangeResult struct {
	IDToken     *oidc.IDToken
	Claims      *IDTokenClaims
	AccessToken string
	RefreshToken string
}

// IDTokenClaims are the claims we extract from the ID token.
type IDTokenClaims struct {
	Subject   string `json:"sub"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	GivenName string `json:"given_name"`
	Nonce     string `json:"nonce"`
}

// Exchange converts an authorization code into verified user claims.
func (s *Service) Exchange(ctx context.Context, code, redirectURL, codeVerifier, nonce string) (*ExchangeResult, error) {
	if !s.cfg.Enabled || s.provider == nil {
		return nil, fmt.Errorf("oidc not enabled")
	}

	cfg := s.oauth2
	if redirectURL != "" {
		cfg.RedirectURL = redirectURL
	}

	opts := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_verifier", codeVerifier),
	}
	oauth2Token, err := cfg.Exchange(ctx, code, opts...)
	if err != nil {
		return nil, fmt.Errorf("oauth2 exchange: %w", err)
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in token response")
	}

	verifier := s.provider.Verifier(&oidc.Config{ClientID: s.cfg.ClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("id token verify: %w", err)
	}

	if nonce != "" && idToken.Nonce != nonce {
		return nil, fmt.Errorf("nonce mismatch")
	}

	var claims IDTokenClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("parse id token claims: %w", err)
	}

	return &ExchangeResult{
		IDToken:      idToken,
		Claims:       &claims,
		AccessToken:  oauth2Token.AccessToken,
		RefreshToken: oauth2Token.RefreshToken,
	}, nil
}

// ProvisionUser creates or updates a User from OIDC claims and returns the user.
func (s *Service) ProvisionUser(ctx context.Context, claims *IDTokenClaims, tenantID string) (*repository.UserRecord, error) {
	if claims == nil || claims.Subject == "" {
		return nil, fmt.Errorf("missing subject claim")
	}
	if tenantID == "" {
		tenantID = "default"
	}

	// Try to find existing user by OIDC subject stored in email or name for now.
	// Production deployments should add an oidc_subject column to users.
	users, err := s.store.ListUsers(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	for _, u := range users {
		if u.Email == claims.Email {
			return &u, nil
		}
	}

	name := claims.Name
	if name == "" {
		name = claims.GivenName
	}
	if name == "" {
		name = claims.Email
	}
	if name == "" {
		name = "oidc-" + claims.Subject
	}

	apiKey, err := repository.GenerateToken("gk-", 8)
	if err != nil {
		return nil, fmt.Errorf("generate api key: %w", err)
	}
	apiSecret, err := repository.GenerateToken("gs-", 16)
	if err != nil {
		return nil, fmt.Errorf("generate api secret: %w", err)
	}

	user, err := s.store.CreateUser(ctx, repository.CreateUserParams{
		TenantID:   tenantID,
		Name:       name,
		Email:      claims.Email,
		Role:       repository.RoleTenantUser,
		Quota:      -1,
		APIKey:     apiKey,
		SecretHash: repository.HashSecret(apiSecret),
		Status:     repository.StatusActive,
	})
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

// HTTPClient returns the underlying HTTP client, exposed for tests.
func (s *Service) HTTPClient() *http.Client {
	return s.client
}

// DiscoverEndpoints is a lightweight helper that fetches the issuer's well-known configuration.
func DiscoverEndpoints(ctx context.Context, issuer string, client *http.Client) (map[string]any, error) {
	if client == nil {
		client = http.DefaultClient
	}
	wellKnown := strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var endpoints map[string]any
	if err := json.Unmarshal(body, &endpoints); err != nil {
		return nil, err
	}
	return endpoints, nil
}

// IsJWTLike returns true if the token looks like a JWT (three base64url segments).
func IsJWTLike(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		if _, err := base64.RawURLEncoding.DecodeString(p); err != nil {
			return false
		}
	}
	return true
}

func encodeURLSafe(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

//nolint:unused
func parseURL(raw string) (*url.URL, error) {
	return url.Parse(raw)
}

//nolint:unused
func now() time.Time { return time.Now().UTC() }
