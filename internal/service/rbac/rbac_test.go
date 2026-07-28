package rbac

import (
	"context"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/repository"
)

type fakeRoleStore struct {
	permissions []repository.PermissionRecord
	err         error
}

func (f *fakeRoleStore) CreateRole(ctx context.Context, params repository.CreateRoleParams) (*repository.RoleRecord, error) {
	return nil, nil
}
func (f *fakeRoleStore) ListRoles(ctx context.Context, tenantID string, filter repository.RoleFilter) ([]repository.RoleRecord, error) {
	return nil, nil
}
func (f *fakeRoleStore) GetRole(ctx context.Context, tenantID string, id string) (*repository.RoleRecord, error) {
	return nil, nil
}
func (f *fakeRoleStore) UpdateRole(ctx context.Context, tenantID string, id string, params repository.UpdateRoleParams) (*repository.RoleRecord, error) {
	return nil, nil
}
func (f *fakeRoleStore) DeleteRole(ctx context.Context, tenantID string, id string) error {
	return nil
}
func (f *fakeRoleStore) ListPermissions(ctx context.Context) ([]repository.PermissionRecord, error) {
	return f.permissions, f.err
}
func (f *fakeRoleStore) GetRolePermissions(ctx context.Context, roleID string) ([]repository.PermissionRecord, error) {
	return f.permissions, f.err
}
func (f *fakeRoleStore) SetRolePermissions(ctx context.Context, roleID string, permissionIDs []string) error {
	return nil
}
func (f *fakeRoleStore) GetUserRoleIDs(ctx context.Context, userID string) ([]string, error) {
	return nil, nil
}
func (f *fakeRoleStore) GetUserPermissions(ctx context.Context, userID string) ([]repository.PermissionRecord, error) {
	return f.permissions, f.err
}

func TestHasPermission(t *testing.T) {
	store := &fakeRoleStore{
		permissions: []repository.PermissionRecord{
			{ID: "p1", Code: "provider:read"},
			{ID: "p2", Code: "provider:write"},
		},
	}
	svc := New(store, nil, time.Hour)

	if !svc.HasPermission(context.Background(), "user-1", "provider:read") {
		t.Fatal("expected provider:read to be allowed")
	}
	if !svc.HasPermission(context.Background(), "user-1", "provider:write") {
		t.Fatal("expected provider:write to be allowed")
	}
	if svc.HasPermission(context.Background(), "user-1", "api_key:write") {
		t.Fatal("expected api_key:write to be denied")
	}

	// Cached: second lookup should not hit store again; behavior stays the same.
	if !svc.HasPermission(context.Background(), "user-1", "provider:read") {
		t.Fatal("expected cached provider:read to be allowed")
	}
}

func TestHasPermissionEmptyUser(t *testing.T) {
	svc := New(&fakeRoleStore{}, nil, time.Hour)
	if svc.HasPermission(context.Background(), "", "provider:read") {
		t.Fatal("expected empty user to be denied")
	}
}

func TestHasPermissionStoreError(t *testing.T) {
	store := &fakeRoleStore{err: context.DeadlineExceeded}
	svc := New(store, nil, time.Hour)
	if svc.HasPermission(context.Background(), "user-1", "provider:read") {
		t.Fatal("expected store error to result in denial")
	}
}

func TestInvalidate(t *testing.T) {
	store := &fakeRoleStore{
		permissions: []repository.PermissionRecord{{ID: "p1", Code: "provider:read"}},
	}
	svc := New(store, nil, time.Hour)
	if !svc.HasPermission(context.Background(), "user-1", "provider:read") {
		t.Fatal("expected initial permission")
	}
	svc.Invalidate("user-1")
	store.permissions = nil
	if svc.HasPermission(context.Background(), "user-1", "provider:read") {
		t.Fatal("expected permission to be invalidated")
	}
}
