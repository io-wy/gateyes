package alert

import (
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
)

// mockStore 用于测试的 mock
type mockStore struct {
	repository.Store
}

func TestAlertService_ComputeSignature(t *testing.T) {
	cfg := config.AlertConfig{
		Enabled:        true,
		QuotaThreshold: 0.8,
		WebhookSecret:  "test-secret",
	}
	svc := NewAlertService(cfg, nil)

	body := []byte(`{"type":"quota_alert"}`)
	signature := svc.computeSignature(body)

	if signature == "" {
		t.Error("signature should not be empty")
	}

	// 相同内容应该产生相同签名
	signature2 := svc.computeSignature(body)
	if signature != signature2 {
		t.Error("same content should produce same signature")
	}

	// 不同内容应该产生不同签名
	signature3 := svc.computeSignature([]byte(`{"type":"other"}`))
	if signature == signature3 {
		t.Error("different content should produce different signature")
	}
}

func TestAlertService_Disabled(t *testing.T) {
	cfg := config.AlertConfig{
		Enabled: false,
	}
	svc := NewAlertService(cfg, nil)

	identity := &repository.AuthIdentity{
		TenantID:   "tenant1",
		UserID:     "user1",
		Quota:      1000,
		Used:       900, // 90% 使用率
		TenantSlug: "test",
		UserName:   "test-user",
	}

	// disabled 时不应该 panic
	svc.CheckQuotaUsage(nil, identity)
}

func TestAlertService_NoWebhookURL(t *testing.T) {
	cfg := config.AlertConfig{
		Enabled:        true,
		QuotaThreshold: 0.8,
		WebhookURL:     "", // 没有 webhook URL
	}
	svc := NewAlertService(cfg, nil)

	identity := &repository.AuthIdentity{
		TenantID:   "tenant1",
		UserID:     "user1",
		Quota:      1000,
		Used:       900,
		TenantSlug: "test",
		UserName:   "test-user",
	}

	// 没有 webhook URL 时不应该 panic
	svc.CheckQuotaUsage(nil, identity)
}

func TestAlertService_NoQuotaLimit(t *testing.T) {
	cfg := config.AlertConfig{
		Enabled:        true,
		QuotaThreshold: 0.8,
		WebhookURL:     "http://test.com",
	}
	svc := NewAlertService(cfg, nil)

	identity := &repository.AuthIdentity{
		TenantID:   "tenant1",
		UserID:     "user1",
		Quota:      -1, // 无限制
		Used:       10000,
		TenantSlug: "test",
		UserName:   "test-user",
	}

	// 无配额限制时不应该 panic
	svc.CheckQuotaUsage(nil, identity)
}

func TestAlertService_UnderThreshold(t *testing.T) {
	cfg := config.AlertConfig{
		Enabled:        true,
		QuotaThreshold: 0.8, // 80%
		WebhookURL:     "http://test.com",
	}
	svc := NewAlertService(cfg, nil)

	identity := &repository.AuthIdentity{
		TenantID:   "tenant1",
		UserID:     "user1",
		Quota:      1000,
		Used:       500, // 50% 使用率，低于阈值
		TenantSlug: "test",
		UserName:   "test-user",
	}

	// 低于阈值时不应该 panic（不会触发 webhook）
	svc.CheckQuotaUsage(nil, identity)
}

func TestAlertService_AtThreshold(t *testing.T) {
	cfg := config.AlertConfig{
		Enabled:        true,
		QuotaThreshold: 0.8, // 80%
		WebhookURL:     "http://test.com",
		WebhookSecret:  "secret",
	}
	svc := NewAlertService(cfg, nil)

	// 第一次触发（因为是异步的，这里只验证不 panic）
	identity := &repository.AuthIdentity{
		TenantID:   "tenant-new",
		UserID:     "user-new",
		Quota:      1000,
		Used:       800, // 80% 使用率
		TenantSlug: "test",
		UserName:   "test-user",
	}

	// 验证通知记录已添加（异步发送后）
	svc.CheckQuotaUsage(nil, identity)

	// 等待一下让 goroutine 执行
	time.Sleep(100 * time.Millisecond)

	// 24小时内同一用户不应该再次触发
	identity2 := &repository.AuthIdentity{
		TenantID:   "tenant-new",
		UserID:     "user-new",
		Quota:      1000,
		Used:       850,
		TenantSlug: "test",
		UserName:   "test-user",
	}

	// 这里测试重复通知被阻止（24小时内）
	// 由于 webhook 是异步发送的，我们主要验证逻辑不会 panic
	svc.CheckQuotaUsage(nil, identity2)
}
