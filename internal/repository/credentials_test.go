package repository

import "testing"

func TestGenerateTokenPrefixAndLength(t *testing.T) {
	got, err := GenerateToken("gk-", 8)
	if err != nil {
		t.Fatalf("GenerateToken() error: %v", err)
	}
	if want := 3 + 16; len(got) != want {
		t.Fatalf("len(GenerateToken()) = %d, want %d", len(got), want)
	}
	if got[:3] != "gk-" {
		t.Fatalf("GenerateToken() prefix = %q, want %q", got[:3], "gk-")
	}
}

func TestHashSecretVerifySecretAndRoles(t *testing.T) {
	hash := HashSecret("secret-value")
	if hash == "" {
		t.Fatal("HashSecret(secret-value) = empty, want non-empty")
	}
	if HashSecret("") != "" {
		t.Fatalf("HashSecret(\"\") = %q, want %q", HashSecret(""), "")
	}
	if !VerifySecret("secret-value", hash) {
		t.Fatal("VerifySecret(secret-value, hash) = false, want true")
	}
	if VerifySecret("wrong", hash) {
		t.Fatal("VerifySecret(wrong, hash) = true, want false")
	}
	if !VerifySecret("", "") {
		t.Fatal("VerifySecret(\"\", \"\") = false, want true")
	}
	if !IsAdminRole(RoleTenantAdmin) || !IsAdminRole(RoleSuperAdmin) || IsAdminRole(RoleTenantUser) {
		t.Fatal("IsAdminRole() returned unexpected result")
	}
	if !HasRole(RoleTenantAdmin, RoleTenantUser, RoleTenantAdmin) {
		t.Fatal("HasRole() = false, want true")
	}
	if HasRole(RoleTenantUser, RoleSuperAdmin) {
		t.Fatal("HasRole() = true, want false")
	}
}
