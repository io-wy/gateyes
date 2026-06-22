package alert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
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

// The TestAlertService_* cases below exercise every branch of CheckQuotaUsage.
// They assert the observable side effect — whether the user was recorded in
// notifiedUsers — rather than merely "does not panic". notifiedUsers is written
// synchronously inside CheckQuotaUsage (only the webhook POST is async), so it
// is a stable, non-flaky signal for "an alert was triggered".

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

	// Disabled: must be a no-op, no alert recorded.
	svc.CheckQuotaUsage(context.Background(), identity)
	if len(svc.notifiedUsers) != 0 {
		t.Errorf("CheckQuotaUsage(disabled) recorded %d notifications, want 0", len(svc.notifiedUsers))
	}
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

	// No webhook channel configured: send() is a no-op, but the threshold is still
	// breached, so the user is still marked notified (this prevents a channel added
	// later from replaying the same alert within the dedup window).
	svc.CheckQuotaUsage(context.Background(), identity)
	if len(svc.notifiedUsers) != 1 {
		t.Errorf("CheckQuotaUsage(no webhook URL) recorded %d notifications, want 1", len(svc.notifiedUsers))
	}
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

	// Unlimited quota (Quota <= 0): never alerts.
	svc.CheckQuotaUsage(context.Background(), identity)
	if len(svc.notifiedUsers) != 0 {
		t.Errorf("CheckQuotaUsage(unlimited quota) recorded %d notifications, want 0", len(svc.notifiedUsers))
	}
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

	// Below threshold (50% < 80%): no alert.
	svc.CheckQuotaUsage(context.Background(), identity)
	if len(svc.notifiedUsers) != 0 {
		t.Errorf("CheckQuotaUsage(under threshold) recorded %d notifications, want 0", len(svc.notifiedUsers))
	}
}

func TestAlertService_AtThreshold(t *testing.T) {
	cfg := config.AlertConfig{
		Enabled:        true,
		QuotaThreshold: 0.8, // 80%
		WebhookURL:     "http://test.com",
		WebhookSecret:  "secret",
	}
	svc := NewAlertService(cfg, nil)

	identity := &repository.AuthIdentity{
		TenantID:   "tenant-new",
		UserID:     "user-new",
		Quota:      1000,
		Used:       800, // exactly at the 80% threshold
		TenantSlug: "test",
		UserName:   "test-user",
	}

	// At threshold: the user must be recorded as notified. No sleep is needed —
	// notifiedUsers is stamped synchronously; only the webhook POST is async.
	svc.CheckQuotaUsage(context.Background(), identity)
	if len(svc.notifiedUsers) != 1 {
		t.Fatalf("CheckQuotaUsage(at threshold) recorded %d notifications, want 1", len(svc.notifiedUsers))
	}
	firstNotified := svc.notifiedUsers["tenant-new:user-new"]
	if firstNotified.IsZero() {
		t.Fatal("CheckQuotaUsage(at threshold) did not stamp a notification time")
	}

	// Same user again within the 24h window: must be deduplicated, not re-stamped.
	identity2 := *identity
	identity2.Used = 850
	svc.CheckQuotaUsage(context.Background(), &identity2)
	if len(svc.notifiedUsers) != 1 {
		t.Fatalf("CheckQuotaUsage(dedup) recorded %d notifications, want 1", len(svc.notifiedUsers))
	}
	if got := svc.notifiedUsers["tenant-new:user-new"]; !got.Equal(firstNotified) {
		t.Errorf("CheckQuotaUsage(dedup) re-stamped notification time: got %v, want unchanged %v", got, firstNotified)
	}
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
