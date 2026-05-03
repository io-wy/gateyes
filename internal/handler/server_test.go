package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNewServer_RoutesRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})

	routes := env.server.engine.Routes()
	want := map[string]string{
		"GET /health":                       "GET",
		"GET /ready":                        "GET",
		"GET /metrics":                      "GET",
		"GET /v1/responses/:id":             "GET",
		"GET /v1/models":                    "GET",
		"POST /v1/responses":                "POST",
		"POST /v1/chat/completions":         "POST",
		"POST /v1/messages":                 "POST",
		"POST /v1/embeddings":               "POST",
		"GET /admin/dashboard":              "GET",
		"GET /admin/providers":              "GET",
		"POST /admin/providers":             "POST",
		"GET /admin/providers/:name":        "GET",
		"PUT /admin/providers/:name":        "PUT",
		"DELETE /admin/providers/:name":     "DELETE",
		"GET /admin/tenants":                "GET",
		"POST /admin/tenants":               "POST",
		"GET /admin/tenants/:id":            "GET",
		"PUT /admin/tenants/:id":            "PUT",
		"DELETE /admin/tenants/:id":         "DELETE",
		"POST /admin/tenants/:id/providers": "POST",
		"GET /admin/users":                  "GET",
		"POST /admin/users":                 "POST",
		"GET /admin/users/:id":              "GET",
		"PUT /admin/users/:id":              "PUT",
		"DELETE /admin/users/:id":           "DELETE",
		"GET /admin/projects":               "GET",
		"POST /admin/projects":              "POST",
		"GET /admin/projects/:id":           "GET",
		"PUT /admin/projects/:id":           "PUT",
		"DELETE /admin/projects/:id":        "DELETE",
		"GET /admin/keys":                   "GET",
		"POST /admin/keys":                  "POST",
		"GET /admin/keys/:id":               "GET",
		"PUT /admin/keys/:id":               "PUT",
		"GET /admin/services":               "GET",
		"POST /admin/services":              "POST",
		"GET /admin/services/:id":           "GET",
		"PUT /admin/services/:id":           "PUT",
	}

	got := make(map[string]string, len(routes))
	for _, r := range routes {
		got[r.Method+" "+r.Path] = r.Method
	}

	for path, method := range want {
		if got[path] != method {
			t.Fatalf("missing route %s %s", method, path)
		}
	}
}

func TestNewServer_MiddlewareStack(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})

	// Recovery middleware: panic should be recovered and return 500
	panicRoute := env.server.engine.Group("/panic-test")
	panicRoute.GET("/", func(c *gin.Context) {
		panic("intentional panic")
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic-test/", nil)
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic recovery status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	// Correlation middleware: request without auth to /health should succeed
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Auth middleware: admin route without token should return 401
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthed admin status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	// Guard middleware: LLM route with auth but bad payload should be rejected before upstream
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("expected non-OK for empty LLM request body")
	}
}
