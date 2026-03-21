package alert

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
)

func TestSendWebhookAddsSignatureHeader(t *testing.T) {
	requests := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(context.Background())
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	svc := NewAlertService(config.AlertConfig{
		Enabled:       true,
		WebhookURL:    server.URL,
		WebhookSecret: "secret",
	}, nil)
	svc.sendWebhook(context.Background(), QuotaAlert{
		Type:      "quota_alert",
		Timestamp: time.Now(),
		Alert:     AlertData{TenantID: "tenant-1", UserID: "user-1"},
	})

	select {
	case req := <-requests:
		if req.Header.Get("X-Signature") == "" {
			t.Fatal("sendWebhook() missing X-Signature header")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sendWebhook() timed out waiting for request")
	}
}

func TestCheckQuotaUsageSkipsBelowThresholdAndUnlimitedQuota(t *testing.T) {
	svc := NewAlertService(config.AlertConfig{
		Enabled:        true,
		WebhookURL:     "http://example.com",
		QuotaThreshold: 0.8,
	}, nil)
	svc.CheckQuotaUsage(context.Background(), &repository.AuthIdentity{
		TenantID: "tenant-1",
		UserID:   "user-1",
		Quota:    -1,
		Used:     999,
	})
	if len(svc.notifiedUsers) != 0 {
		t.Fatalf("CheckQuotaUsage(unlimited) notifiedUsers = %d, want %d", len(svc.notifiedUsers), 0)
	}
}
