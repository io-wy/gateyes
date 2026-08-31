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
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
)

const contractTraceparent = "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"

var contractUpstreamURL = regexp.MustCompile(`http://127\.0\.0\.1:\d+`)

var updateErrorContractGoldens = flag.Bool("update-error-contract-goldens", false, "rewrite error contract golden fixtures")

func TestContractHeaderMiddlewareBeforeAdminAuth(t *testing.T) {
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/dashboard", nil)
	req.Header.Set("X-Request-ID", "contract-request-id")
	req.Header.Set("traceparent", contractTraceparent)
	env.server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /admin/v1/dashboard status = %d, want %d: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if got := rec.Header().Get("X-Request-ID"); got != "contract-request-id" {
		t.Errorf("X-Request-ID = %q, want contract-request-id", got)
	}
	if got := rec.Header().Get("traceparent"); got != contractTraceparent {
		t.Errorf("traceparent = %q, want %q", got, contractTraceparent)
	}
	assertErrorContractFixture(t, "admin_missing_auth.json", rec)
}

func TestContractErrorPermissionDenied(t *testing.T) {
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	rec := performJSONRequest(t, env, http.MethodGet, "/admin/v1/providers", "test-key:test-secret", "")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /admin/v1/providers status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	assertErrorContractFixture(t, "admin_permission_denied.json", rec)
}

func TestContractErrorValidation(t *testing.T) {
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	seedAdminToken(t, env, repository.RoleTenantAdmin, "contract-validation-admin", "secret")
	rec := performJSONRequest(t, env, http.MethodPost, "/admin/v1/providers", "contract-validation-admin:secret", `{}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /admin/v1/providers status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	assertErrorContractFixture(t, "admin_validation.json", rec)
}

func TestContractErrorUpstreamFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"contract upstream failure"}`))
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{upstreamURL: upstream.URL, endpoint: "chat"})
	rec := performJSONRequest(t, env, http.MethodPost, "/v1/responses", "test-key:test-secret", `{"model":"public-model","input":"hello"}`)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("POST /v1/responses status = %d, want %d: %s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
	assertErrorContractFixture(t, "upstream_failure.json", rec)
}

func TestContractHeaderCacheMetadata(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-contract","object":"chat.completion","created":1700000000,"model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"cached contract"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{upstreamURL: upstream.URL, endpoint: "chat"})
	rec := performJSONRequest(t, env, http.MethodPost, "/v1/responses", "test-key:test-secret", `{"model":"public-model","input":"hello"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/responses status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	want := map[string]string{
		"X-Gateyes-Cache-Result": "skip",
		"X-Gateyes-Cache-Layer":  "l1",
		"X-Gateyes-Cache-Reason": "disabled",
	}
	for header, value := range want {
		if got := rec.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}
}

func assertErrorContractFixture(t *testing.T, name string, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	got := decodeContractJSON(t, rec.Body.Bytes())
	path := filepath.Join("..", "..", "testdata", "contracts", "http", "error", name)
	if *updateErrorContractGoldens {
		if err := os.WriteFile(path, append([]byte(prettyErrorContractJSON(got)), '\n'), 0o644); err != nil {
			t.Fatalf("write error contract fixture %s: %v", path, err)
		}
		return
	}
	wantBody, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read error contract fixture %s: %v (run with -update-error-contract-goldens to create it)\nactual:\n%s", path, err, prettyErrorContractJSON(got))
	}
	want := decodeContractJSON(t, wantBody)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("error contract %s mismatch\ngot:\n%s\nwant:\n%s", name, prettyErrorContractJSON(got), prettyErrorContractJSON(want))
	}
}

func decodeContractJSON(t *testing.T, body []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("decode contract JSON: %v\nbody: %s", err, body)
	}
	normalizeErrorContractValue(value)
	return value
}

func normalizeErrorContractValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if text, ok := item.(string); ok {
				typed[key] = contractUpstreamURL.ReplaceAllString(text, "<upstream>")
				continue
			}
			normalizeErrorContractValue(item)
		}
	case []any:
		for _, item := range typed {
			normalizeErrorContractValue(item)
		}
	}
}

func prettyErrorContractJSON(value any) string {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return strings.TrimSuffix(body.String(), "\n")
}
