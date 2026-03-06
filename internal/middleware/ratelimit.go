package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gateyes/internal/config"
)

type RateLimiter struct {
	config  config.RateLimitConfig
	auth    config.AuthConfig
	service BudgetService
}

func NewRateLimiter(cfg config.RateLimitConfig, auth config.AuthConfig) *RateLimiter {
	return &RateLimiter{
		config:  cfg,
		auth:    auth,
		service: newBudgetService(cfg.Backend, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.RedisPrefix, "rate"),
	}
}

func (r *RateLimiter) Middleware() Middleware {
	if !r.config.Enabled {
		return Noop()
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

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if _, ok := skip[req.URL.Path]; ok {
				next.ServeHTTP(w, req)
				return
			}

			clearInternalUsageHeaders(req)

			estimate := estimateTokenUsage(req, defaultCompletion)
			subject := resolveQuotaSubject(req, r.auth, r.config.TenantHeader, estimate.Model)
			now := time.Now().UTC()

			counters, tokenCounters := buildRateCounters(rules, subject, estimate, now)
			result, err := r.service.Consume(req.Context(), counters)
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

			next.ServeHTTP(w, req)

			if len(tokenCounters) == 0 || estimate.TotalTokens <= 0 {
				return
			}

			actual := usageFromRequestMeta(req)
			if actual.TotalTokens <= 0 {
				return
			}

			delta := actual.TotalTokens - estimate.TotalTokens
			if delta == 0 {
				return
			}

			adjustments := make([]budgetAdjustment, 0, len(tokenCounters))
			for _, counter := range tokenCounters {
				adjustments = append(adjustments, budgetAdjustment{
					Key:   counter.Key,
					Delta: delta,
					TTL:   counter.TTL,
				})
			}
			if err := r.service.Adjust(req.Context(), adjustments); err != nil {
				slog.Warn("rate limit token adjustment failed", "error", err)
			}
		})
	}
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
	subject quotaSubject,
	usage tokenUsage,
	now time.Time,
) ([]budgetCounter, []budgetCounter) {
	counters := make([]budgetCounter, 0, len(rules)*3)
	tokenCounters := make([]budgetCounter, 0, len(rules))
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
			counters = append(counters, budgetCounter{
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
			counters = append(counters, budgetCounter{
				Key:   fmt.Sprintf("rl:%s:rpm:%d:%s", ruleName, minuteBucket, scope),
				Limit: limit,
				Cost:  1,
				TTL:   2 * time.Minute,
			})
		}

		if rule.TokensPerMinute > 0 && usage.TotalTokens > 0 {
			counter := budgetCounter{
				Key:   fmt.Sprintf("rl:%s:tpm:%d:%s", ruleName, minuteBucket, scope),
				Limit: rule.TokensPerMinute,
				Cost:  usage.TotalTokens,
				TTL:   2 * time.Minute,
			}
			counters = append(counters, counter)
			tokenCounters = append(tokenCounters, counter)
		}
	}

	return counters, tokenCounters
}

func legacyDimension(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "ip":
		return "ip"
	case "header":
		return "tenant"
	case "auth":
		fallthrough
	default:
		return "user"
	}
}

func newBudgetService(backend, addr, password string, db int, prefix, suffix string) BudgetService {
	normalizedBackend := strings.ToLower(strings.TrimSpace(backend))
	if normalizedBackend == "redis" {
		service, err := NewRedisBudgetService(addr, password, db, budgetPrefix(prefix, suffix))
		if err != nil {
			slog.Warn("failed to init redis budget service, fallback to memory", "error", err)
		} else {
			return service
		}
	}
	return NewMemoryBudgetService()
}

func budgetPrefix(prefix, suffix string) string {
	base := strings.TrimSpace(prefix)
	if base == "" {
		base = "gateyes"
	}
	sfx := strings.TrimSpace(suffix)
	if sfx == "" {
		return base
	}
	return base + ":" + sfx
}
