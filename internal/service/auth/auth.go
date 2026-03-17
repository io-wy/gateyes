package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/gateyes/gateway/internal/repository"
)

type Auth struct {
	repo *repository.APIKeyRepository
}

func NewAuth(repo *repository.APIKeyRepository) *Auth {
	return &Auth{repo: repo}
}

func (a *Auth) Verify(key, secret string) (*repository.APIKeyInfo, bool) {
	info, ok := a.repo.Get(key)
	if !ok {
		return nil, false
	}
	if info.Secret != "" && info.Secret != secret {
		return nil, false
	}
	return info, true
}

func (a *Auth) VerifyAPIKey(key string) (*repository.APIKeyInfo, bool) {
	return a.repo.Get(key)
}

func (a *Auth) VerifySignature(key, secret, timestamp, body, signature string) bool {
	// HMAC-SHA256 signature verification
	message := timestamp + body
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func (a *Auth) ExtractKey(auth string) (key, secret string) {
	if auth == "" {
		return "", ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 {
		return "", ""
	}
	if parts[0] != "Bearer" {
		return "", ""
	}
	// key:secret format
	keyParts := strings.SplitN(parts[1], ":", 2)
	if len(keyParts) == 2 {
		return keyParts[0], keyParts[1]
	}
	return parts[1], ""
}

func (a *Auth) CheckModel(key, model string) bool {
	return a.repo.IsAllowedModel(key, model)
}
