package oidc

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTService issues and verifies access/refresh tokens for admin sessions.
type JWTService struct {
	secret []byte
}

// AccessClaims are embedded in short-lived access tokens.
type AccessClaims struct {
	UserID   string `json:"uid"`
	TenantID string `json:"tid"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// RefreshClaims are embedded in long-lived refresh tokens.
type RefreshClaims struct {
	UserID string `json:"uid"`
	jwt.RegisteredClaims
}

// NewJWTService creates a JWT service with the given signing secret.
func NewJWTService(secret []byte) *JWTService {
	if len(secret) == 0 {
		// Dev fallback; production must set a real secret.
		secret = []byte("gateyes-dev-jwt-secret-change-me")
	}
	return &JWTService{secret: secret}
}

// IssueAccessToken signs a short-lived access token.
func (s *JWTService) IssueAccessToken(userID, tenantID, role string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	claims := AccessClaims{
		UserID:   userID,
		TenantID: tenantID,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			Issuer:    "gateyes",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// IssueRefreshToken signs a long-lived refresh token.
func (s *JWTService) IssueRefreshToken(userID string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	claims := RefreshClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			Issuer:    "gateyes",
			ID:        generateJTI(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// VerifyAccessToken parses and validates an access token.
func (s *JWTService) VerifyAccessToken(tokenStr string) (*AccessClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &AccessClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse access token: %w", err)
	}
	claims, ok := token.Claims.(*AccessClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid access token")
	}
	return claims, nil
}

// VerifyRefreshToken parses and validates a refresh token.
func (s *JWTService) VerifyRefreshToken(tokenStr string) (*RefreshClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &RefreshClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse refresh token: %w", err)
	}
	claims, ok := token.Claims.(*RefreshClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid refresh token")
	}
	return claims, nil
}

func generateJTI() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
