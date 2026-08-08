package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/gateyes/gateway/internal/repository"
)

var (
	ErrInvalidAPIKey   = errors.New("invalid API key")
	ErrInactiveAPIKey  = errors.New("inactive API key")
	ErrExpiredAPIKey   = errors.New("expired API key")
	ErrModelNotAllowed = errors.New("model not allowed")
	ErrQuotaExceeded   = errors.New("quota exceeded")
	ErrBudgetExceeded  = errors.New("budget exceeded")
	ErrForbidden       = errors.New("forbidden")
)

const defaultIdentityCacheTTL = 30 * time.Second

type Auth struct {
	store    repository.Store
	rdb      *redis.Client
	cache    map[string]*cachedIdentity
	cacheMu  sync.RWMutex
	cacheTTL time.Duration
}

type cachedIdentity struct {
	identity  *repository.AuthIdentity
	revisions identityCacheRevisions
	expires   time.Time
}

type redisCachedIdentity struct {
	Identity  repository.AuthIdentity `json:"identity"`
	Revisions identityCacheRevisions  `json:"revisions"`
}

type identityCacheRevisions struct {
	APIKey  string `json:"api_key,omitempty"`
	User    string `json:"user,omitempty"`
	Tenant  string `json:"tenant,omitempty"`
	Project string `json:"project,omitempty"`
}

func NewAuth(store repository.Store) *Auth {
	return &Auth{
		store:    store,
		cache:    make(map[string]*cachedIdentity),
		cacheTTL: defaultIdentityCacheTTL,
	}
}

func (a *Auth) SetRedis(rdb *redis.Client) {
	a.rdb = rdb
}

func (a *Auth) getCached(ctx context.Context, key string) *repository.AuthIdentity {
	a.cacheMu.RLock()
	entry, ok := a.cache[key]
	if !ok || time.Now().After(entry.expires) {
		a.cacheMu.RUnlock()
		return nil
	}
	identity := entry.identity
	revisions := entry.revisions
	a.cacheMu.RUnlock()

	if a.rdb != nil && identity != nil {
		current, ok := a.currentIdentityRevisions(ctx, identity)
		if ok && !revisions.equal(current) {
			a.deleteLocalCache(key)
			return nil
		}
	}
	return entry.identity
}

func (a *Auth) setCached(key string, identity *repository.AuthIdentity, revisions identityCacheRevisions) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	a.cache[key] = &cachedIdentity{identity: identity, revisions: revisions, expires: time.Now().Add(a.cacheTTL)}
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

func (a *Auth) deleteLocalCache(key string) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	delete(a.cache, key)
}

func (a *Auth) invalidateCache(key string) {
	a.deleteLocalCache(key)
	if a.rdb != nil {
		_ = a.rdb.Del(context.Background(), identityCacheKey(key)).Err()
	}
}

func (a *Auth) InvalidateKey(key string) {
	if a == nil || strings.TrimSpace(key) == "" {
		return
	}
	a.invalidateCache(key)
}

func (a *Auth) InvalidateAPIKey(apiKeyID string) {
	a.bumpIdentityRevision("api_key", apiKeyID)
}

func (a *Auth) InvalidateUser(userID string) {
	a.bumpIdentityRevision("user", userID)
}

func (a *Auth) InvalidateTenant(tenantID string) {
	a.bumpIdentityRevision("tenant", tenantID)
}

func (a *Auth) InvalidateProject(projectID string) {
	a.bumpIdentityRevision("project", projectID)
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
	if identity.APIKeyExpiresAt != nil && time.Now().UTC().After(identity.APIKeyExpiresAt.UTC()) {
		return ErrExpiredAPIKey
	}
	if !repository.VerifySecret(secret, identity.SecretHash) {
		return ErrInvalidAPIKey
	}
	return nil
}

func (a *Auth) authenticateAPIKey(ctx context.Context, key, secret string) (*repository.AuthIdentity, error) {
	// Try cache first to avoid the 4-table JOIN hot path.
	if identity := a.getCached(ctx, key); identity != nil {
		if err := verifyIdentity(identity, secret); err != nil {
			return nil, err
		}
		return identity, nil
	}
	if identity, revisions := a.getRedisCached(ctx, key); identity != nil {
		if err := verifyIdentity(identity, secret); err != nil {
			return nil, err
		}
		a.setCached(key, identity, revisions)
		return identity, nil
	}

	identity, err := a.store.Authenticate(ctx, key)
	if err != nil {
		return nil, err
	}
	if err := verifyIdentity(identity, secret); err != nil {
		return nil, err
	}

	revisions, ok := a.currentIdentityRevisions(ctx, identity)
	a.setCached(key, identity, revisions)
	if ok {
		a.setRedisCached(ctx, key, identity, revisions)
	}
	return identity, nil
}

func identityCacheKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "auth:identity:" + hex.EncodeToString(sum[:])
}

func identityRevisionKey(scope, id string) string {
	return fmt.Sprintf("auth:identity_rev:%s:%s", scope, id)
}

func (r identityCacheRevisions) equal(other identityCacheRevisions) bool {
	return r.APIKey == other.APIKey &&
		r.User == other.User &&
		r.Tenant == other.Tenant &&
		r.Project == other.Project
}

func (a *Auth) getRedisCached(ctx context.Context, key string) (*repository.AuthIdentity, identityCacheRevisions) {
	if a.rdb == nil || strings.TrimSpace(key) == "" {
		return nil, identityCacheRevisions{}
	}
	raw, err := a.rdb.Get(ctx, identityCacheKey(key)).Bytes()
	if err != nil {
		return nil, identityCacheRevisions{}
	}
	var envelope redisCachedIdentity
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Identity.APIKeyID == "" {
		return nil, identityCacheRevisions{}
	}
	identity := &envelope.Identity
	current, ok := a.currentIdentityRevisions(ctx, identity)
	if ok && !envelope.Revisions.equal(current) {
		_ = a.rdb.Del(ctx, identityCacheKey(key)).Err()
		return nil, identityCacheRevisions{}
	}
	return identity, envelope.Revisions
}

func (a *Auth) setRedisCached(ctx context.Context, key string, identity *repository.AuthIdentity, revisions identityCacheRevisions) {
	if a.rdb == nil || strings.TrimSpace(key) == "" || identity == nil {
		return
	}
	raw, err := json.Marshal(redisCachedIdentity{Identity: *identity, Revisions: revisions})
	if err != nil {
		return
	}
	_ = a.rdb.Set(ctx, identityCacheKey(key), raw, a.cacheTTL).Err()
}

func (a *Auth) currentIdentityRevisions(ctx context.Context, identity *repository.AuthIdentity) (identityCacheRevisions, bool) {
	if a.rdb == nil || identity == nil {
		return identityCacheRevisions{}, true
	}
	var keys []string
	var fields []string
	add := func(field, scope, id string) {
		if strings.TrimSpace(id) == "" {
			return
		}
		fields = append(fields, field)
		keys = append(keys, identityRevisionKey(scope, id))
	}
	add("api_key", "api_key", identity.APIKeyID)
	add("user", "user", identity.UserID)
	add("tenant", "tenant", identity.TenantID)
	add("project", "project", identity.ProjectID)
	if len(keys) == 0 {
		return identityCacheRevisions{}, true
	}

	values, err := a.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return identityCacheRevisions{}, false
	}
	var revisions identityCacheRevisions
	for i, value := range values {
		revision := ""
		if value != nil {
			revision = fmt.Sprint(value)
		}
		switch fields[i] {
		case "api_key":
			revisions.APIKey = revision
		case "user":
			revisions.User = revision
		case "tenant":
			revisions.Tenant = revision
		case "project":
			revisions.Project = revision
		}
	}
	return revisions, true
}

func (a *Auth) bumpIdentityRevision(scope, id string) {
	if a == nil || a.rdb == nil || strings.TrimSpace(id) == "" {
		return
	}
	_ = a.rdb.Incr(context.Background(), identityRevisionKey(scope, id)).Err()
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
