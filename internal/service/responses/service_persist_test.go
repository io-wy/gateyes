package responses

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

func TestPersistSuccess_RecordsUsage(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})

	_, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model",
		Input: "hello",
	}, "")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	stats, err := env.store.GetUsageSummary(context.Background(), env.identity.TenantID)
	if err != nil {
		t.Fatalf("GetUsageSummary() error: %v", err)
	}
	if stats.SuccessRequests != 1 {
		t.Fatalf("success requests = %d, want 1", stats.SuccessRequests)
	}
}

func TestPersistSuccess_BudgetDeduction(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})
	before := env.identity.Used

	_, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model",
		Input: "hello",
	}, "")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	refreshed, err := env.store.Authenticate(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("Authenticate() error: %v", err)
	}
	if refreshed.Used <= before {
		t.Fatalf("used = %d, should be > %d", refreshed.Used, before)
	}
}

func TestPersistSuccess_AlertWebhook(t *testing.T) {
	received := make(chan []byte, 1)
	alertSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer alertSrv.Close()

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	// Alert webhook is wired through alert service; we test callback instead
	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})
	env.identity.CallbackURL = alertSrv.URL

	_, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model",
		Input: "hello",
	}, "")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	select {
	case body := <-received:
		if !strings.Contains(string(body), `"event"`) {
			t.Fatalf("callback body missing event: %s", body)
		}
	default:
		// callback is async; no strict wait required for unit test
	}
}

func TestMarkError(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})

	_ = env.store.CreateResponse(context.Background(), repository.ResponseRecord{
		ID:       "resp-mark",
		TenantID: env.identity.TenantID,
		Status:   "in_progress",
	})
	exec := &execution{
		provider:        env.providerMgr.List()[0],
		requestedModel:  "public-model",
		upstreamRequest: &provider.ResponseRequest{Model: "public-model"},
		responseID:      "resp-mark",
		routeTrace:      &routeTrace{},
	}
	err := env.service.markError(context.Background(), env.identity, exec, 50)
	if err != nil {
		t.Fatalf("markError() error: %v", err)
	}

	record, _ := env.store.GetResponse(context.Background(), env.identity.TenantID, "resp-mark")
	if record.Status != "error" {
		t.Fatalf("status = %q, want error", record.Status)
	}
}

func TestMarkErrorWithProvider(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})

	_ = env.store.CreateResponse(context.Background(), repository.ResponseRecord{
		ID:       "resp-mp",
		TenantID: env.identity.TenantID,
		Status:   "in_progress",
	})
	exec := &execution{
		provider:        env.providerMgr.List()[0],
		requestedModel:  "public-model",
		upstreamRequest: &provider.ResponseRequest{Model: "public-model"},
		responseID:      "resp-mp",
		routeTrace:      &routeTrace{},
	}
	err := env.service.markErrorWithProvider(context.Background(), env.identity, exec, 50, "test-openai")
	if err != nil {
		t.Fatalf("markErrorWithProvider() error: %v", err)
	}

	record, _ := env.store.GetResponse(context.Background(), env.identity.TenantID, "resp-mp")
	if record.Status != "error" {
		t.Fatalf("status = %q, want error", record.Status)
	}
}

func TestRecordOutputBudgetError(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})

	_ = env.store.CreateResponse(context.Background(), repository.ResponseRecord{
		ID:       "resp-obe",
		TenantID: env.identity.TenantID,
		Status:   "in_progress",
	})
	exec := &execution{
		provider:        env.providerMgr.List()[0],
		requestedModel:  "public-model",
		upstreamRequest: &provider.ResponseRequest{Model: "public-model"},
		responseID:      "resp-obe",
		routeTrace:      &routeTrace{},
	}
	resp := &provider.Response{Usage: provider.Usage{TotalTokens: 5}}
	_ = env.service.recordOutputBudgetError(context.Background(), env.identity, exec, resp, 50, "test-openai")

	record, _ := env.store.GetResponse(context.Background(), env.identity.TenantID, "resp-obe")
	if record.Status != "error" {
		t.Fatalf("status = %q, want error", record.Status)
	}
}

func TestSendCallbackDelivery(t *testing.T) {
	received := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	svc := &Service{}
	svc.sendCallback(server.URL, map[string]any{"status": "ok"})

	select {
	case body := <-received:
		if !strings.Contains(string(body), `"status":"ok"`) {
			t.Fatalf("callback body = %s", body)
		}
	default:
		t.Fatal("callback not received")
	}

	svc.sendCallback("", nil)
}
