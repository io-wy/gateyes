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

type AlertService struct {
	cfg        config.AlertConfig
	store      repository.Store
	httpClient *http.Client

	mu            sync.RWMutex
	notifiedUsers map[string]time.Time
}

type Event struct {
	Type      string         `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Payload   map[string]any `json:"payload"`
}

type QuotaAlert struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Alert     AlertData `json:"alert"`
}

type AlertData struct {
	TenantID     string  `json:"tenant_id"`
	TenantSlug   string  `json:"tenant_slug"`
	UserID       string  `json:"user_id"`
	UserName     string  `json:"user_name"`
	Quota        int     `json:"quota"`
	Used         int     `json:"used"`
	Remaining    int     `json:"remaining"`
	UsagePercent float64 `json:"usage_percent"`
	Threshold    float64 `json:"threshold"`
}

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

func NewAlertService(cfg config.AlertConfig, store repository.Store) *AlertService {
	if !cfg.Enabled {
		return &AlertService{cfg: cfg}
	}
	return &AlertService{
		cfg:           cfg,
		store:         store,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
		notifiedUsers: make(map[string]time.Time),
	}
}

func (s *AlertService) CheckQuotaUsage(ctx context.Context, identity *repository.AuthIdentity) {
	if !s.cfg.Enabled || s.cfg.WebhookURL == "" || identity == nil {
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

	alert := QuotaAlert{
		Type:      "quota_alert",
		Timestamp: time.Now(),
		Alert: AlertData{
			TenantID:     identity.TenantID,
			TenantSlug:   identity.TenantSlug,
			UserID:       identity.UserID,
			UserName:     identity.UserName,
			Quota:        identity.Quota,
			Used:         identity.Used,
			Remaining:    identity.Quota - identity.Used,
			UsagePercent: usagePercent,
			Threshold:    s.cfg.QuotaThreshold * 100,
		},
	}
	go s.sendWebhook(ctx, alert)

	s.mu.Lock()
	s.notifiedUsers[key] = time.Now()
	s.mu.Unlock()
}

func (s *AlertService) NotifyProviderStateChanged(ctx context.Context, event ProviderStateChange) {
	if !s.cfg.Enabled || s.cfg.ProviderStateURL == "" {
		return
	}
	go s.send(ctx, s.cfg.ProviderStateURL, "provider_state_changed", structToMap(event))
}

func (s *AlertService) NotifyBudgetExhausted(ctx context.Context, event BudgetExhausted) {
	if !s.cfg.Enabled || s.cfg.BudgetExhaustedURL == "" {
		return
	}
	go s.send(ctx, s.cfg.BudgetExhaustedURL, "budget_exhausted", structToMap(event))
}

func (s *AlertService) NotifyRequestEvent(ctx context.Context, payload map[string]any) {
	if !s.cfg.Enabled || s.cfg.RequestEventURL == "" {
		return
	}
	go s.send(ctx, s.cfg.RequestEventURL, "request_event", payload)
}

func (s *AlertService) NotifyErrorEvent(ctx context.Context, payload map[string]any) {
	if !s.cfg.Enabled || s.cfg.ErrorEventURL == "" {
		return
	}
	go s.send(ctx, s.cfg.ErrorEventURL, "error_event", payload)
}

func (s *AlertService) send(ctx context.Context, url string, eventType string, payload map[string]any) {
	if stringsTrim(url) == "" {
		return
	}
	body, err := json.Marshal(Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Payload:   payload,
	})
	if err != nil {
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if s.cfg.WebhookSecret != "" {
		req.Header.Set("X-Signature", s.computeSignature(body))
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
}

func (s *AlertService) sendWebhook(ctx context.Context, alert QuotaAlert) {
	if !s.cfg.Enabled || stringsTrim(s.cfg.WebhookURL) == "" {
		return
	}
	body, err := json.Marshal(alert)
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if s.cfg.WebhookSecret != "" {
		req.Header.Set("X-Signature", s.computeSignature(body))
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
}

func (s *AlertService) computeSignature(body []byte) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.WebhookSecret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
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
