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
	"sync"
	"time"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
)

// AlertService 配额预警服务
type AlertService struct {
	cfg       config.AlertConfig
	store     repository.Store
	httpClient *http.Client

	mu           sync.RWMutex
	notifiedUsers map[string]time.Time // 记录已通知的用户和时间
}

// QuotaAlert 预警消息
type QuotaAlert struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Alert     AlertData `json:"alert"`
}

type AlertData struct {
	TenantID     string `json:"tenant_id"`
	TenantSlug   string `json:"tenant_slug"`
	UserID       string `json:"user_id"`
	UserName     string `json:"user_name"`
	Quota        int    `json:"quota"`
	Used         int    `json:"used"`
	Remaining   int    `json:"remaining"`
	UsagePercent float64 `json:"usage_percent"`
	Threshold    float64 `json:"threshold"`
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

// CheckQuotaUsage 检查配额使用情况并发送预警
func (s *AlertService) CheckQuotaUsage(ctx context.Context, identity *repository.AuthIdentity) {
	if !s.cfg.Enabled || s.cfg.WebhookURL == "" {
		return
	}

	// 已通知过的用户，24小时内不重复通知
	key := fmt.Sprintf("%s:%s", identity.TenantID, identity.UserID)
	s.mu.RLock()
	if lastNotified, ok := s.notifiedUsers[key]; ok {
		if time.Since(lastNotified) < 24*time.Hour {
			s.mu.RUnlock()
			return
		}
	}
	s.mu.RUnlock()

	// 计算使用率
	if identity.Quota <= 0 {
		return
	}
	usagePercent := float64(identity.Used) / float64(identity.Quota) * 100

	// 超过阈值才通知
	if usagePercent < s.cfg.QuotaThreshold*100 {
		return
	}

	// 发送预警
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

	// 记录已通知
	s.mu.Lock()
	s.notifiedUsers[key] = time.Now()
	s.mu.Unlock()
}

func (s *AlertService) sendWebhook(ctx context.Context, alert QuotaAlert) {
	body, err := json.Marshal(alert)
	if err != nil {
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	// 如果配置了签名密钥，添加 HMAC 签名
	if s.cfg.WebhookSecret != "" {
		signature := s.computeSignature(body)
		req.Header.Set("X-Signature", signature)
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
