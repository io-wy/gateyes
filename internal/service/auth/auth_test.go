package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/repository"
)

type fakeIdentityStore struct {
	identity           *repository.AuthIdentity
	authenticateErr    error
	touchErr           error
	consumeOK          bool
	consumeErr         error
	usageErr           error
	touchedAPIKeyID    string
	consumedUserID     string
	consumedTokens     int
	usageRecords       []repository.UsageRecord
}

func (f *fakeIdentityStore) CreateUser(ctx context.Context, params repository.CreateUserParams) (*repository.UserRecord, error) {
	return nil, nil
}

func (f *fakeIdentityStore) ListUsers(ctx context.Context, tenantID string) ([]repository.UserRecord, error) {
	return nil, nil
}

func (f *fakeIdentityStore) GetUser(ctx context.Context, tenantID string, idOrAPIKey string) (*repository.UserRecord, error) {
	return nil, nil
}

func (f *fakeIdentityStore) UpdateUser(ctx context.Context, tenantID string, idOrAPIKey string, params repository.UpdateUserParams) (*repository.UserRecord, error) {
	return nil, nil
}

func (f *fakeIdentityStore) DeleteUser(ctx context.Context, tenantID string, idOrAPIKey string) error {
	return nil
}

func (f *fakeIdentityStore) ResetUserUsage(ctx context.Context, tenantID string, idOrAPIKey string) (*repository.UserRecord, error) {
	return nil, nil
}

func (f *fakeIdentityStore) Stats(ctx context.Context, tenantID string) (*repository.UserStats, error) {
	return nil, nil
}

func (f *fakeIdentityStore) Authenticate(ctx context.Context, key string) (*repository.AuthIdentity, error) {
	if f.authenticateErr != nil {
		return nil, f.authenticateErr
	}
	return f.identity, nil
}

func (f *fakeIdentityStore) TouchAPIKey(ctx context.Context, apiKeyID string, at time.Time) error {
	f.touchedAPIKeyID = apiKeyID
	return f.touchErr
}

func (f *fakeIdentityStore) ConsumeQuota(ctx context.Context, userID string, tokens int) (bool, error) {
	f.consumedUserID = userID
	f.consumedTokens = tokens
	return f.consumeOK, f.consumeErr
}

func (f *fakeIdentityStore) EnsureBootstrapKey(ctx context.Context, params repository.BootstrapAPIKeyParams) error {
	return nil
}

func (f *fakeIdentityStore) CreateUsageRecord(ctx context.Context, record repository.UsageRecord) error {
	if f.usageErr != nil {
		return f.usageErr
	}
	f.usageRecords = append(f.usageRecords, record)
	return nil
}

func (f *fakeIdentityStore) GetUsageSummary(ctx context.Context, tenantID string) (*repository.UsageStats, error) {
	return nil, nil
}

func (f *fakeIdentityStore) GetProviderUsageSummary(ctx context.Context, tenantID string) (map[string]repository.ProviderUsageStats, error) {
	return nil, nil
}

func (f *fakeIdentityStore) GetUserUsageDetail(ctx context.Context, tenantID, userID string, startTime, endTime time.Time) ([]repository.UsageRecord, error) {
	return nil, nil
}

func (f *fakeIdentityStore) GetUserUsageTrend(ctx context.Context, tenantID, userID string, days int) ([]repository.DailyUsage, error) {
	return nil, nil
}

func (f *fakeIdentityStore) GetTenantUsageTrend(ctx context.Context, tenantID string, days int) ([]repository.DailyUsage, error) {
	return nil, nil
}

func (f *fakeIdentityStore) EnsureTenant(ctx context.Context, params repository.EnsureTenantParams) (*repository.TenantRecord, error) {
	return nil, nil
}

func (f *fakeIdentityStore) ListTenants(ctx context.Context) ([]repository.TenantRecord, error) {
	return nil, nil
}

func (f *fakeIdentityStore) GetTenant(ctx context.Context, idOrSlug string) (*repository.TenantRecord, error) {
	return nil, nil
}

func (f *fakeIdentityStore) UpdateTenant(ctx context.Context, idOrSlug string, params repository.UpdateTenantParams) (*repository.TenantRecord, error) {
	return nil, nil
}

func (f *fakeIdentityStore) ListTenantProviders(ctx context.Context, tenantID string) ([]string, error) {
	return nil, nil
}

func (f *fakeIdentityStore) ReplaceTenantProviders(ctx context.Context, tenantID string, providerNames []string) error {
	return nil
}

func (f *fakeIdentityStore) CreateResponse(ctx context.Context, record repository.ResponseRecord) error {
	return nil
}

func (f *fakeIdentityStore) UpdateResponse(ctx context.Context, record repository.ResponseRecord) error {
	return nil
}

func (f *fakeIdentityStore) GetResponse(ctx context.Context, tenantID string, id string) (*repository.ResponseRecord, error) {
	return nil, nil
}

func baseIdentity() *repository.AuthIdentity {
	return &repository.AuthIdentity{
		APIKeyID:     "api-1",
		APIKey:       "key-1",
		SecretHash:   repository.HashSecret("secret-1"),
		APIStatus:    repository.StatusActive,
		UserID:       "user-1",
		UserStatus:   repository.StatusActive,
		TenantID:     "tenant-1",
		TenantStatus: repository.StatusActive,
		Role:         repository.RoleTenantAdmin,
		Quota:        20,
		Used:         4,
		Models:       []string{"gpt-1"},
	}
}

func TestAuthenticateCoversErrorStatusAndSuccess(t *testing.T) {
	store := &fakeIdentityStore{authenticateErr: repository.ErrNotFound}
	service := NewAuth(store)

	if _, err := service.Authenticate(context.Background(), "missing", "secret"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("Authenticate(not found) error = %v, want %v", err, ErrInvalidAPIKey)
	}

	inactive := baseIdentity()
	inactive.APIStatus = repository.StatusInactive
	service = NewAuth(&fakeIdentityStore{identity: inactive})
	if _, err := service.Authenticate(context.Background(), "key-1", "secret-1"); !errors.Is(err, ErrInactiveAPIKey) {
		t.Fatalf("Authenticate(inactive) error = %v, want %v", err, ErrInactiveAPIKey)
	}

	service = NewAuth(&fakeIdentityStore{identity: baseIdentity()})
	if _, err := service.Authenticate(context.Background(), "key-1", "wrong"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("Authenticate(wrong secret) error = %v, want %v", err, ErrInvalidAPIKey)
	}

	got, err := service.Authenticate(context.Background(), "key-1", "secret-1")
	if err != nil {
		t.Fatalf("Authenticate(success) error: %v", err)
	}
	if got.UserID != "user-1" {
		t.Fatalf("Authenticate(success).UserID = %q, want %q", got.UserID, "user-1")
	}
}

func TestTouchCheckModelHasQuotaRequireRoleAndExtractKey(t *testing.T) {
	store := &fakeIdentityStore{identity: baseIdentity()}
	service := NewAuth(store)

	if err := service.Touch(context.Background(), store.identity); err != nil {
		t.Fatalf("Touch() error: %v", err)
	}
	if got, want := store.touchedAPIKeyID, "api-1"; got != want {
		t.Fatalf("Touch() touched API key = %q, want %q", got, want)
	}
	if !service.CheckModel(store.identity, "gpt-1") || service.CheckModel(store.identity, "claude-1") {
		t.Fatal("CheckModel() returned unexpected result")
	}
	if !service.HasQuota(store.identity, 10) || service.HasQuota(store.identity, 17) {
		t.Fatal("HasQuota() returned unexpected result")
	}
	if err := service.RequireRole(store.identity, repository.RoleTenantAdmin); err != nil {
		t.Fatalf("RequireRole(allowed) error: %v", err)
	}
	if err := service.RequireRole(nil, repository.RoleTenantAdmin); !errors.Is(err, ErrForbidden) {
		t.Fatalf("RequireRole(nil) error = %v, want %v", err, ErrForbidden)
	}
	key, secret := service.ExtractKey("Bearer key-1:secret-1")
	if key != "key-1" || secret != "secret-1" {
		t.Fatalf("ExtractKey(Bearer key-1:secret-1) = (%q, %q), want (%q, %q)", key, secret, "key-1", "secret-1")
	}
	key, secret = service.ExtractKey("Bearer key-1")
	if key != "key-1" || secret != "" {
		t.Fatalf("ExtractKey(Bearer key-1) = (%q, %q), want (%q, %q)", key, secret, "key-1", "")
	}
}

func TestRecordUsageSuccessAndQuotaExceeded(t *testing.T) {
	identity := baseIdentity()
	store := &fakeIdentityStore{
		identity:  identity,
		consumeOK: true,
	}
	service := NewAuth(store)

	err := service.RecordUsage(context.Background(), identity, "openai", "gpt-1", 3, 2, 5, 1.2, 40, "success", "")
	if err != nil {
		t.Fatalf("RecordUsage(success) error: %v", err)
	}
	if got, want := identity.Used, 9; got != want {
		t.Fatalf("RecordUsage(success) updated Used = %d, want %d", got, want)
	}
	if got, want := store.consumedUserID, "user-1"; got != want {
		t.Fatalf("RecordUsage(success) consumed user = %q, want %q", got, want)
	}
	if len(store.usageRecords) != 1 {
		t.Fatalf("RecordUsage(success) usage records = %d, want %d", len(store.usageRecords), 1)
	}

	quotaStore := &fakeIdentityStore{
		identity:  baseIdentity(),
		consumeOK: false,
	}
	quotaService := NewAuth(quotaStore)
	if err := quotaService.RecordUsage(context.Background(), quotaStore.identity, "openai", "gpt-1", 1, 1, 2, 0.1, 10, "success", ""); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("RecordUsage(quota exceeded) error = %v, want %v", err, ErrQuotaExceeded)
	}
	if len(quotaStore.usageRecords) != 0 {
		t.Fatalf("RecordUsage(quota exceeded) usage records = %d, want %d", len(quotaStore.usageRecords), 0)
	}
}
