// oidc-mock is a minimal OIDC provider for local manual testing of Gateyes admin SSO.
// It implements discovery, authorization, token, and JWKS endpoints.
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	listenAddr   = "localhost:8090"
	issuer       = "http://" + listenAddr
	clientID     = "gateyes-local"
	clientSecret = "gateyes-local-secret"
)

type authCode struct {
	state        string
	nonce        string
	redirectURI  string
	codeChallenge string
}

var (
	privateKey *rsa.PrivateKey
	codesMu    sync.Mutex
	codes      = map[string]*authCode{}
)

func main() {
	var err error
	privateKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		slog.Error("failed to generate rsa key", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", discovery)
	mux.HandleFunc("/authorize", authorize)
	mux.HandleFunc("/token", token)
	mux.HandleFunc("/keys", jwks)

	slog.Info("oidc mock listening", "addr", listenAddr, "issuer", issuer)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                 issuer,
		"authorization_endpoint": issuer + "/authorize",
		"token_endpoint":         issuer + "/token",
		"jwks_uri":               issuer + "/keys",
		"response_types_supported": []string{"code"},
		"subject_types_supported":  []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":        []string{"openid", "email", "profile"},
		"claims_supported":        []string{"sub", "email", "name", "given_name", "nonce"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post"},
	})
}

func authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != clientID {
		http.Error(w, "invalid client_id", http.StatusBadRequest)
		return
	}
	if q.Get("response_type") != "code" {
		http.Error(w, "unsupported response_type", http.StatusBadRequest)
		return
	}
	redirectURI := q.Get("redirect_uri")
	if redirectURI == "" {
		http.Error(w, "missing redirect_uri", http.StatusBadRequest)
		return
	}

	code := randomString(32)
	codesMu.Lock()
	codes[code] = &authCode{
		state:         q.Get("state"),
		nonce:         q.Get("nonce"),
		redirectURI:   redirectURI,
		codeChallenge: q.Get("code_challenge"),
	}
	codesMu.Unlock()

	u, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	q2 := u.Query()
	q2.Set("code", code)
	if state := q.Get("state"); state != "" {
		q2.Set("state", state)
	}
	u.RawQuery = q2.Encode()
	w.Header().Set("Location", u.String())
	w.WriteHeader(http.StatusFound)
}

func token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if r.FormValue("grant_type") != "authorization_code" {
		http.Error(w, "unsupported grant_type", http.StatusBadRequest)
		return
	}
	if r.FormValue("client_id") != clientID || r.FormValue("client_secret") != clientSecret {
		http.Error(w, "invalid client credentials", http.StatusUnauthorized)
		return
	}

	code := r.FormValue("code")
	codesMu.Lock()
	ac, ok := codes[code]
	if ok {
		delete(codes, code)
	}
	codesMu.Unlock()
	if !ok {
		http.Error(w, "invalid code", http.StatusBadRequest)
		return
	}
	if r.FormValue("redirect_uri") != ac.redirectURI {
		http.Error(w, "redirect_uri mismatch", http.StatusBadRequest)
		return
	}

	verifier := r.FormValue("code_verifier")
	if ac.codeChallenge != "" {
		challenge := pkceChallenge(verifier)
		if challenge != ac.codeChallenge {
			http.Error(w, "invalid code_verifier", http.StatusBadRequest)
			return
		}
	}

	idToken, err := signIDToken(ac.nonce)
	if err != nil {
		http.Error(w, "failed to sign id_token", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"access_token":  randomString(32),
		"token_type":    "Bearer",
		"expires_in":    3600,
		"refresh_token": randomString(32),
		"id_token":      idToken,
	})
}

func jwks(w http.ResponseWriter, _ *http.Request) {
	pub := privateKey.Public().(*rsa.PublicKey)
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	writeJSON(w, map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"use": "sig",
			"kid": "mock-key-1",
			"alg": "RS256",
			"n":   n,
			"e":   e,
		}},
	})
}

func signIDToken(nonce string) (string, error) {
	claims := jwt.MapClaims{
		"iss":   issuer,
		"sub":   "mock-oidc-subject",
		"aud":   clientID,
		"email": "admin@example.com",
		"name":  "OIDC Admin",
		"given_name": "OIDC",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "mock-key-1"
	return token.SignedString(privateKey)
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode json", "error", err)
	}
}
