package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/provider"
)

type Middleware struct {
	auth    *auth.Auth
	limiter *limiter.Limiter
}

func New(store repository.Store, limiterSvc *limiter.Limiter) *Middleware {
	return &Middleware{
		auth:    auth.NewAuth(store),
		limiter: limiterSvc,
	}
}

func (m *Middleware) AuthService() *auth.Auth {
	return m.auth
}

func (m *Middleware) Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		key, secret := m.auth.ExtractKey(c.GetHeader("Authorization"))
		identity, err := m.auth.Authenticate(c.Request.Context(), key, secret)
		if err != nil {
			status := http.StatusUnauthorized
			message := "invalid API key"
			if errors.Is(err, auth.ErrInactiveAPIKey) {
				status = http.StatusForbidden
				message = "inactive API key"
			}
			c.JSON(status, gin.H{"error": gin.H{"message": message, "type": "invalid_request_error"}})
			c.Abort()
			return
		}

		SetIdentity(c, identity)
		c.Next()
	}
}

func (m *Middleware) RequireRoles(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, ok := Identity(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
			c.Abort()
			return
		}
		if !repository.HasRole(identity.Role, roles...) {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func (m *Middleware) GuardLLMRequest() gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, ok := Identity(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
			c.Abort()
			return
		}

		meta, err := extractRequestMeta(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
			c.Abort()
			return
		}
		if !m.auth.CheckModel(identity, meta.Model) {
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"message": auth.ErrModelNotAllowed.Error(), "type": "invalid_request_error"}})
			c.Abort()
			return
		}
		if !m.auth.HasQuota(identity, meta.EstimatedTokens) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": auth.ErrQuotaExceeded.Error(), "type": "rate_limit_error"}})
			c.Abort()
			return
		}
		if m.limiter != nil && !m.limiter.Allow(c.Request.Context(), identity.APIKey, meta.EstimatedTokens) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": "rate limit exceeded", "type": "rate_limit_error"}})
			c.Abort()
			return
		}

		SetRequestMeta(c, meta)
		c.Next()
	}
}

func extractRequestMeta(c *gin.Context) (*RequestMeta, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	var req provider.ResponseRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	req.Normalize()

	return &RequestMeta{
		Model:           req.Model,
		EstimatedTokens: req.EstimatePromptTokens(),
	}, nil
}
