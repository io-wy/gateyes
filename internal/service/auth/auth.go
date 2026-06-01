package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/gateyes/gateway/internal/repository"
)

var (
	ErrInvalidAPIKey   = errors.New("invalid API key")
	ErrInactiveAPIKey  = errors.New("inactive API key")
	ErrModelNotAllowed = errors.New("model not allowed")
	ErrQuotaExceeded   = errors.New("quota exceeded")
	ErrBudgetExceeded  = errors.New("budget exceeded")
	ErrForbidden       = errors.New("forbidden")
)

const defaultIdentityCacheTTL = 30 * time.Second

type Auth struct {
	store    repository.Store
	cache    map[string]*cachedIdentity
	cacheMu  sync.RWMutex
	cacheTTL time.Duration
}

type cachedIdentity struct {
	identity *repository.AuthIdentity
	expires  time.Time
}

func NewAuth(store repository.Store) *Auth {
	return &Auth{
		store:    store,
		cache:    make(map[string]*cachedIdentity),
		cacheTTL: defaultIdentityCacheTTL,
	}
}

func (a *Auth) getCached(key string) *repository.AuthIdentity {
	a.cacheMu.RLock()
	defer a.cacheMu.RUnlock()
	entry, ok := a.cache[key]
	if !ok || time.Now().After(entry.expires) {
		return nil
	}
	return entry.identity
}

func (a *Auth) setCached(key string, identity *repository.AuthIdentity) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	a.cache[key] = &cachedIdentity{identity: identity, expires: time.Now().Add(a.cacheTTL)}
	// Sampling cleanup: evict expired entries ~1/64 of the time.
	if len(a.cache)%64 == 0 {
		now := time.Now()
		for k, v := range a.cache {
			if now.After(v.expires) {
				delete(a.cache, k)
			}
		}
	}
}

func (a *Auth) invalidateCache(key string) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	delete(a.cache, key)
}

func (a *Auth) Authenticate(ctx context.Context, key, secret string) (*repository.AuthIdentity, error) {
	identity, err := a.authenticateAPIKey(ctx, key, secret)
	if err == nil {
		return identity, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}

	// fallback to virtual key
	identity, err = a.authenticateVirtualKey(ctx, key, secret)
	if err == nil {
		return identity, nil
	}
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrInvalidAPIKey
	}
	return nil, err
}

func verifyIdentity(identity *repository.AuthIdentity, secret string) error {
	if !isIdentityActive(identity) {
		return ErrInactiveAPIKey
	}
	if !repository.VerifySecret(secret, identity.SecretHash) {
		return ErrInvalidAPIKey
	}
	return nil
}

func (a *Auth) authenticateAPIKey(ctx context.Context, key, secret string) (*repository.AuthIdentity, error) {
	// Try cache first to avoid the 4-table JOIN hot path.
	if identity := a.getCached(key); identity != nil {
		if err := verifyIdentity(identity, secret); err != nil {
			return nil, err
		}
		return identity, nil
	}

	identity, err := a.store.Authenticate(ctx, key)
	if err != nil {
		return nil, err
	}
	if err := verifyIdentity(identity, secret); err != nil {
		return nil, err
	}

	a.setCached(key, identity)
	return identity, nil
}

func isIdentityActive(identity *repository.AuthIdentity) bool {
	return identity.APIStatus == repository.StatusActive &&
		identity.UserStatus == repository.StatusActive &&
		identity.TenantStatus == repository.StatusActive
}

func (a *Auth) authenticateVirtualKey(ctx context.Context, key, secret string) (*repository.AuthIdentity, error) {
	vk, err := a.store.AuthenticateVirtualKey(ctx, key)
	if err != nil {
		return nil, err
	}
	if !repository.VerifySecret(secret, vk.SecretHash) {
		return nil, ErrInvalidAPIKey
	}
	// load parent api key identity
	parent, err := a.store.Authenticate(ctx, vk.APIKeyID)
	if err != nil {
		return nil, ErrInvalidAPIKey
	}
	if !isIdentityActive(parent) {
		return nil, ErrInactiveAPIKey
	}

	// overlay virtual key restrictions
	parent.VirtualKeyID = vk.ID
	if vk.BudgetUSD > 0 {
		parent.VirtualKeyBudgetUSD = vk.BudgetUSD
		parent.VirtualKeySpentUSD = vk.SpentUSD
		parent.VirtualKeyBudgetPolicy = vk.BudgetPolicy
	}
	if vk.RateLimitQPS > 0 {
		parent.APIKeyRateLimitQPS = vk.RateLimitQPS
	}
	if len(vk.AllowedModels) > 0 {
		parent.APIKeyModels = vk.AllowedModels
	}
	if len(vk.AllowedProviders) > 0 {
		parent.APIKeyProviders = vk.AllowedProviders
	}
	parent.CallbackURL = vk.CallbackURL
	return parent, nil
}

func (a *Auth) Touch(ctx context.Context, identity *repository.AuthIdentity) error {
	return a.store.TouchAPIKey(ctx, identity.APIKeyID, time.Now().UTC())
}

func (a *Auth) CheckModel(identity *repository.AuthIdentity, model string) bool {
	if len(identity.Models) == 0 && len(identity.APIKeyModels) == 0 {
		return true
	}
	if len(identity.Models) > 0 && !contains(identity.Models, model) {
		return false
	}
	if len(identity.APIKeyModels) > 0 && !contains(identity.APIKeyModels, model) {
		return false
	}
	return true
}

func (a *Auth) CheckProvider(identity *repository.AuthIdentity, providerName string) bool {
	if len(identity.APIKeyProviders) == 0 {
		return true
	}
	return contains(identity.APIKeyProviders, providerName)
}

func (a *Auth) CheckService(identity *repository.AuthIdentity, requestPrefix string) bool {
	if len(identity.APIKeyServices) == 0 {
		return true
	}
	return contains(identity.APIKeyServices, strings.ToLower(strings.TrimSpace(requestPrefix)))
}

func (a *Auth) EffectiveRateLimitQPS(identity *repository.AuthIdentity) int {
	if identity == nil {
		return 0
	}
	if identity.APIKeyRateLimitQPS > 0 {
		return identity.APIKeyRateLimitQPS
	}
	return identity.QPS
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

	if cost > 0 || consumeQuota {
		tokens := 0
		if consumeQuota {
			tokens = totalTokens
		}
		ok, err := a.store.ConsumeBudgets(ctx, identity.APIKeyID, identity.ProjectID, identity.TenantID, identity.VirtualKeyID, identity.UserID, cost, tokens)
		if err != nil {
			if errors.Is(err, repository.ErrQuotaExceeded) {
				return ErrQuotaExceeded
			}
			return err
		}
		if !ok {
			return ErrBudgetExceeded
		}
	}

	return a.store.CreateUsageRecord(ctx, repository.UsageRecord{
		TenantID:         identity.TenantID,
		ProjectID:        identity.ProjectID,
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

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
