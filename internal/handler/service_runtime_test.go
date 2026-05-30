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

func TestServiceResponses_BadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/service/greeting/responses", bytes.NewBufferString(`{bad`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad JSON, got %d", rec.Code)
	}
}

func TestServiceChat_BadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/service/greeting/chat/completions", bytes.NewBufferString(`{bad`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad JSON, got %d", rec.Code)
	}
}

func TestServiceMessages_BadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/service/greeting/messages", bytes.NewBufferString(`{bad`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad JSON, got %d", rec.Code)
	}
}

func TestServiceInvoke_BadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/service/greeting/invoke", bytes.NewBufferString(`{bad`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad JSON, got %d", rec.Code)
	}
}

func TestServiceResponses_Unauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/service/greeting/responses", bytes.NewBufferString(`{"input":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", rec.Code)
	}
}

func TestServiceChat_Unauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/service/greeting/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", rec.Code)
	}
}

func TestServiceMessages_Unauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/service/greeting/messages", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", rec.Code)
	}
}

func TestServiceInvoke_Unauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/service/greeting/invoke", bytes.NewBufferString(`{"variables":{"name":"x"}}`))
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", rec.Code)
	}
}

func TestReady_WithProviders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ready with providers status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func setupGreetingService(t *testing.T, env *handlerTestEnv) {
	t.Helper()
	ctx := context.Background()
	createResult, err := env.catalogSvc.CreateService(ctx, repository.CreateServiceParams{
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
	if _, _, err := env.catalogSvc.PublishServiceVersion(ctx, "tenant-a", createResult.Service.ID, createResult.InitialVersion.ID, "published"); err != nil {
		t.Fatalf("PublishServiceVersion() error: %v", err)
	}
}

func TestServiceResponses_Stream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"stream svc\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"created_at\":1,\"model\":\"m\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{upstreamURL: upstream.URL, endpoint: "chat"})
	setupGreetingService(t, env)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/service/greeting/responses", bytes.NewBufferString(`{"input":"hello","stream":true}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST service responses stream status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("stream svc")) {
		t.Fatalf("expected stream delta in body, got %q", rec.Body.String())
	}
}

func TestServiceChat_Stream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{upstreamURL: upstream.URL, endpoint: "chat"})
	setupGreetingService(t, env)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/service/greeting/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}],"stream":true}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST service chat stream status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestServiceMessages_Stream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{upstreamURL: upstream.URL, endpoint: "chat"})
	setupGreetingService(t, env)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/service/greeting/messages", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hi"}],"stream":true}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST service messages stream status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestServiceInvoke_Stream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"invoke stream\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"created_at\":1,\"model\":\"m\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{upstreamURL: upstream.URL, endpoint: "chat"})
	setupGreetingService(t, env)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/service/greeting/invoke", bytes.NewBufferString(`{"variables":{"name":"x"},"stream":true}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST service invoke stream status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}
