package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gateyes/internal/config"
	"gateyes/internal/requestmeta"
)

func TestAuthEnforcesVirtualKeyWhenAuthDisabled(t *testing.T) {
	cfg := config.AuthConfig{
		Enabled:    false,
		Header:     "Authorization",
		QueryParam: "api_key",
		VirtualKeys: map[string]config.VirtualKeyConfig{
			"vk-team-a": {
				Enabled: true,
			},
		},
	}

	handler := Auth(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(requestmeta.HeaderVirtualKey); got != "vk-team-a" {
			t.Fatalf("expected virtual key header, got %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	{
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("missing token expected 401, got %d", rec.Code)
		}
	}

	{
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req.Header.Set("Authorization", "Bearer vk-team-a")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("valid virtual key expected 200, got %d", rec.Code)
		}
	}
}
