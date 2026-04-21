package responses

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/db"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/repository/sqlstore"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/provider"
	"github.com/gateyes/gateway/internal/service/router"
)

func TestCreateMarksUsageAndResponseOnUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusBadGateway)
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})

	_, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model",
		Input: "hello",
	}, "")
	if err == nil {
		t.Fatalf("expected upstream error")
	}

	stats, err := env.store.GetUsageSummary(context.Background(), env.identity.TenantID)
	if err != nil {
		t.Fatalf("usage summary: %v", err)
	}
	if stats.FailedRequests != 1 {
		t.Fatalf("expected 1 failed usage record, got %d", stats.FailedRequests)
	}

	var count int
	var status string
	if err := env.database.Conn.QueryRowContext(context.Background(), `
SELECT COUNT(1), MAX(status)
FROM responses`).Scan(&count, &status); err != nil {
		t.Fatalf("query responses: %v", err)
	}
	if count != 1 || status != "error" {
		t.Fatalf("expected one error response record, got count=%d status=%q", count, status)
	}
}

func TestCreateStreamPersistsCompletedResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"stream hello\"}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_up\",\"created_at\":1700000000,\"model\":\"provider-model\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"stream hello\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":2,\"total_tokens\":5}}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "responses",
		providers:   []string{"test-openai"},
	})

	stream, err := env.service.CreateStream(context.Background(), env.identity, &provider.ResponseRequest{
		Model:  "public-model",
		Input:  "hello",
		Stream: true,
	}, "session-1")
	if err != nil {
		t.Fatalf("create stream: %v", err)
	}

	var eventTypes []string
	var streamErr error
	eventsCh := stream.Events
	errCh := stream.Errors
	for eventsCh != nil || errCh != nil {
		select {
		case event, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
				continue
			}
			eventTypes = append(eventTypes, event.Type)
		case err, ok := <-errCh:
			if !ok {
				errCh = nil
				continue
			}
			if err != nil {
				streamErr = err
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for stream")
		}
	}

	if streamErr != nil {
		t.Fatalf("unexpected stream error: %v", streamErr)
	}
	if len(eventTypes) < 2 || eventTypes[0] != provider.EventResponseStarted || eventTypes[len(eventTypes)-1] != provider.EventResponseCompleted {
		t.Fatalf("unexpected event sequence: %v", eventTypes)
	}

	stats, err := env.store.GetUsageSummary(context.Background(), env.identity.TenantID)
	if err != nil {
		t.Fatalf("usage summary: %v", err)
	}
	if stats.SuccessRequests != 1 {
		t.Fatalf("expected 1 successful usage record, got %d", stats.SuccessRequests)
	}

	var responseBody string
	if err := env.database.Conn.QueryRowContext(context.Background(), `
SELECT response_body
FROM responses
LIMIT 1`).Scan(&responseBody); err != nil {
		t.Fatalf("query response body: %v", err)
	}
	if !strings.Contains(responseBody, "stream hello") {
		t.Fatalf("expected persisted response body to contain stream text, got %q", responseBody)
	}
}

func TestCreateStreamEmitsPayloadFromFinalResponseWithoutDelta(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_up\",\"created_at\":1700000000,\"model\":\"provider-model\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"final hello\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":2,\"total_tokens\":5}}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "responses",
		providers:   []string{"test-openai"},
	})

	stream, err := env.service.CreateStream(context.Background(), env.identity, &provider.ResponseRequest{
		Model:  "public-model",
		Input:  "hello",
		Stream: true,
	}, "session-emit-final")
	if err != nil {
		t.Fatalf("create stream: %v", err)
	}

	var (
		eventTypes []string
		deltas     []string
		streamErr  error
	)
	eventsCh := stream.Events
	errCh := stream.Errors
	for eventsCh != nil || errCh != nil {
		select {
		case event, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
				continue
			}
			eventTypes = append(eventTypes, event.Type)
			if event.Delta != "" {
				deltas = append(deltas, event.Delta)
			}
		case err, ok := <-errCh:
			if !ok {
				errCh = nil
				continue
			}
			if err != nil {
				streamErr = err
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for stream")
		}
	}

	if streamErr != nil {
		t.Fatalf("unexpected stream error: %v", streamErr)
	}
	if len(eventTypes) < 3 || eventTypes[0] != provider.EventResponseStarted || eventTypes[1] != provider.EventContentDelta || eventTypes[len(eventTypes)-1] != provider.EventResponseCompleted {
		t.Fatalf("unexpected event sequence: %v", eventTypes)
	}
	if strings.Join(deltas, "") != "final hello" {
		t.Fatalf("unexpected deltas: %v", deltas)
	}
}

func TestCreateStreamFallsBackToNonStreamResponseWhenStreamHasNoRenderablePayload(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		if r.Header.Get("Accept") == "text/event-stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_up\",\"created_at\":1700000000,\"model\":\"provider-model\",\"status\":\"completed\",\"output\":null,\"usage\":{\"input_tokens\":3,\"output_tokens\":2,\"total_tokens\":5}}}\n\n")
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         "resp_up",
			"created_at": 1700000000,
			"model":      "provider-model",
			"status":     "completed",
			"output": []map[string]any{{
				"id":     "msg_1",
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []map[string]any{{
					"type": "output_text",
					"text": "fallback hello",
				}},
			}},
			"usage": map[string]any{"input_tokens": 3, "output_tokens": 2, "total_tokens": 5},
		})
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "responses",
		providers:   []string{"test-openai"},
	})

	stream, err := env.service.CreateStream(context.Background(), env.identity, &provider.ResponseRequest{
		Model:  "public-model",
		Input:  "hello",
		Stream: true,
	}, "session-fallback-final")
	if err != nil {
		t.Fatalf("create stream: %v", err)
	}

	var (
		eventTypes []string
		deltas     []string
		streamErr  error
	)
	eventsCh := stream.Events
	errCh := stream.Errors
	for eventsCh != nil || errCh != nil {
		select {
		case event, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
				continue
			}
			eventTypes = append(eventTypes, event.Type)
			if event.Delta != "" {
				deltas = append(deltas, event.Delta)
			}
		case err, ok := <-errCh:
			if !ok {
				errCh = nil
				continue
			}
			if err != nil {
				streamErr = err
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for stream")
		}
	}

	if streamErr != nil {
		t.Fatalf("unexpected stream error: %v", streamErr)
	}
	if len(eventTypes) < 3 || eventTypes[0] != provider.EventResponseStarted || eventTypes[1] != provider.EventContentDelta || eventTypes[len(eventTypes)-1] != provider.EventResponseCompleted {
		t.Fatalf("unexpected event sequence: %v", eventTypes)
	}
	if strings.Join(deltas, "") != "fallback hello" {
		t.Fatalf("unexpected deltas: %v", deltas)
	}
}

func TestCreateStreamReturnsOutputBudgetTooLowWhenOnlyThinkingIsProduced(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_up\",\"created_at\":1700000000,\"model\":\"provider-model\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"thinking\",\"thinking\":\"internal reasoning\",\"signature\":\"sig-1\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":60,\"total_tokens\":63}}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "responses",
		providers:   []string{"test-openai"},
	})

	stream, err := env.service.CreateStream(context.Background(), env.identity, &provider.ResponseRequest{
		Model:           "public-model",
		Input:           "hello",
		Stream:          true,
		MaxOutputTokens: 64,
	}, "session-thinking-only")
	if err != nil {
		t.Fatalf("create stream: %v", err)
	}

	var (
		eventTypes []string
		streamErr  error
	)
	eventsCh := stream.Events
	errCh := stream.Errors
	for eventsCh != nil || errCh != nil {
		select {
		case event, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
				continue
			}
			eventTypes = append(eventTypes, event.Type)
		case err, ok := <-errCh:
			if !ok {
				errCh = nil
				continue
			}
			if err != nil {
				streamErr = err
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for stream")
		}
	}

	if !errors.Is(streamErr, ErrOutputBudgetTooLow) {
		t.Fatalf("unexpected stream error: %v, want %v", streamErr, ErrOutputBudgetTooLow)
	}
	if len(eventTypes) != 1 || eventTypes[0] != provider.EventResponseStarted {
		t.Fatalf("unexpected event sequence before budget error: %v", eventTypes)
	}
}

func TestCreateStreamReturnsOutputBudgetTooLowWhenLowBudgetProducesNoVisibleOutput(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_up\",\"created_at\":1700000000,\"model\":\"provider-model\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":0,\"total_tokens\":3}}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "responses",
		providers:   []string{"test-openai"},
	})

	stream, err := env.service.CreateStream(context.Background(), env.identity, &provider.ResponseRequest{
		Model:           "public-model",
		Input:           "hello",
		Stream:          true,
		MaxOutputTokens: 64,
	}, "session-no-visible-output")
	if err != nil {
		t.Fatalf("create stream: %v", err)
	}

	var streamErr error
	eventsCh := stream.Events
	errCh := stream.Errors
	for eventsCh != nil || errCh != nil {
		select {
		case _, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
			}
		case err, ok := <-errCh:
			if !ok {
				errCh = nil
				continue
			}
			if err != nil {
				streamErr = err
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for stream")
		}
	}

	if !errors.Is(streamErr, ErrOutputBudgetTooLow) {
		t.Fatalf("unexpected stream error: %v, want %v", streamErr, ErrOutputBudgetTooLow)
	}
}

func TestCreateStreamRecoversChatPayloadFromFinishOnlyStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		if r.Header.Get("Accept") == "text/event-stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"id\":\"chat-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"provider-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
			_, _ = fmt.Fprint(w, "data: {\"id\":\"chat-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"provider-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":5,\"total_tokens\":8}}\n\n")
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chat-1",
			"object":  "chat.completion",
			"created": 1700000000,
			"model":   "provider-model",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "recovered hello",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 5, "total_tokens": 8},
		})
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})

	stream, err := env.service.CreateStream(context.Background(), env.identity, &provider.ResponseRequest{
		Model:  "public-model",
		Input:  "hello",
		Stream: true,
	}, "session-chat-recover")
	if err != nil {
		t.Fatalf("create stream: %v", err)
	}

	var (
		eventTypes []string
		deltas     []string
		streamErr  error
	)
	eventsCh := stream.Events
	errCh := stream.Errors
	for eventsCh != nil || errCh != nil {
		select {
		case event, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
				continue
			}
			eventTypes = append(eventTypes, event.Type)
			if event.Delta != "" {
				deltas = append(deltas, event.Delta)
			}
		case err, ok := <-errCh:
			if !ok {
				errCh = nil
				continue
			}
			if err != nil {
				streamErr = err
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for stream")
		}
	}

	if streamErr != nil {
		t.Fatalf("unexpected stream error: %v", streamErr)
	}
	if len(eventTypes) < 3 || eventTypes[0] != provider.EventResponseStarted || eventTypes[1] != provider.EventContentDelta || eventTypes[len(eventTypes)-1] != provider.EventResponseCompleted {
		t.Fatalf("unexpected event sequence: %v", eventTypes)
	}
	if strings.Join(deltas, "") != "recovered hello" {
		t.Fatalf("unexpected deltas: %v", deltas)
	}
}

func TestCreateStreamMarksCancelledAndRecordsPartialUsageOnClientDisconnect(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected streaming response writer")
		}
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial hello\"}\n\n")
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "responses",
		providers:   []string{"test-openai"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := env.service.CreateStream(ctx, env.identity, &provider.ResponseRequest{
		Model:  "public-model",
		Input:  "hello",
		Stream: true,
	}, "session-client-cancel")
	if err != nil {
		t.Fatalf("create stream: %v", err)
	}

	var (
		streamErr  error
		sawStarted bool
		sawDelta   bool
	)
	eventsCh := stream.Events
	errCh := stream.Errors
	cancelled := false

	for eventsCh != nil || errCh != nil {
		select {
		case event, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
				continue
			}
			if event.Type == provider.EventResponseStarted {
				sawStarted = true
			}
			if event.Type == provider.EventContentDelta && event.Text() == "partial hello" {
				sawDelta = true
				if !cancelled {
					cancel()
					cancelled = true
				}
			}
		case err, ok := <-errCh:
			if !ok {
				errCh = nil
				continue
			}
			if err != nil {
				streamErr = err
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for cancelled stream")
		}
	}

	if !sawStarted || !sawDelta {
		t.Fatalf("expected started and partial delta before cancellation, got started=%v delta=%v", sawStarted, sawDelta)
	}
	if !errors.Is(streamErr, context.Canceled) {
		t.Fatalf("unexpected stream error: %v, want %v", streamErr, context.Canceled)
	}

	record := waitForResponseRecord(t, env.database.Conn, 3*time.Second)
	if record.Status != "cancelled" {
		t.Fatalf("response status = %q, want cancelled", record.Status)
	}
	if !strings.Contains(record.ResponseBody, "partial hello") {
		t.Fatalf("response body = %q, want persisted partial output", record.ResponseBody)
	}

	usage := waitForUsageRecord(t, env.database.Conn, 3*time.Second)
	if usage.Status != "cancelled" || usage.ErrorType != "client_disconnect" {
		t.Fatalf("usage record = %+v, want cancelled/client_disconnect", usage)
	}
	if usage.TotalTokens <= 0 || usage.CompletionTokens <= 0 {
		t.Fatalf("usage record = %+v, want positive partial token counts", usage)
	}

	refreshed, err := env.store.Authenticate(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("refresh identity: %v", err)
	}
	if refreshed.Used != usage.TotalTokens {
		t.Fatalf("user used = %d, want %d", refreshed.Used, usage.TotalTokens)
	}
}

type responsesTestEnv struct {
	database    *db.DB
	store       *sqlstore.Store
	service     *Service
	identity    *repository.AuthIdentity
	providerMgr *provider.Manager
}

type responsesTestEnvConfig struct {
	upstreamURL     string
	endpoint        string
	providers       []string
	providerConfigs []config.ProviderConfig
	routerConfig    config.RouterConfig
}

func newResponsesTestEnv(t *testing.T, cfg responsesTestEnvConfig) *responsesTestEnv {
	t.Helper()

	ctx := context.Background()
	database, err := db.Open(config.DatabaseConfig{
		Driver:                 "sqlite",
		DSN:                    filepath.Join(t.TempDir(), "responses.db"),
		AutoMigrate:            true,
		MaxOpenConns:           1,
		MaxIdleConns:           1,
		ConnMaxLifetimeSeconds: 60,
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	store := sqlstore.New(database)
	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     "tenant-a",
		Slug:   "tenant-a",
		Name:   "tenant-a",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	if err := store.ReplaceTenantProviders(ctx, tenant.ID, cfg.providers); err != nil {
		t.Fatalf("replace tenant providers: %v", err)
	}
	if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID:   tenant.ID,
		Key:        "test-key",
		SecretHash: repository.HashSecret("test-secret"),
		Name:       "test-user",
		Email:      "test@example.com",
		Role:       repository.RoleTenantUser,
		Quota:      1000,
		QPS:        100,
		Models:     nil,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	authSvc := auth.NewAuth(store)
	identity, err := authSvc.Authenticate(ctx, "test-key", "test-secret")
	if err != nil {
		t.Fatalf("authenticate identity: %v", err)
	}

	providerCfgs := cfg.providerConfigs
	if len(providerCfgs) == 0 {
		providerCfgs = []config.ProviderConfig{{
			Name:      "test-openai",
			Type:      "openai",
			BaseURL:   cfg.upstreamURL,
			Endpoint:  cfg.endpoint,
			APIKey:    "upstream-key",
			Model:     "provider-model",
			Timeout:   5,
			Enabled:   true,
			MaxTokens: 256,
		}}
	}

	providerMgr, err := provider.NewManager(providerCfgs)
	if err != nil {
		t.Fatalf("new provider manager: %v", err)
	}

	routerCfg := cfg.routerConfig
	if routerCfg.Strategy == "" {
		routerCfg.Strategy = "round_robin"
	}
	routerSvc := router.NewRouter(routerCfg)
	routerSvc.SetProviders(providerMgr.List())

	service := New(&Dependencies{
		Config:      &config.Config{},
		Store:       store,
		Auth:        authSvc,
		ProviderMgr: providerMgr,
		Router:      routerSvc,
		Alert:       nil,
	})

	return &responsesTestEnv{
		database:    database,
		store:       store,
		service:     service,
		identity:    identity,
		providerMgr: providerMgr,
	}
}

func queryResponseStatus(ctx context.Context, conn *sql.DB) (int, string, error) {
	var count int
	var status string
	err := conn.QueryRowContext(ctx, `
SELECT COUNT(1), MAX(status)
FROM responses`).Scan(&count, &status)
	return count, status, err
}

type responseSnapshot struct {
	Status       string
	ResponseBody string
}

func waitForResponseRecord(t *testing.T, conn *sql.DB, timeout time.Duration) responseSnapshot {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		var record responseSnapshot
		err := conn.QueryRowContext(context.Background(), `
SELECT status, response_body
FROM responses
LIMIT 1`).Scan(&record.Status, &record.ResponseBody)
		if err == nil && record.Status != "" && record.Status != "in_progress" {
			return record
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("query response record: %v", err)
			}
			t.Fatalf("timed out waiting for terminal response record, last status=%q", record.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

type usageSnapshot struct {
	Status           string
	ErrorType        string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

func waitForUsageRecord(t *testing.T, conn *sql.DB, timeout time.Duration) usageSnapshot {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		var record usageSnapshot
		err := conn.QueryRowContext(context.Background(), `
SELECT status, error_type, prompt_tokens, completion_tokens, total_tokens
FROM usage_records
LIMIT 1`).Scan(
			&record.Status,
			&record.ErrorType,
			&record.PromptTokens,
			&record.CompletionTokens,
			&record.TotalTokens,
		)
		if err == nil {
			return record
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for usage record: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBuildUpstreamRequestPreservesOutputFormatAndOptions(t *testing.T) {
	req := &provider.ResponseRequest{
		Model: "public-model",
		Messages: []provider.Message{{
			Role:    "user",
			Content: provider.TextBlocks("hello"),
		}},
		OutputFormat: &provider.OutputFormat{
			Type:   "json_schema",
			Name:   "Answer",
			Strict: true,
			Schema: map[string]any{"type": "object"},
			Raw: map[string]any{
				"type": "json_schema",
				"json_schema": map[string]any{
					"name":   "Answer",
					"strict": true,
					"schema": map[string]any{"type": "object"},
				},
			},
		},
		Options: &provider.RequestOptions{
			System: "be concise",
			Thinking: &provider.AnthropicThinking{
				Type:         "enabled",
				BudgetTokens: 64,
			},
			CacheControl: &provider.AnthropicCacheControl{
				Type: "ephemeral",
				TTL:  "5m",
			},
			Raw: map[string]any{
				"vendor_hint": "anthropic-compatible",
			},
		},
	}

	upstream := buildUpstreamRequest(req)
	if upstream.OutputFormat == nil || upstream.OutputFormat.Type != "json_schema" || upstream.OutputFormat.Name != "Answer" || !upstream.OutputFormat.Strict {
		t.Fatalf("buildUpstreamRequest() output format = %+v, want preserved output format", upstream.OutputFormat)
	}
	if upstream.Options == nil || upstream.Options.System != "be concise" {
		t.Fatalf("buildUpstreamRequest() options = %+v, want preserved system option", upstream.Options)
	}
	if upstream.Options.Thinking == nil || upstream.Options.Thinking.Type != "enabled" || upstream.Options.Thinking.BudgetTokens != 64 {
		t.Fatalf("buildUpstreamRequest() thinking option = %+v, want preserved thinking config", upstream.Options.Thinking)
	}
	if upstream.Options.CacheControl == nil || upstream.Options.CacheControl.Type != "ephemeral" || upstream.Options.CacheControl.TTL != "5m" {
		t.Fatalf("buildUpstreamRequest() cache_control option = %+v, want preserved cache control", upstream.Options.CacheControl)
	}
	if upstream.Options.Raw["vendor_hint"] != "anthropic-compatible" {
		t.Fatalf("buildUpstreamRequest() raw options = %+v, want preserved raw fallback", upstream.Options.Raw)
	}

	req.OutputFormat.Name = "Mutated"
	req.Options.System = "changed"
	req.Options.Raw["vendor_hint"] = "mutated"
	if upstream.OutputFormat.Name != "Answer" || upstream.Options.System != "be concise" || upstream.Options.Raw["vendor_hint"] != "anthropic-compatible" {
		t.Fatalf("buildUpstreamRequest() should clone output format and options, got format=%+v options=%+v", upstream.OutputFormat, upstream.Options)
	}
}
