package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gateyes/gateway/internal/repository"
)

var (
	ErrInvalidAPIKey   = errors.New("invalid API key")
	ErrInactiveAPIKey  = errors.New("inactive API key")
	ErrModelNotAllowed = errors.New("model not allowed")
	ErrQuotaExceeded   = errors.New("quota exceeded")
	ErrForbidden       = errors.New("forbidden")
)

type Auth struct {
	store repository.Store
}

func NewAuth(store repository.Store) *Auth {
	return &Auth{store: store}
}

func (a *Auth) Authenticate(ctx context.Context, key, secret string) (*repository.AuthIdentity, error) {
	identity, err := a.store.Authenticate(ctx, key)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidAPIKey
		}
		return nil, err
	}

	if identity.APIStatus != repository.StatusActive || identity.UserStatus != repository.StatusActive || identity.TenantStatus != repository.StatusActive {
		return nil, ErrInactiveAPIKey
	}

	if !repository.VerifySecret(secret, identity.SecretHash) {
		return nil, ErrInvalidAPIKey
	}

	return identity, nil
}

func (a *Auth) Touch(ctx context.Context, identity *repository.AuthIdentity) error {
	return a.store.TouchAPIKey(ctx, identity.APIKeyID, time.Now().UTC())
}

func (a *Auth) CheckModel(identity *repository.AuthIdentity, model string) bool {
	if len(identity.Models) == 0 {
		return true
	}
	for _, allowed := range identity.Models {
		if allowed == model {
			return true
		}
	}
	return false
}

func (a *Auth) HasQuota(identity *repository.AuthIdentity, tokens int) bool {
	if identity.Quota <= 0 {
		return true
	}
	return identity.Used+tokens <= identity.Quota
}

func (a *Auth) RequireRole(identity *repository.AuthIdentity, roles ...string) error {
	if identity == nil || !repository.HasRole(identity.Role, roles...) {
		return ErrForbidden
	}
	return nil
}

func (a *Auth) RecordUsage(
	ctx context.Context,
	identity *repository.AuthIdentity,
	providerName string,
	model string,
	promptTokens int,
	completionTokens int,
	totalTokens int,
	cost float64,
	latencyMs int64,
	status string,
	errorType string,
) error {
	return a.recordUsage(ctx, identity, providerName, model, promptTokens, completionTokens, totalTokens, cost, latencyMs, status, errorType, status == "success")
}

func (a *Auth) RecordBillableUsage(
	ctx context.Context,
	identity *repository.AuthIdentity,
	providerName string,
	model string,
	promptTokens int,
	completionTokens int,
	totalTokens int,
	cost float64,
	latencyMs int64,
	status string,
	errorType string,
) error {
	return a.recordUsage(ctx, identity, providerName, model, promptTokens, completionTokens, totalTokens, cost, latencyMs, status, errorType, totalTokens > 0)
}

func (a *Auth) recordUsage(
	ctx context.Context,
	identity *repository.AuthIdentity,
	providerName string,
	model string,
	promptTokens int,
	completionTokens int,
	totalTokens int,
	cost float64,
	latencyMs int64,
	status string,
	errorType string,
	consumeQuota bool,
) error {
	if err := a.store.TouchAPIKey(ctx, identity.APIKeyID, time.Now().UTC()); err != nil {
		return err
	}

	if consumeQuota {
		ok, err := a.store.ConsumeQuota(ctx, identity.UserID, totalTokens)
		if err != nil {
			return err
		}
		if !ok {
			return ErrQuotaExceeded
		}
		identity.Used += totalTokens
	}

	return a.store.CreateUsageRecord(ctx, repository.UsageRecord{
		TenantID:         identity.TenantID,
		UserID:           identity.UserID,
		APIKeyID:         identity.APIKeyID,
		ProviderName:     providerName,
		Model:            model,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		Cost:             cost,
		LatencyMs:        latencyMs,
		Status:           status,
		ErrorType:        errorType,
	})
}

func (a *Auth) ExtractKey(authHeader string) (key string, secret string) {
	if authHeader == "" {
		return "", ""
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return "", ""
	}

	keyParts := strings.SplitN(parts[1], ":", 2)
	if len(keyParts) == 2 {
		return keyParts[0], keyParts[1]
	}

	return parts[1], ""
}
