package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"gateyes/internal/requestmeta"

	dto "github.com/prometheus/client_model/go"
)

func TestMetricsCardinalityLimiterCollapsesToOther(t *testing.T) {
	metrics := NewMetricsWithOptions("gateyes_test", MetricsOptions{
		MaxVirtualKeyLabels: 1,
		MaxModelLabels:      1,
		MaxProviderLabels:   1,
		MaskVirtualKey:      true,
		LabelValueMaxLen:    64,
	})

	handler := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), metrics.Middleware(true))

	for i := 1; i <= 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req.Header.Set(requestmeta.HeaderVirtualKey, fmt.Sprintf("vk-%d", i))
		req.Header.Set(requestmeta.HeaderResolvedModel, fmt.Sprintf("model-%d", i))
		req.Header.Set(requestmeta.HeaderResolvedProvider, fmt.Sprintf("provider-%d", i))

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d expected 200, got %d", i, rec.Code)
		}
	}

	snapshot := &dto.Metric{}
	if err := metrics.gatewayRequests.WithLabelValues("other", "other", "other", "200").Write(snapshot); err != nil {
		t.Fatalf("read metric failed: %v", err)
	}
	if got := snapshot.GetCounter().GetValue(); got != 2 {
		t.Fatalf("expected collapsed label counter=2, got %.0f", got)
	}
}
