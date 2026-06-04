package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/filter"
	"github.com/gateyes/gateway/internal/service/provider"
)

type GuardMiddleware struct {
	pipeline *filter.Pipeline
	metrics  MetricsRecorder
}

// NewGuardMiddleware creates a GuardMiddleware that delegates policy checks
// to the provided filter Pipeline.
func NewGuardMiddleware(pipeline *filter.Pipeline, metrics MetricsRecorder) *GuardMiddleware {
	return &GuardMiddleware{
		pipeline: pipeline,
		metrics:  metrics,
	}
}

// GuardLLMRequest validates LLM requests through the filter pipeline.
// Identity extraction and body parsing remain in the middleware layer;
// all policy checks (model whitelist, quota, budget, rate limits) are
// delegated to the Pipeline.
func (m *GuardMiddleware) GuardLLMRequest() gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, ok := Identity(c)
		if !ok {
			recordMiddlewareError(m.metrics, c, metricsResultAuthError, "invalid_api_key")
			c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
			c.Abort()
			return
		}

		meta, body, err := extractRequestMeta(c)
		if err != nil {
			recordMiddlewareError(m.metrics, c, metricsResultClientError, "invalid_request")
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
			c.Abort()
			return
		}

		reqCtx := &filter.RequestContext{
			Context:         c.Request.Context(),
			Identity:        identity,
			Model:           meta.Model,
			EstimatedTokens: meta.EstimatedTokens,
			Body:            body,
		}

		if m.pipeline != nil {
			res := m.pipeline.Execute(reqCtx)
			if res.Action == filter.Block {
				recordMiddlewareError(m.metrics, c, res.MetricsResult, res.MetricsClass)
				c.JSON(res.HTTPStatus, gin.H{"error": gin.H{"message": res.Error.Error(), "type": res.ErrorType}})
				c.Abort()
				return
			}
		}

		SetRequestMeta(c, meta)
		c.Next()
	}
}

func extractRequestMeta(c *gin.Context) (*RequestMeta, []byte, error) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, nil, err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	if isEmbeddingsPath(c.Request.URL.Path) {
		meta, err := extractEmbeddingMeta(body)
		return meta, body, err
	}

	var req provider.ResponseRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, body, err
	}
	req.Normalize()

	return &RequestMeta{
		Model:           req.Model,
		EstimatedTokens: req.EstimateAdmissionTokens(),
	}, body, nil
}

func isEmbeddingsPath(path string) bool {
	return strings.Contains(path, "/embeddings")
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
