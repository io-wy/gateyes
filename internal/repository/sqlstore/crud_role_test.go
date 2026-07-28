package sqlstore

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
)

func TestCreateAndGetRole(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	role, err := store.CreateRole(ctx, repository.CreateRoleParams{
		TenantID:      "",
		Name:          "custom-role",
		Description:   "A custom role",
		PermissionIDs: nil,
	})
	if err != nil {
		t.Fatalf("CreateRole() error: %v", err)
	}
	if role.Name != "custom-role" {
		t.Fatalf("CreateRole() name = %q, want %q", role.Name, "custom-role")
	}

	got, err := store.GetRole(ctx, "", role.ID)
	if err != nil {
		t.Fatalf("GetRole() error: %v", err)
	}
	if got.ID != role.ID {
		t.Fatalf("GetRole() id = %q, want %q", got.ID, role.ID)
	}
}

func TestRoleCRUD(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	perms, err := store.ListPermissions(ctx)
	if err != nil {
		t.Fatalf("ListPermissions() error: %v", err)
	}
	if len(perms) == 0 {
		t.Fatal("expected seed permissions")
	}

	var permIDs []string
	var targetPerm string
	for _, p := range perms {
		permIDs = append(permIDs, p.ID)
		if targetPerm == "" {
			targetPerm = p.Code
		}
	}

	role, err := store.CreateRole(ctx, repository.CreateRoleParams{
		Name:          "admin-lite",
		Description:   "",
		PermissionIDs: permIDs,
	})
	if err != nil {
		t.Fatalf("CreateRole() error: %v", err)
	}
	if len(role.Permissions) != len(permIDs) {
		t.Fatalf("CreateRole() permissions = %d, want %d", len(role.Permissions), len(permIDs))
	}

	updated, err := store.UpdateRole(ctx, "", role.ID, repository.UpdateRoleParams{
		Name:          strPtr("admin-lite-renamed"),
		Description:   strPtr("renamed"),
		PermissionIDs: &[]string{permIDs[0]},
	})
	if err != nil {
		t.Fatalf("UpdateRole() error: %v", err)
	}
	if updated.Name != "admin-lite-renamed" {
		t.Fatalf("UpdateRole() name = %q, want %q", updated.Name, "admin-lite-renamed")
	}
	if len(updated.Permissions) != 1 {
		t.Fatalf("UpdateRole() permissions = %d, want 1", len(updated.Permissions))
	}

	if err := store.DeleteRole(ctx, "", role.ID); err != nil {
		t.Fatalf("DeleteRole() error: %v", err)
	}
	if _, err := store.GetRole(ctx, "", role.ID); err != repository.ErrNotFound {
		t.Fatalf("GetRole(deleted) error = %v, want %v", err, repository.ErrNotFound)
	}
}

func TestSystemRoleProtection(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.UpdateRole(ctx, "", "role_super_admin", repository.UpdateRoleParams{
		Name: strPtr("hacked"),
	})
	if err == nil {
		t.Fatal("expected error updating system role")
	}

	if err := store.DeleteRole(ctx, "", "role_super_admin"); err == nil {
		t.Fatal("expected error deleting system role")
	}
}

func TestGetUserPermissions(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Migration seeds role_super_admin with all permissions and links existing users.
	perms, err := store.GetUserPermissions(ctx, "")
	if err != nil {
		t.Fatalf("GetUserPermissions() error: %v", err)
	}
	// Empty user id has no roles/permissions.
	if len(perms) != 0 {
		t.Fatalf("GetUserPermissions() = %d, want 0", len(perms))
	}
}

func strPtr(s string) *string { return &s }
