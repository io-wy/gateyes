package responses

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/provider"
)

func TestCreateReturnsQuotaExceededAfterUpstreamSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"chatcmpl-upstream","object":"chat.completion","created":1700000000,"model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`))
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL:  upstream.URL,
		endpoint:     "chat",
		providers:    []string{"test-openai"},
	})
	env.identity.Quota = 1
	env.identity.Used = 0

	_, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model",
		Input: "hello",
	}, "")
	if !errors.Is(err, auth.ErrQuotaExceeded) {
		t.Fatalf("Service.Create(quota exceeded) error = %v, want %v", err, auth.ErrQuotaExceeded)
	}
}

func TestWrapErrorAndGinError(t *testing.T) {
	tests := []struct {
		err        error
		wantStatus int
		wantType   string
	}{
		{err: auth.ErrModelNotAllowed, wantStatus: 403, wantType: "invalid_request_error"},
		{err: auth.ErrQuotaExceeded, wantStatus: 429, wantType: "rate_limit_error"},
		{err: ErrNoProvider, wantStatus: 503, wantType: "internal_error"},
		{err: errors.New("boom"), wantStatus: 500, wantType: "internal_error"},
	}

	for _, tt := range tests {
		got := WrapError(tt.err)
		if got.Status != tt.wantStatus || got.Type != tt.wantType {
			t.Fatalf("WrapError(%v) = %+v, want status=%d type=%q", tt.err, got, tt.wantStatus, tt.wantType)
		}
		if got.Error() == "" {
			t.Fatalf("WrapError(%v).Error() = empty, want non-empty", tt.err)
		}
	}
}
