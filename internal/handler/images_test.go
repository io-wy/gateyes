package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

func TestImageGenerations_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var upstreamBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/generations" {
			t.Fatalf("upstream path = %q, want /images/generations", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1700000000,
			"data": []map[string]any{{
				"url":            "https://example.test/image.png",
				"revised_prompt": "a small red cube",
			}},
		})
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{upstreamURL: upstream.URL, endpoint: "chat"})
	env.providerMgr.ApplyRegistry([]repository.ProviderRegistryRecord{{
		Name:           "test-openai",
		Enabled:        true,
		HealthStatus:   provider.ProviderHealthHealthy,
		SupportsImages: true,
	}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(`{"model":"gpt-image-1","prompt":"red cube","n":1,"size":"1024x1024","response_format":"url"}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if upstreamBody["prompt"] != "red cube" || upstreamBody["model"] != "gpt-image-1" {
		t.Fatalf("unexpected upstream body: %#v", upstreamBody)
	}
	var payload provider.ImageGenerationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Created != 1700000000 || len(payload.Data) != 1 || payload.Data[0].URL == "" {
		t.Fatalf("unexpected payload: %s", rec.Body.String())
	}
}

func TestImageGenerations_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	env := newHandlerTestEnv(t, handlerTestEnvConfig{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(`{invalid`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestImageGenerations_InvalidAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	env := newHandlerTestEnv(t, handlerTestEnvConfig{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(`{"model":"gpt-image-1","prompt":"red cube"}`))
	req.Header.Set("Authorization", "Bearer bad-key:bad-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestImageGenerations_NoImageProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)

	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	env.providerMgr.ApplyRegistry([]repository.ProviderRegistryRecord{{
		Name:           "test-openai",
		Enabled:        true,
		HealthStatus:   provider.ProviderHealthHealthy,
		SupportsImages: false,
	}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(`{"model":"gpt-image-1","prompt":"red cube"}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
