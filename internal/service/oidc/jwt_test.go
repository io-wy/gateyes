package oidc

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWTAccessToken(t *testing.T) {
	svc := NewJWTService([]byte("test-secret"))
	token, err := svc.IssueAccessToken("user-1", "tenant-1", "tenant_admin", time.Hour)
	if err != nil {
		t.Fatalf("IssueAccessToken() error: %v", err)
	}

	claims, err := svc.VerifyAccessToken(token)
	if err != nil {
		t.Fatalf("VerifyAccessToken() error: %v", err)
	}
	if claims.UserID != "user-1" {
		t.Fatalf("UserID = %q, want %q", claims.UserID, "user-1")
	}
	if claims.TenantID != "tenant-1" {
		t.Fatalf("TenantID = %q, want %q", claims.TenantID, "tenant-1")
	}
	if claims.Role != "tenant_admin" {
		t.Fatalf("Role = %q, want %q", claims.Role, "tenant_admin")
	}
}

func TestJWTRefreshToken(t *testing.T) {
	svc := NewJWTService([]byte("test-secret"))
	token, err := svc.IssueRefreshToken("user-1", time.Hour)
	if err != nil {
		t.Fatalf("IssueRefreshToken() error: %v", err)
	}

	claims, err := svc.VerifyRefreshToken(token)
	if err != nil {
		t.Fatalf("VerifyRefreshToken() error: %v", err)
	}
	if claims.UserID != "user-1" {
		t.Fatalf("UserID = %q, want %q", claims.UserID, "user-1")
	}
}

func TestJWTVerifyInvalid(t *testing.T) {
	svc := NewJWTService([]byte("test-secret"))
	if _, err := svc.VerifyAccessToken("not-a-jwt"); err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestJWTWrongSecret(t *testing.T) {
	svc1 := NewJWTService([]byte("secret-one"))
	svc2 := NewJWTService([]byte("secret-two"))
	token, err := svc1.IssueAccessToken("user-1", "tenant-1", "tenant_admin", time.Hour)
	if err != nil {
		t.Fatalf("IssueAccessToken() error: %v", err)
	}
	if _, err := svc2.VerifyAccessToken(token); err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestJWTExpired(t *testing.T) {
	svc := NewJWTService([]byte("test-secret"))
	claims := AccessClaims{
		UserID:   "user-1",
		TenantID: "tenant-1",
		Role:     "tenant_admin",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(-time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC().Add(-2 * time.Hour)),
			Issuer:    "gateyes",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}
	if _, err := svc.VerifyAccessToken(tokenStr); err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestIsJWTLike(t *testing.T) {
	validJWT := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	if !IsJWTLike(validJWT) {
		t.Fatal("expected valid JWT shape to be recognized")
	}
	if IsJWTLike("gk-abc123:secret") {
		t.Fatal("expected API key not to look like JWT")
	}
	if IsJWTLike("not.valid") {
		t.Fatal("expected two-segment string not to look like JWT")
	}
}
