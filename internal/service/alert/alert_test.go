package alert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
)

func TestWebhookChannel_ComputeSignature(t *testing.T) {
	ch := NewWebhookChannel("test", "http://test.com", "test-secret", nil)

	body := []byte(`{"type":"quota_alert"}`)
	signature := ch.computeSignature(body)

	if signature == "" {
		t.Error("signature should not be empty")
	}

	// 相同内容应该产生相同签名
	signature2 := ch.computeSignature(body)
	if signature != signature2 {
		t.Error("same content should produce same signature")
	}

	// 不同内容应该产生不同签名
	signature3 := ch.computeSignature([]byte(`{"type":"other"}`))
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
	svc.CheckQuotaUsage(context.Background(), identity)
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
	svc.CheckQuotaUsage(context.Background(), identity)
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
	svc.CheckQuotaUsage(context.Background(), identity)
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
	svc.CheckQuotaUsage(context.Background(), identity)
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
	svc.CheckQuotaUsage(context.Background(), identity)

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
	svc.CheckQuotaUsage(context.Background(), identity2)
}

func TestAlertService_AdditionalWebhookTypes(t *testing.T) {
	requests := make(chan map[string]any, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode webhook body: %v", err)
		}
		requests <- payload
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	svc := NewAlertService(config.AlertConfig{
		Enabled:            true,
		WebhookSecret:      "secret",
		ProviderStateURL:   server.URL,
		BudgetExhaustedURL: server.URL,
		RequestEventURL:    server.URL,
		ErrorEventURL:      server.URL,
	}, nil)

	svc.NotifyProviderStateChanged(context.Background(), ProviderStateChange{
		ProviderName: "openai-a",
		Previous:     "healthy",
		Current:      "unhealthy",
		Error:        "boom",
	})
	svc.NotifyBudgetExhausted(context.Background(), BudgetExhausted{
		TenantID:    "tenant-1",
		BudgetScope: "project",
		CostUSD:     1.2,
	})
	svc.NotifyRequestEvent(context.Background(), map[string]any{"status": "success"})
	svc.NotifyErrorEvent(context.Background(), map[string]any{"status": "error"})

	received := make(map[string]bool)
	deadline := time.After(2 * time.Second)
	for len(received) < 4 {
		select {
		case payload := <-requests:
			received[payload["type"].(string)] = true
		case <-deadline:
			t.Fatalf("received webhook types = %#v, want provider_state_changed/budget_exhausted/request_event/error_event", received)
		}
	}
}

func TestAlertAggregator_Dedup(t *testing.T) {
	agg := NewAlertAggregator(100 * time.Millisecond)

	if !agg.ShouldSend("key1") {
		t.Error("first send should be allowed")
	}
	if agg.ShouldSend("key1") {
		t.Error("duplicate within window should be blocked")
	}
	if !agg.ShouldSend("key2") {
		t.Error("different key should be allowed")
	}

	time.Sleep(150 * time.Millisecond)
	if !agg.ShouldSend("key1") {
		t.Error("after window expires, send should be allowed again")
	}
}

func TestAlertAggregator_Cleanup(t *testing.T) {
	agg := NewAlertAggregator(50 * time.Millisecond)
	agg.ShouldSend("old-key")

	time.Sleep(100 * time.Millisecond)
	agg.Cleanup()

	agg.mu.RLock()
	_, exists := agg.states["old-key"]
	agg.mu.RUnlock()
	if exists {
		t.Error("expired key should be cleaned up")
	}
}
