package handler

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

var updatePublicContractGoldens = flag.Bool("update-public-contract-goldens", false, "rewrite public JSON contract golden fixtures")

func TestContractPublicJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name    string
		path    string
		body    string
		prepare func(t *testing.T, env *handlerTestEnv)
	}{
		{name: "responses", path: "/v1/responses", body: `{"model":"public-model","input":"hello"}`},
		{name: "chat", path: "/v1/chat/completions", body: `{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`},
		{name: "messages", path: "/v1/messages", body: `{"model":"public-model","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`},
		{name: "service-runtime", path: "/service/greeting/responses", body: `{"input":"hello"}`, prepare: preparePublicContractService},
		{name: "embeddings", path: "/v1/embeddings", body: `{"model":"provider-model","input":"hello"}`},
		{name: "images", path: "/v1/images/generations", body: `{"model":"gpt-image-1","prompt":"red cube","n":1,"size":"1024x1024","response_format":"url"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(publicContractUpstream())
			defer upstream.Close()
			env := newHandlerTestEnv(t, handlerTestEnvConfig{upstreamURL: upstream.URL, endpoint: "chat"})
			preparePublicContractProvider(env)
			if tc.prepare != nil {
				tc.prepare(t, env)
			}

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Authorization", "Bearer test-key:test-secret")
			req.Header.Set("Content-Type", "application/json")
			env.server.engine.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
				t.Fatalf("Content-Type = %q, want application/json; charset=utf-8", got)
			}

			got := normalizePublicContractJSON(t, rec.Body.Bytes())
			fixture := publicContractFixturePath(t, tc.name)
			if *updatePublicContractGoldens {
				if err := os.WriteFile(fixture, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden %s: %v", fixture, err)
				}
				return
			}
			wantBytes, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatalf("read golden %s: %v (run with -update-public-contract-goldens to create it)\nactual:\n%s", fixture, err, got)
			}
			want := string(wantBytes)
			if want != got {
				t.Fatalf("public JSON contract changed; diff unavailable\nwant:\n%s\ngot:\n%s", want, got)
			}
		})
	}
}

func preparePublicContractProvider(env *handlerTestEnv) {
	env.providerMgr.ApplyRegistry([]repository.ProviderRegistryRecord{{
		Name:               "test-openai",
		Enabled:            true,
		HealthStatus:       provider.ProviderHealthHealthy,
		SupportsChat:       true,
		SupportsResponses:  true,
		SupportsMessages:   true,
		SupportsStream:     true,
		SupportsTools:      true,
		SupportsImages:     true,
		SupportsEmbeddings: true,
	}})
}

func preparePublicContractService(t *testing.T, env *handlerTestEnv) {
	t.Helper()
	result, err := env.catalogSvc.CreateService(t.Context(), repository.CreateServiceParams{
		TenantID:        "tenant-a",
		Name:            "Greeting Service",
		RequestPrefix:   "greeting",
		DefaultProvider: "test-openai",
		DefaultModel:    "provider-model",
		Enabled:         true,
		Config: repository.ServiceConfig{
			Surfaces: []string{"responses"},
		},
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	if _, _, err := env.catalogSvc.PublishServiceVersion(t.Context(), "tenant-a", result.Service.ID, result.InitialVersion.ID, "published"); err != nil {
		t.Fatalf("PublishServiceVersion: %v", err)
	}
}

func publicContractUpstream() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/embeddings":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": "list",
				"data":   []map[string]any{{"object": "embedding", "index": 0, "embedding": []float64{0.1, 0.2, 0.3}}},
				"model":  "provider-model",
				"usage":  map[string]any{"prompt_tokens": 3, "total_tokens": 3},
			})
		case "/images/generations":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"created": 1700000000,
				"data":    []map[string]any{{"url": "https://example.test/image.png", "revised_prompt": "a small red cube"}},
			})
		case "/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl-contract",
				"object":  "chat.completion",
				"created": 1700000000,
				"model":   "provider-model",
				"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "contract hello"}, "finish_reason": "stop"}},
				"usage":   map[string]any{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5},
			})
		default:
			http.NotFound(w, r)
		}
	})
}

func normalizePublicContractJSON(t *testing.T, body []byte) string {
	t.Helper()
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("decode response JSON: %v\nbody: %s", err, body)
	}
	normalizePublicContractValue(value)
	var encodedBuffer bytes.Buffer
	encoder := json.NewEncoder(&encodedBuffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		t.Fatalf("encode normalized JSON: %v", err)
	}
	encoded := bytes.TrimSuffix(encodedBuffer.Bytes(), []byte("\n"))
	return string(encoded) + "\n"
}

func normalizePublicContractValue(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			switch key {
			case "id":
				if _, ok := child.(string); ok {
					v[key] = "<id>"
					continue
				}
			case "created":
				if _, ok := child.(float64); ok {
					v[key] = float64(0)
					continue
				}
			}
			normalizePublicContractValue(child)
		}
	case []any:
		for _, child := range v {
			normalizePublicContractValue(child)
		}
	}
}

func publicContractFixturePath(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "contracts", "http", "public", fmt.Sprintf("%s.json", strings.ReplaceAll(name, "-", "_")))
}
