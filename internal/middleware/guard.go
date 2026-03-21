package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/provider"
)

type GuardMiddleware struct {
	auth    *auth.Auth
	limiter *limiter.Limiter
}

func NewGuardMiddleware(authSvc *auth.Auth, limiterSvc *limiter.Limiter) *GuardMiddleware {
	return &GuardMiddleware{
		auth:    authSvc,
		limiter: limiterSvc,
	}
}

// GuardLLMRequest LLM 请求校验：模型白名单 + 配额 + 限流
func (m *GuardMiddleware) GuardLLMRequest() gin.HandlerFunc {
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

		// 模型白名单检查
		if !m.auth.CheckModel(identity, meta.Model) {
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"message": auth.ErrModelNotAllowed.Error(), "type": "invalid_request_error"}})
			c.Abort()
			return
		}

		// 配额检查
		if !m.auth.HasQuota(identity, meta.EstimatedTokens) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": auth.ErrQuotaExceeded.Error(), "type": "rate_limit_error"}})
			c.Abort()
			return
		}

		// 限流检查
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
		Model:            req.Model,
		EstimatedTokens:  req.EstimatePromptTokens(),
	}, nil
}
