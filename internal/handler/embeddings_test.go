package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

func TestEmbeddings_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{{
				"object":    "embedding",
				"index":     0,
				"embedding": []float64{0.1, 0.2, 0.3},
			}},
			"model": "provider-model",
			"usage": map[string]any{"prompt_tokens": 3, "total_tokens": 3},
		})
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{upstreamURL: upstream.URL, endpoint: "chat"})
	env.providerMgr.ApplyRegistry([]repository.ProviderRegistryRecord{{
		Name:               "test-openai",
		Enabled:            true,
		HealthStatus:       provider.ProviderHealthHealthy,
		SupportsEmbeddings: true,
	}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewBufferString(`{"model":"provider-model","input":"hello"}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload provider.EmbeddingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Object != "list" {
		t.Fatalf("expected list object, got %q", payload.Object)
	}
}

func TestEmbeddings_SelectsProviderByRequestedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{{
				"object":    "embedding",
				"index":     0,
				"embedding": []float64{1},
			}},
			"model": "embed-first",
			"usage": map[string]any{"prompt_tokens": 1, "total_tokens": 1},
		})
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{{
				"object":    "embedding",
				"index":     0,
				"embedding": []float64{2},
			}},
			"model": "embed-target",
			"usage": map[string]any{"prompt_tokens": 1, "total_tokens": 1},
		})
	}))
	defer second.Close()

	embeddings := true
	env := newHandlerTestEnv(t, handlerTestEnvConfig{
		providerConfigs: []config.ProviderConfig{
			{Name: "first-embeddings", Type: "openai", BaseURL: first.URL, Endpoint: "chat", APIKey: "first-key", Model: "embed-first", Timeout: 5, Enabled: true, MaxTokens: 256, Capabilities: config.ProviderCapabilitiesConfig{Embeddings: &embeddings}},
			{Name: "target-embeddings", Type: "openai", BaseURL: second.URL, Endpoint: "chat", APIKey: "target-key", Model: "embed-target", Timeout: 5, Enabled: true, MaxTokens: 256, Capabilities: config.ProviderCapabilitiesConfig{Embeddings: &embeddings}},
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewBufferString(`{"model":"embed-target","input":"hello"}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload provider.EmbeddingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Model != "embed-target" {
		t.Fatalf("selected model = %q, want embed-target", payload.Model)
	}
}

func TestEmbeddings_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	env := newHandlerTestEnv(t, handlerTestEnvConfig{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewBufferString(`{invalid`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestEmbeddings_InvalidAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	env := newHandlerTestEnv(t, handlerTestEnvConfig{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewBufferString(`{"model":"embed-model","input":"hello"}`))
	req.Header.Set("Authorization", "Bearer bad-key:bad-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}
