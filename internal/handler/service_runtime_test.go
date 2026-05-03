package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
)

func setupServiceRuntimeEnv(t *testing.T) (*handlerTestEnv, func()) {
	t.Helper()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-upstream",
			"object":  "chat.completion",
			"created": 1700000000,
			"model":   "provider-model",
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": "service hello"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 1, "total_tokens": 3},
		})
	}))

	env := newHandlerTestEnv(t, handlerTestEnvConfig{upstreamURL: upstream.URL, endpoint: "chat"})

	createResult, err := env.catalogSvc.CreateService(context.Background(), repository.CreateServiceParams{
		TenantID:        "tenant-a",
		Name:            "Greeting Service",
		RequestPrefix:   "greeting",
		DefaultProvider: "test-openai",
		DefaultModel:    "provider-model",
		Enabled:         true,
		Config: repository.ServiceConfig{
			Surfaces: []string{"invoke", "chat", "responses", "messages"},
			PromptTemplate: &repository.PromptTemplateConfig{
				UserTemplate: "Say hello to {{name}}",
				Variables: []repository.PromptTemplateVariable{{
					Name:     "name",
					Required: true,
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateService() error: %v", err)
	}
	if _, _, err := env.catalogSvc.PublishServiceVersion(context.Background(), "tenant-a", createResult.Service.ID, createResult.InitialVersion.ID, "published"); err != nil {
		t.Fatalf("PublishServiceVersion() error: %v", err)
	}
	return env, upstream.Close
}

func TestServiceResponses_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env, cleanup := setupServiceRuntimeEnv(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/service/greeting/responses", bytes.NewBufferString(`{"model":"provider-model","input":"hello"}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestServiceChat_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env, cleanup := setupServiceRuntimeEnv(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/service/greeting/chat/completions", bytes.NewBufferString(`{"model":"provider-model","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestServiceMessages_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env, cleanup := setupServiceRuntimeEnv(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/service/greeting/messages", bytes.NewBufferString(`{"model":"provider-model","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestServiceInvoke_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env, cleanup := setupServiceRuntimeEnv(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/service/greeting/invoke", bytes.NewBufferString(`{"variables":{"name":"Gateyes"}}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
