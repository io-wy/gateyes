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

type Quota struct {
	config  config.QuotaConfig
	auth    config.AuthConfig
	service BudgetService
}

type quotaRule struct {
	name             string
	dimensions       []string
	tenantHeader     string
	requestsPerDay   int64
	requestsPerMonth int64
	tokensPerDay     int64
	tokensPerMonth   int64
	legacyRequests   int64
	legacyWindow     time.Duration
}

func NewQuota(cfg config.QuotaConfig, auth config.AuthConfig) *Quota {
	return &Quota{
		config:  cfg,
		auth:    auth,
		service: newBudgetService(cfg.Backend, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.RedisPrefix, "quota"),
	}
}

func (q *Quota) Middleware() Middleware {
	if !q.config.Enabled {
		return Noop()
	}

	skip := make(map[string]struct{}, len(q.config.SkipPaths))
	for _, path := range q.config.SkipPaths {
		skip[path] = struct{}{}
	}

	rules := normalizeQuotaRules(q.config)
	defaultCompletion := q.config.DefaultCompletion
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
			baseSubject := resolveQuotaSubject(req, q.auth, q.config.TenantHeader, estimate.Model)
			now := time.Now().UTC()

			counters, tokenCounters := q.buildCounters(req, rules, baseSubject, estimate, now)
			result, err := q.service.Consume(req.Context(), counters)
			if err != nil {
				slog.Error("quota consume failed", "error", err)
				http.Error(w, "quota service unavailable", http.StatusServiceUnavailable)
				return
			}
			if !result.Allowed {
				retrySeconds := int(result.RetryAfter.Seconds())
				if retrySeconds <= 0 {
					retrySeconds = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
				http.Error(w, "quota exceeded", http.StatusTooManyRequests)
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
			if err := q.service.Adjust(req.Context(), adjustments); err != nil {
				slog.Warn("quota token adjustment failed", "error", err)
			}
		})
	}
}

func normalizeQuotaRules(cfg config.QuotaConfig) []quotaRule {
	rules := make([]quotaRule, 0, len(cfg.Rules)+2)
	for _, input := range cfg.Rules {
		if !input.Enabled {
			continue
		}
		dimensions := normalizeDimensionList(input.Dimensions)
		rules = append(rules, quotaRule{
			name:             withDefaultName(input.Name, "rule"),
			dimensions:       dimensions,
			tenantHeader:     firstNonEmpty(input.TenantHeader, cfg.TenantHeader),
			requestsPerDay:   input.RequestsPerDay,
			requestsPerMonth: input.RequestsPerMonth,
			tokensPerDay:     input.TokensPerDay,
			tokensPerMonth:   input.TokensPerMonth,
		})
	}
	if len(rules) > 0 {
		return rules
	}

	legacyDimensions := []string{legacyDimension(cfg.By)}
	rules = append(rules, quotaRule{
		name:           "legacy-window",
		dimensions:     legacyDimensions,
		tenantHeader:   firstNonEmpty(cfg.Header, cfg.TenantHeader),
		legacyRequests: int64(cfg.Requests),
		legacyWindow:   cfg.Window.Duration,
	})

	if cfg.TokensPerDay > 0 || cfg.TokensPerMonth > 0 {
		rules = append(rules, quotaRule{
			name:           "legacy-token",
			dimensions:     legacyDimensions,
			tenantHeader:   firstNonEmpty(cfg.Header, cfg.TenantHeader),
			tokensPerDay:   cfg.TokensPerDay,
			tokensPerMonth: cfg.TokensPerMonth,
		})
	}

	return rules
}

func (q *Quota) buildCounters(
	req *http.Request,
	rules []quotaRule,
	baseSubject quotaSubject,
	usage tokenUsage,
	now time.Time,
) ([]budgetCounter, []budgetCounter) {
	counters := make([]budgetCounter, 0, len(rules)*5)
	tokenCounters := make([]budgetCounter, 0, len(rules)*2)

	for _, rule := range rules {
		subject := baseSubject
		if rule.tenantHeader != "" && !strings.EqualFold(rule.tenantHeader, q.config.TenantHeader) {
			subject.Tenant = strings.TrimSpace(req.Header.Get(rule.tenantHeader))
		}
		scope := buildDimensionKey(rule.dimensions, subject)
		name := sanitizeDimensionValue(rule.name)

		if rule.legacyRequests > 0 && rule.legacyWindow > 0 {
			windowSeconds := int64(rule.legacyWindow.Seconds())
			if windowSeconds <= 0 {
				windowSeconds = int64((24 * time.Hour).Seconds())
			}
			bucket := now.Unix() / windowSeconds
			counters = append(counters, budgetCounter{
				Key:   fmt.Sprintf("qt:%s:req-window:%d:%s", name, bucket, scope),
				Limit: rule.legacyRequests,
				Cost:  1,
				TTL:   rule.legacyWindow + time.Minute,
			})
		}

		if rule.requestsPerDay > 0 {
			dayKey, dayTTL := dayBucket(now)
			counters = append(counters, budgetCounter{
				Key:   fmt.Sprintf("qt:%s:req-day:%s:%s", name, dayKey, scope),
				Limit: rule.requestsPerDay,
				Cost:  1,
				TTL:   dayTTL,
			})
		}

		if rule.requestsPerMonth > 0 {
			monthKey, monthTTL := monthBucket(now)
			counters = append(counters, budgetCounter{
				Key:   fmt.Sprintf("qt:%s:req-month:%s:%s", name, monthKey, scope),
				Limit: rule.requestsPerMonth,
				Cost:  1,
				TTL:   monthTTL,
			})
		}

		if rule.tokensPerDay > 0 && usage.TotalTokens > 0 {
			dayKey, dayTTL := dayBucket(now)
			counter := budgetCounter{
				Key:   fmt.Sprintf("qt:%s:tok-day:%s:%s", name, dayKey, scope),
				Limit: rule.tokensPerDay,
				Cost:  usage.TotalTokens,
				TTL:   dayTTL,
			}
			counters = append(counters, counter)
			tokenCounters = append(tokenCounters, counter)
		}

		if rule.tokensPerMonth > 0 && usage.TotalTokens > 0 {
			monthKey, monthTTL := monthBucket(now)
			counter := budgetCounter{
				Key:   fmt.Sprintf("qt:%s:tok-month:%s:%s", name, monthKey, scope),
				Limit: rule.tokensPerMonth,
				Cost:  usage.TotalTokens,
				TTL:   monthTTL,
			}
			counters = append(counters, counter)
			tokenCounters = append(tokenCounters, counter)
		}
	}

	return counters, tokenCounters
}

func dayBucket(now time.Time) (string, time.Duration) {
	date := now.Format("20060102")
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return date, next.Sub(now) + time.Minute
}

func monthBucket(now time.Time) (string, time.Duration) {
	date := now.Format("200601")
	next := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	return date, next.Sub(now) + time.Minute
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func withDefaultName(name, fallback string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	return fallback
}
