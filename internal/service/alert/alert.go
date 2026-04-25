package alert

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
)

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

type ProviderStateChange struct {
	ProviderName string `json:"provider_name"`
	Previous     string `json:"previous"`
	Current      string `json:"current"`
	Error        string `json:"error,omitempty"`
}

type BudgetExhausted struct {
	TenantID     string  `json:"tenant_id"`
	ProjectID    string  `json:"project_id,omitempty"`
	APIKeyID     string  `json:"api_key_id,omitempty"`
	ProviderName string  `json:"provider_name,omitempty"`
	Model        string  `json:"model,omitempty"`
	CostUSD      float64 `json:"cost_usd"`
	BudgetScope  string  `json:"budget_scope"`
	SpentUSD     float64 `json:"spent_usd,omitempty"`
	BudgetUSD    float64 `json:"budget_usd,omitempty"`
}

type Alert struct {
	Severity Severity
	Type     string
	Key      string
	Payload  map[string]any
}

type Channel interface {
	Name() string
	Send(ctx context.Context, alert Alert) error
	Match(labels map[string]string) bool
}

type WebhookChannel struct {
	name   string
	client *http.Client
	target string
	secret string
	labels map[string]string
}

func NewWebhookChannel(name, target, secret string, labels map[string]string) *WebhookChannel {
	return &WebhookChannel{
		name:   name,
		client: &http.Client{Timeout: 10 * time.Second},
		target: target,
		secret: secret,
		labels: labels,
	}
}

func (w *WebhookChannel) Name() string { return w.name }

func (w *WebhookChannel) Send(ctx context.Context, alert Alert) error {
	body, err := json.Marshal(struct {
		Severity  string         `json:"severity"`
		Type      string         `json:"type"`
		Timestamp time.Time      `json:"timestamp"`
		Payload   map[string]any `json:"payload"`
	}{
		Severity:  string(alert.Severity),
		Type:      alert.Type,
		Timestamp: time.Now(),
		Payload:   alert.Payload,
	})
	if err != nil {
		return fmt.Errorf("marshal alert: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if w.secret != "" {
		req.Header.Set("X-Signature", w.computeSignature(body))
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

func (w *WebhookChannel) Match(labels map[string]string) bool {
	if len(w.labels) == 0 {
		return true
	}
	for k, v := range w.labels {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func (w *WebhookChannel) computeSignature(body []byte) string {
	mac := hmac.New(sha256.New, []byte(w.secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

type AlertAggregator struct {
	window time.Duration
	mu     sync.RWMutex
	states map[string]time.Time
}

func NewAlertAggregator(window time.Duration) *AlertAggregator {
	if window <= 0 {
		window = 5 * time.Minute
	}
	return &AlertAggregator{
		window: window,
		states: make(map[string]time.Time),
	}
}

func (a *AlertAggregator) ShouldSend(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if last, ok := a.states[key]; ok && time.Since(last) < a.window {
		return false
	}
	a.states[key] = time.Now()
	return true
}

func (a *AlertAggregator) Cleanup() {
	a.mu.Lock()
	defer a.mu.Unlock()
	cutoff := time.Now().Add(-a.window)
	for k, v := range a.states {
		if v.Before(cutoff) {
			delete(a.states, k)
		}
	}
}

type AlertService struct {
	cfg        config.AlertConfig
	store      repository.Store
	channels   []Channel
	aggregator *AlertAggregator

	// Backward compatibility: legacy notifiedUsers map for quota alerts
	mu            sync.RWMutex
	notifiedUsers map[string]time.Time
}

func NewAlertService(cfg config.AlertConfig, store repository.Store) *AlertService {
	s := &AlertService{
		cfg:           cfg,
		store:         store,
		channels:      buildChannels(cfg),
		aggregator:    NewAlertAggregator(time.Duration(cfg.DedupWindowSeconds) * time.Second),
		notifiedUsers: make(map[string]time.Time),
	}
	if !cfg.Enabled {
		s.channels = nil
	}
	return s
}

func buildChannels(cfg config.AlertConfig) []Channel {
	var channels []Channel
	for _, cc := range cfg.Channels {
		switch strings.ToLower(cc.Type) {
		case "webhook":
			ch := NewWebhookChannel(cc.Name, cc.Target, cc.Secret, cc.Labels)
			channels = append(channels, ch)
		}
	}
	// Backward compatibility: create implicit webhook channels from legacy URL fields
	if len(channels) == 0 {
		if cfg.WebhookURL != "" {
			channels = append(channels, NewWebhookChannel("default-webhook", cfg.WebhookURL, cfg.WebhookSecret, nil))
		}
		if cfg.ProviderStateURL != "" {
			channels = append(channels, NewWebhookChannel("provider-state", cfg.ProviderStateURL, cfg.WebhookSecret, map[string]string{"event": "provider_state"}))
		}
		if cfg.BudgetExhaustedURL != "" {
			channels = append(channels, NewWebhookChannel("budget-exhausted", cfg.BudgetExhaustedURL, cfg.WebhookSecret, map[string]string{"event": "budget_exhausted"}))
		}
		if cfg.RequestEventURL != "" {
			channels = append(channels, NewWebhookChannel("request-event", cfg.RequestEventURL, cfg.WebhookSecret, map[string]string{"event": "request_event"}))
		}
		if cfg.ErrorEventURL != "" {
			channels = append(channels, NewWebhookChannel("error-event", cfg.ErrorEventURL, cfg.WebhookSecret, map[string]string{"event": "error_event"}))
		}
	}
	return channels
}

func (s *AlertService) send(ctx context.Context, alert Alert, routeLabels map[string]string) {
	if !s.cfg.Enabled || len(s.channels) == 0 {
		return
	}
	for _, ch := range s.channels {
		if !ch.Match(routeLabels) {
			continue
		}
		go func(c Channel) {
			sendCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := c.Send(sendCtx, alert); err != nil {
				// Swallow: alert delivery failure should not break business flow
			}
		}(ch)
	}
}

func (s *AlertService) CheckQuotaUsage(ctx context.Context, identity *repository.AuthIdentity) {
	if !s.cfg.Enabled || identity == nil {
		return
	}

	key := fmt.Sprintf("%s:%s", identity.TenantID, identity.UserID)
	s.mu.RLock()
	if lastNotified, ok := s.notifiedUsers[key]; ok && time.Since(lastNotified) < 24*time.Hour {
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	if identity.Quota <= 0 {
		return
	}
	usagePercent := float64(identity.Used) / float64(identity.Quota) * 100
	if usagePercent < s.cfg.QuotaThreshold*100 {
		return
	}

	alert := Alert{
		Severity: SeverityWarning,
		Type:     "quota_alert",
		Key:      key,
		Payload: map[string]any{
			"tenant_id":     identity.TenantID,
			"tenant_slug":   identity.TenantSlug,
			"user_id":       identity.UserID,
			"user_name":     identity.UserName,
			"quota":         identity.Quota,
			"used":          identity.Used,
			"remaining":     identity.Quota - identity.Used,
			"usage_percent": usagePercent,
			"threshold":     s.cfg.QuotaThreshold * 100,
		},
	}
	s.send(ctx, alert, map[string]string{"event": "quota_alert", "severity": "warning"})

	s.mu.Lock()
	s.notifiedUsers[key] = time.Now()
	s.mu.Unlock()
}

func (s *AlertService) NotifyProviderStateChanged(ctx context.Context, event ProviderStateChange) {
	if !s.cfg.Enabled {
		return
	}
	severity := SeverityInfo
	if event.Current == "unhealthy" {
		severity = SeverityCritical
	} else if event.Current == "degraded" {
		severity = SeverityWarning
	}
	key := fmt.Sprintf("provider_state:%s", event.ProviderName)
	if !s.aggregator.ShouldSend(key) {
		return
	}
	s.send(ctx, Alert{
		Severity: severity,
		Type:     "provider_state_changed",
		Key:      key,
		Payload:  structToMap(event),
	}, map[string]string{"event": "provider_state", "severity": string(severity)})
}

func (s *AlertService) NotifyBudgetExhausted(ctx context.Context, event BudgetExhausted) {
	if !s.cfg.Enabled {
		return
	}
	key := fmt.Sprintf("budget_exhausted:%s:%s:%s", event.TenantID, event.ProjectID, event.APIKeyID)
	if !s.aggregator.ShouldSend(key) {
		return
	}
	s.send(ctx, Alert{
		Severity: SeverityCritical,
		Type:     "budget_exhausted",
		Key:      key,
		Payload:  structToMap(event),
	}, map[string]string{"event": "budget_exhausted", "severity": "critical"})
}

func (s *AlertService) NotifyRequestEvent(ctx context.Context, payload map[string]any) {
	if !s.cfg.Enabled {
		return
	}
	s.send(ctx, Alert{
		Severity: SeverityInfo,
		Type:     "request_event",
		Key:      "request_event",
		Payload:  payload,
	}, map[string]string{"event": "request_event", "severity": "info"})
}

func (s *AlertService) NotifyErrorEvent(ctx context.Context, payload map[string]any) {
	if !s.cfg.Enabled {
		return
	}
	key := fmt.Sprintf("error_event:%v", payload["error_class"])
	if !s.aggregator.ShouldSend(key) {
		return
	}
	s.send(ctx, Alert{
		Severity: SeverityWarning,
		Type:     "error_event",
		Key:      key,
		Payload:  payload,
	}, map[string]string{"event": "error_event", "severity": "warning"})
}

func structToMap(value any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return map[string]any{}
	}
	return result
}

func stringsTrim(value string) string {
	return strings.TrimSpace(value)
}
