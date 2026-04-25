package budget

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
)

type fakeBudgetStore struct {
	checkResults map[string]*repository.BudgetCheckResult
	checkErr     error
}

func (f *fakeBudgetStore) CheckAPIKeyBudget(ctx context.Context, apiKeyID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	if r, ok := f.checkResults["api_key"]; ok {
		return r, nil
	}
	return &repository.BudgetCheckResult{Allowed: true, Scope: "api_key"}, nil
}

func (f *fakeBudgetStore) CheckProjectBudget(ctx context.Context, projectID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	if r, ok := f.checkResults["project"]; ok {
		return r, nil
	}
	return &repository.BudgetCheckResult{Allowed: true, Scope: "project"}, nil
}

func (f *fakeBudgetStore) CheckTenantBudget(ctx context.Context, tenantID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	if r, ok := f.checkResults["tenant"]; ok {
		return r, nil
	}
	return &repository.BudgetCheckResult{Allowed: true, Scope: "tenant"}, nil
}

func TestBudgetService_Allowed(t *testing.T) {
	store := &fakeBudgetStore{}
	svc := New(store)

	identity := &repository.AuthIdentity{
		APIKeyID:  "key-1",
		ProjectID: "proj-1",
		TenantID:  "tenant-1",
	}

	result, err := svc.Check(context.Background(), CheckRequest{
		Identity:      identity,
		EstimatedCost: 10,
		Model:         "gpt-4",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected allowed")
	}
	if result.RejectError != nil {
		t.Errorf("expected no reject error, got %v", result.RejectError)
	}
	if len(result.Scopes) != 3 {
		t.Errorf("expected 3 scopes, got %d", len(result.Scopes))
	}
}

func TestBudgetService_HardReject(t *testing.T) {
	store := &fakeBudgetStore{
		checkResults: map[string]*repository.BudgetCheckResult{
			"api_key": {Allowed: false, Scope: "api_key", Policy: repository.BudgetPolicyHardReject},
		},
	}
	svc := New(store)

	identity := &repository.AuthIdentity{
		APIKeyID:  "key-1",
		ProjectID: "proj-1",
		TenantID:  "tenant-1",
	}

	result, err := svc.Check(context.Background(), CheckRequest{
		Identity:      identity,
		EstimatedCost: 10,
		Model:         "gpt-4",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected not allowed")
	}
	if result.RejectError != ErrBudgetHardRejected {
		t.Errorf("expected ErrBudgetHardRejected, got %v", result.RejectError)
	}
}

func TestBudgetService_SoftAlert(t *testing.T) {
	store := &fakeBudgetStore{
		checkResults: map[string]*repository.BudgetCheckResult{
			"project": {Allowed: false, Scope: "project", Policy: repository.BudgetPolicySoftAlert},
		},
	}
	svc := New(store)

	identity := &repository.AuthIdentity{
		APIKeyID:  "key-1",
		ProjectID: "proj-1",
		TenantID:  "tenant-1",
	}

	result, err := svc.Check(context.Background(), CheckRequest{
		Identity:      identity,
		EstimatedCost: 10,
		Model:         "gpt-4",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected allowed for soft alert")
	}
	if !result.AlertSent {
		t.Error("expected AlertSent to be true")
	}
}

func TestBudgetService_Grace(t *testing.T) {
	store := &fakeBudgetStore{
		checkResults: map[string]*repository.BudgetCheckResult{
			"tenant": {Allowed: false, Scope: "tenant", Policy: repository.BudgetPolicyGrace},
		},
	}
	svc := New(store)

	identity := &repository.AuthIdentity{
		APIKeyID:  "key-1",
		ProjectID: "proj-1",
		TenantID:  "tenant-1",
	}

	result, err := svc.Check(context.Background(), CheckRequest{
		Identity:      identity,
		EstimatedCost: 10,
		Model:         "gpt-4",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Error("expected allowed for grace mode")
	}
	if result.AlertSent {
		t.Error("expected AlertSent to be false for grace")
	}
}

func TestBudgetService_DefaultPolicyHardReject(t *testing.T) {
	store := &fakeBudgetStore{
		checkResults: map[string]*repository.BudgetCheckResult{
			"api_key": {Allowed: false, Scope: "api_key", Policy: ""},
		},
	}
	svc := New(store)

	identity := &repository.AuthIdentity{
		APIKeyID: "key-1",
		TenantID: "tenant-1",
	}

	result, err := svc.Check(context.Background(), CheckRequest{
		Identity:      identity,
		EstimatedCost: 10,
		Model:         "gpt-4",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected not allowed for empty policy default")
	}
	if result.RejectError != ErrBudgetHardRejected {
		t.Errorf("expected ErrBudgetHardRejected, got %v", result.RejectError)
	}
}

func TestBudgetService_UnknownPolicyHardReject(t *testing.T) {
	store := &fakeBudgetStore{
		checkResults: map[string]*repository.BudgetCheckResult{
			"api_key": {Allowed: false, Scope: "api_key", Policy: "unknown_policy"},
		},
	}
	svc := New(store)

	identity := &repository.AuthIdentity{
		APIKeyID: "key-1",
		TenantID: "tenant-1",
	}

	result, err := svc.Check(context.Background(), CheckRequest{
		Identity:      identity,
		EstimatedCost: 10,
		Model:         "gpt-4",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("expected not allowed for unknown policy")
	}
	if result.RejectError != ErrBudgetHardRejected {
		t.Errorf("expected ErrBudgetHardRejected, got %v", result.RejectError)
	}
}

func TestBudgetService_CheckError(t *testing.T) {
	store := &fakeBudgetStore{checkErr: repository.ErrNotFound}
	svc := New(store)

	identity := &repository.AuthIdentity{
		APIKeyID: "key-1",
		TenantID: "tenant-1",
	}

	_, err := svc.Check(context.Background(), CheckRequest{
		Identity:      identity,
		EstimatedCost: 10,
		Model:         "gpt-4",
	})
	if err == nil {
		t.Error("expected error")
	}
}
