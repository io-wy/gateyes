package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/alert"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/budget"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/provider"
)

type GuardMiddleware struct {
	auth       *auth.Auth
	limiter    *limiter.Limiter
	budgetSvc  *budget.Service
	alertSvc   *alert.AlertService
	metrics    MetricsRecorder
}

func NewGuardMiddleware(authSvc *auth.Auth, limiterSvc *limiter.Limiter, budgetSvc *budget.Service, alertSvc *alert.AlertService, metrics MetricsRecorder) *GuardMiddleware {
	return &GuardMiddleware{
		auth:      authSvc,
		limiter:   limiterSvc,
		budgetSvc: budgetSvc,
		alertSvc:  alertSvc,
		metrics:   metrics,
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

		// 预算预检查
		if m.budgetSvc != nil {
			budgetResult, err := m.budgetSvc.Check(c.Request.Context(), budget.CheckRequest{
				Identity:      identity,
				EstimatedCost: 0,
				ProviderName:  "",
				Model:         meta.Model,
			})
			if err != nil {
				recordMiddlewareError(m.metrics, c, metricsResultClientError, "budget_check_error")
				c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "budget check failed", "type": "internal_error"}})
				c.Abort()
				return
			}
			if !budgetResult.Allowed {
				recordMiddlewareError(m.metrics, c, metricsResultRateLimited, "budget_exceeded")
				c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": budgetResult.RejectError.Error(), "type": "rate_limit_error"}})
				c.Abort()
				return
			}
			if budgetResult.AlertSent && m.alertSvc != nil {
				scope := firstSoftAlertScope(budgetResult.Scopes)
				m.alertSvc.NotifyBudgetExhausted(c.Request.Context(), alert.BudgetExhausted{
					TenantID:    identity.TenantID,
					ProjectID:   identity.ProjectID,
					APIKeyID:    identity.APIKeyID,
					Model:       meta.Model,
					BudgetScope: scope,
				})
			}
		}

		// 限流检查
		// P0 fix: 传入 identity.QPS，让用户配置的 QPS 生效
		// P3 fix: 使用 EstimateAdmissionTokens 替代 EstimatePromptTokens，将 output token 也纳入限流
		if m.limiter != nil && !m.limiter.Allow(c.Request.Context(), identity.APIKey, m.auth.EffectiveRateLimitQPS(identity), meta.EstimatedTokens) {
			recordMiddlewareError(m.metrics, c, metricsResultRateLimited, "rate_limited")
			c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": "rate limit exceeded", "type": "rate_limit_error"}})
			c.Abort()
			return
		}
		if m.limiter != nil && !m.limiter.CheckTenant(identity.TenantID, meta.EstimatedTokens) {
			recordMiddlewareError(m.metrics, c, metricsResultRateLimited, "tenant_rate_limited")
			c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": "tenant rate limit exceeded", "type": "rate_limit_error"}})
			c.Abort()
			return
		}
		if m.limiter != nil && !m.limiter.CheckModel(meta.Model, meta.EstimatedTokens) {
			recordMiddlewareError(m.metrics, c, metricsResultRateLimited, "model_rate_limited")
			c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": "model rate limit exceeded", "type": "rate_limit_error"}})
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

	if isEmbeddingsPath(c.Request.URL.Path) {
		return extractEmbeddingMeta(body)
	}

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

func isEmbeddingsPath(path string) bool {
	return strings.Contains(path, "/embeddings")
}

func firstSoftAlertScope(scopes []budget.ScopeResult) string {
	for _, s := range scopes {
		if s.Policy == repository.BudgetPolicySoftAlert {
			return s.Scope
		}
	}
	return "unknown"
}

func extractEmbeddingMeta(body []byte) (*RequestMeta, error) {
	var req struct {
		Model string `json:"model"`
		Input any    `json:"input"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return &RequestMeta{
		Model:           req.Model,
		EstimatedTokens: estimateEmbeddingTokens(req.Input),
	}, nil
}

func estimateEmbeddingTokens(input any) int {
	switch v := input.(type) {
	case string:
		return provider.RoughTokenCount(v)
	case []any:
		total := 0
		for _, item := range v {
			if s, ok := item.(string); ok {
				total += provider.RoughTokenCount(s)
			}
		}
		return total
	case []string:
		total := 0
		for _, s := range v {
			total += provider.RoughTokenCount(s)
		}
		return total
	default:
		return 1
	}
}
