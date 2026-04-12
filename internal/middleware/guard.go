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
	metrics MetricsRecorder
}

func NewGuardMiddleware(authSvc *auth.Auth, limiterSvc *limiter.Limiter, metrics MetricsRecorder) *GuardMiddleware {
	return &GuardMiddleware{
		auth:    authSvc,
		limiter: limiterSvc,
		metrics: metrics,
	}
}

// GuardLLMRequest LLM 请求校验：模型白名单 + 配额 + 限流
func (m *GuardMiddleware) GuardLLMRequest() gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, ok := Identity(c)
		if !ok {
			recordMiddlewareError(m.metrics, c, metricsResultAuthError, "invalid_api_key")
			c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
			c.Abort()
			return
		}

		meta, err := extractRequestMeta(c)
		if err != nil {
			recordMiddlewareError(m.metrics, c, metricsResultClientError, "invalid_request")
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
			c.Abort()
			return
		}

		// 模型白名单检查
		if !m.auth.CheckModel(identity, meta.Model) {
			recordMiddlewareError(m.metrics, c, metricsResultAuthError, "model_not_allowed")
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"message": auth.ErrModelNotAllowed.Error(), "type": "invalid_request_error"}})
			c.Abort()
			return
		}

		// 配额检查
		if !m.auth.HasQuota(identity, meta.EstimatedTokens) {
			recordMiddlewareError(m.metrics, c, metricsResultRateLimited, "quota_exceeded")
			c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": auth.ErrQuotaExceeded.Error(), "type": "rate_limit_error"}})
			c.Abort()
			return
		}

		// 限流检查
		// P0 fix: 传入 identity.QPS，让用户配置的 QPS 生效
		// P3 fix: 使用 EstimateAdmissionTokens 替代 EstimatePromptTokens，将 output token 也纳入限流
		if m.limiter != nil && !m.limiter.Allow(c.Request.Context(), identity.APIKey, identity.QPS, meta.EstimatedTokens) {
			recordMiddlewareError(m.metrics, c, metricsResultRateLimited, "rate_limited")
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
		EstimatedTokens: req.EstimateAdmissionTokens(),
	}, nil
}
