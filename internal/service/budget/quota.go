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

type Quota struct {
	config  config.QuotaConfig
	auth    config.AuthConfig
	backend Backend
	initErr error
}

func NewQuota(cfg config.QuotaConfig, auth config.AuthConfig) *Quota {
	backend, err := NewBackend(
		cfg.Backend,
		cfg.RedisAddr,
		cfg.RedisPassword,
		cfg.RedisDB,
		cfg.RedisPrefix,
		"quota",
		cfg.RedisStrict,
	)
	return &Quota{
		config:  cfg,
		auth:    auth,
		backend: backend,
		initErr: err,
	}
}

func (q *Quota) InitError() error {
	return q.initErr
}

func (q *Quota) Handle(next http.Handler) http.Handler {
	if !q.config.Enabled {
		return next
	}
	if q.initErr != nil || q.backend == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			http.Error(w, "quota service unavailable", http.StatusServiceUnavailable)
		})
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

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if _, ok := skip[req.URL.Path]; ok {
			next.ServeHTTP(w, req)
			return
		}

		usage.ClearInternalHeaders(req)

		estimate := usage.EstimateRequest(req, defaultCompletion)
		usage.SetEstimatedHeader(req, estimate)

		baseSubject := resolveSubject(req, q.auth, q.config.TenantHeader, estimate.Model)
		if requiresQuotaVirtualKey(rules) && strings.TrimSpace(baseSubject.VirtualKey) == "" {
			http.Error(w, "virtual key required", http.StatusUnauthorized)
			return
		}

		now := time.Now().UTC()
		counters, tokenCounters := q.buildCounters(req, rules, baseSubject, estimate, now)
		result, err := q.backend.Consume(req.Context(), counters)
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

		recorder := newResponseRecorder(w)
		next.ServeHTTP(recorder, req)

		actual := usage.FromHeaders(req)
		if err := adjustTokenCounters(
			req,
			q.backend,
			recorder.status,
			estimate,
			actual,
			tokenCounters,
			"quota rollback on error failed",
			"quota token adjustment failed",
		); err != nil {
			slog.Warn("quota post-processing failed", "error", err)
		}
	})
}

func normalizeQuotaRules(cfg config.QuotaConfig) []quotaRule {
	rules := make([]quotaRule, 0, len(cfg.Rules)+2)
	for _, input := range cfg.Rules {
		if !input.Enabled {
			continue
		}
		rules = append(rules, quotaRule{
			name:             withDefaultName(input.Name, "rule"),
			dimensions:       normalizeDimensionList(input.Dimensions),
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
	baseSubject Subject,
	estimate usage.TokenUsage,
	now time.Time,
) ([]dto.BudgetCounter, []dto.BudgetCounter) {
	counters := make([]dto.BudgetCounter, 0, len(rules)*5)
	tokenCounters := make([]dto.BudgetCounter, 0, len(rules)*2)

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
			counters = append(counters, dto.BudgetCounter{
				Key:   fmt.Sprintf("qt:%s:req-window:%d:%s", name, bucket, scope),
				Limit: rule.legacyRequests,
				Cost:  1,
				TTL:   rule.legacyWindow + time.Minute,
			})
		}

		if rule.requestsPerDay > 0 {
			dayKey, dayTTL := dayBucket(now)
			counters = append(counters, dto.BudgetCounter{
				Key:   fmt.Sprintf("qt:%s:req-day:%s:%s", name, dayKey, scope),
				Limit: rule.requestsPerDay,
				Cost:  1,
				TTL:   dayTTL,
			})
		}

		if rule.requestsPerMonth > 0 {
			monthKey, monthTTL := monthBucket(now)
			counters = append(counters, dto.BudgetCounter{
				Key:   fmt.Sprintf("qt:%s:req-month:%s:%s", name, monthKey, scope),
				Limit: rule.requestsPerMonth,
				Cost:  1,
				TTL:   monthTTL,
			})
		}

		if rule.tokensPerDay > 0 && estimate.TotalTokens > 0 {
			dayKey, dayTTL := dayBucket(now)
			counter := dto.BudgetCounter{
				Key:   fmt.Sprintf("qt:%s:tok-day:%s:%s", name, dayKey, scope),
				Limit: rule.tokensPerDay,
				Cost:  estimate.TotalTokens,
				TTL:   dayTTL,
			}
			counters = append(counters, counter)
			tokenCounters = append(tokenCounters, counter)
		}

		if rule.tokensPerMonth > 0 && estimate.TotalTokens > 0 {
			monthKey, monthTTL := monthBucket(now)
			counter := dto.BudgetCounter{
				Key:   fmt.Sprintf("qt:%s:tok-month:%s:%s", name, monthKey, scope),
				Limit: rule.tokensPerMonth,
				Cost:  estimate.TotalTokens,
				TTL:   monthTTL,
			}
			counters = append(counters, counter)
			tokenCounters = append(tokenCounters, counter)
		}
	}

	return counters, tokenCounters
}

func requiresQuotaVirtualKey(rules []quotaRule) bool {
	for _, rule := range rules {
		for _, dimension := range rule.dimensions {
			if strings.EqualFold(strings.TrimSpace(dimension), "virtual_key") {
				return true
			}
		}
	}
	return false
}
