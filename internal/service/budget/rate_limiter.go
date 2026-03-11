package budget

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gateyes/internal/config"
	"gateyes/internal/dto"
	"gateyes/internal/service/usage"
)

type RateLimiter struct {
	config  config.RateLimitConfig
	auth    config.AuthConfig
	backend Backend
	initErr error
}

func NewRateLimiter(cfg config.RateLimitConfig, auth config.AuthConfig) *RateLimiter {
	backend, err := NewBackend(
		cfg.Backend,
		cfg.RedisAddr,
		cfg.RedisPassword,
		cfg.RedisDB,
		cfg.RedisPrefix,
		"rate",
		cfg.RedisStrict,
	)
	return &RateLimiter{
		config:  cfg,
		auth:    auth,
		backend: backend,
		initErr: err,
	}
}

func (r *RateLimiter) InitError() error {
	return r.initErr
}

func (r *RateLimiter) Handle(next http.Handler) http.Handler {
	if !r.config.Enabled {
		return next
	}
	if r.initErr != nil || r.backend == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			http.Error(w, "rate limiter unavailable", http.StatusServiceUnavailable)
		})
	}

	skip := make(map[string]struct{}, len(r.config.SkipPaths))
	for _, path := range r.config.SkipPaths {
		skip[path] = struct{}{}
	}

	rules := normalizeRateRules(r.config)
	defaultCompletion := r.config.DefaultCompletion
	if defaultCompletion <= 0 {
		defaultCompletion = 256
	}

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if _, ok := skip[req.URL.Path]; ok {
			next.ServeHTTP(w, req)
			return
		}

		usage.ClearInternalHeaders(req)

		estimate := usage.EstimateRequest(req, defaultCompletion)
		usage.SetEstimatedHeader(req, estimate)

		subject := resolveSubject(req, r.auth, r.config.TenantHeader, estimate.Model)
		if requiresRateVirtualKey(rules) && strings.TrimSpace(subject.VirtualKey) == "" {
			http.Error(w, "virtual key required", http.StatusUnauthorized)
			return
		}

		now := time.Now().UTC()
		counters, tokenCounters := buildRateCounters(rules, subject, estimate, now)
		result, err := r.backend.Consume(req.Context(), counters)
		if err != nil {
			slog.Error("rate limit consume failed", "error", err)
			http.Error(w, "rate limiter unavailable", http.StatusServiceUnavailable)
			return
		}
		if !result.Allowed {
			retrySeconds := int(result.RetryAfter.Seconds())
			if retrySeconds <= 0 {
				retrySeconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		recorder := newResponseRecorder(w)
		next.ServeHTTP(recorder, req)

		actual := usage.FromHeaders(req)
		if err := adjustTokenCounters(
			req,
			r.backend,
			recorder.status,
			estimate,
			actual,
			tokenCounters,
			"rate limit rollback on error failed",
			"rate limit token adjustment failed",
		); err != nil {
			slog.Warn("rate limit post-processing failed", "error", err)
		}
	})
}

func normalizeRateRules(cfg config.RateLimitConfig) []config.RateLimitRuleConfig {
	rules := make([]config.RateLimitRuleConfig, 0, len(cfg.Rules)+1)
	for _, rule := range cfg.Rules {
		if !rule.Enabled {
			continue
		}
		rules = append(rules, normalizeRateRule(rule, cfg))
	}
	if len(rules) > 0 {
		return rules
	}

	legacy := config.RateLimitRuleConfig{
		Name:              "legacy-rpm",
		Enabled:           true,
		Dimensions:        []string{legacyDimension(cfg.By)},
		RequestsPerMinute: cfg.RequestsPerMinute,
		Burst:             cfg.Burst,
		TenantHeader:      cfg.Header,
	}
	return []config.RateLimitRuleConfig{normalizeRateRule(legacy, cfg)}
}

func normalizeRateRule(rule config.RateLimitRuleConfig, cfg config.RateLimitConfig) config.RateLimitRuleConfig {
	if strings.TrimSpace(rule.Name) == "" {
		rule.Name = "default"
	}
	if len(rule.Dimensions) == 0 {
		rule.Dimensions = []string{"user"}
	}
	if rule.TenantHeader == "" {
		rule.TenantHeader = cfg.TenantHeader
	}
	return rule
}

func buildRateCounters(
	rules []config.RateLimitRuleConfig,
	subject Subject,
	estimate usage.TokenUsage,
	now time.Time,
) ([]dto.BudgetCounter, []dto.BudgetCounter) {
	counters := make([]dto.BudgetCounter, 0, len(rules)*3)
	tokenCounters := make([]dto.BudgetCounter, 0, len(rules))
	secondBucket := now.Unix()
	minuteBucket := now.Unix() / 60

	for _, rule := range rules {
		scope := buildDimensionKey(rule.Dimensions, subject)
		ruleName := sanitizeDimensionValue(rule.Name)

		if rule.RequestsPerSecond > 0 {
			limit := int64(rule.RequestsPerSecond)
			if rule.Burst > 0 {
				limit += int64(rule.Burst)
			}
			counters = append(counters, dto.BudgetCounter{
				Key:   fmt.Sprintf("rl:%s:rps:%d:%s", ruleName, secondBucket, scope),
				Limit: limit,
				Cost:  1,
				TTL:   2 * time.Second,
			})
		}

		if rule.RequestsPerMinute > 0 {
			limit := int64(rule.RequestsPerMinute)
			if rule.Burst > 0 {
				limit += int64(rule.Burst)
			}
			counters = append(counters, dto.BudgetCounter{
				Key:   fmt.Sprintf("rl:%s:rpm:%d:%s", ruleName, minuteBucket, scope),
				Limit: limit,
				Cost:  1,
				TTL:   2 * time.Minute,
			})
		}

		if rule.TokensPerMinute > 0 && estimate.TotalTokens > 0 {
			counter := dto.BudgetCounter{
				Key:   fmt.Sprintf("rl:%s:tpm:%d:%s", ruleName, minuteBucket, scope),
				Limit: rule.TokensPerMinute,
				Cost:  estimate.TotalTokens,
				TTL:   2 * time.Minute,
			}
			counters = append(counters, counter)
			tokenCounters = append(tokenCounters, counter)
		}
	}

	return counters, tokenCounters
}

func requiresRateVirtualKey(rules []config.RateLimitRuleConfig) bool {
	for _, rule := range rules {
		for _, dimension := range rule.Dimensions {
			if strings.EqualFold(strings.TrimSpace(dimension), "virtual_key") {
				return true
			}
		}
	}
	return false
}
