package responses

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/service/provider"
)

type retryMockProvider struct {
	name        string
	modelName   string
	failCount   int
	callCount   int
	failWithErr error
}

func (m *retryMockProvider) Name() string    { return m.name }
func (m *retryMockProvider) Type() string    { return "openai" }
func (m *retryMockProvider) BaseURL() string { return "" }
func (m *retryMockProvider) Model() string   { return m.modelName }
func (m *retryMockProvider) Labels() map[string]string {
	return nil
}
func (m *retryMockProvider) Weight() int           { return 1 }
func (m *retryMockProvider) UnitCost() float64     { return 0 }
func (m *retryMockProvider) Cost(_, _ int) float64 { return 0 }
func (m *retryMockProvider) CreateEmbedding(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, errors.New("not implemented")
}
func (m *retryMockProvider) CreateImageGeneration(ctx context.Context, req *provider.ImageGenerationRequest) (*provider.ImageGenerationResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *retryMockProvider) CreateResponse(ctx context.Context, req *provider.ResponseRequest) (*provider.Response, error) {
	m.callCount++
	if m.callCount <= m.failCount {
		if m.failWithErr != nil {
			return nil, m.failWithErr
		}
		return nil, errors.New("transient error")
	}
	return &provider.Response{ID: "ok", Usage: provider.Usage{TotalTokens: 5}}, nil
}

func (m *retryMockProvider) StreamResponse(ctx context.Context, req *provider.ResponseRequest) (<-chan provider.ResponseEvent, <-chan error) {
	out := make(chan provider.ResponseEvent)
	errCh := make(chan error, 1)
	close(out)
	close(errCh)
	return out, errCh
}

func TestCallWithRetrySucceedsOnFirstAttempt(t *testing.T) {
	p := &retryMockProvider{name: "p1", modelName: "m1", failCount: 0}
	svc := New(&Dependencies{
		Config: &config.Config{Retry: config.RetryConfig{MaxRetries: 2, InitialDelayMs: 10, MaxDelayMs: 100, BackoffFactor: 2}},
	})
	exec := &execution{provider: p, upstreamRequest: &provider.ResponseRequest{Model: "m1"}}

	resp, retries, err := svc.callWithRetry(context.Background(), nil, exec)
	if err != nil || retries != 0 || resp == nil {
		t.Fatalf("callWithRetry() = (%v, %d, %v), want success 0 retries", resp, retries, err)
	}
}

func TestCallWithRetryRetriesThenSucceeds(t *testing.T) {
	p := &retryMockProvider{name: "p1", modelName: "m1", failCount: 2}
	svc := New(&Dependencies{
		Config: &config.Config{Retry: config.RetryConfig{MaxRetries: 3, InitialDelayMs: 10, MaxDelayMs: 100, BackoffFactor: 2}},
	})
	exec := &execution{provider: p, upstreamRequest: &provider.ResponseRequest{Model: "m1"}}

	resp, retries, err := svc.callWithRetry(context.Background(), nil, exec)
	if err != nil || retries != 2 || resp == nil {
		t.Fatalf("callWithRetry() = (%v, %d, %v), want success after 2 retries", resp, retries, err)
	}
}

func TestCallWithRetryExhaustsRetries(t *testing.T) {
	p := &retryMockProvider{name: "p1", modelName: "m1", failCount: 10}
	svc := New(&Dependencies{
		Config: &config.Config{Retry: config.RetryConfig{MaxRetries: 1, InitialDelayMs: 10, MaxDelayMs: 50, BackoffFactor: 2}},
	})
	exec := &execution{provider: p, upstreamRequest: &provider.ResponseRequest{Model: "m1"}}

	_, retries, err := svc.callWithRetry(context.Background(), nil, exec)
	if err == nil {
		t.Fatal("callWithRetry() expected error after exhausted retries")
	}
	if retries != 1 {
		t.Fatalf("callWithRetry() retries = %d, want 1", retries)
	}
}

func TestCallWithRetryDoesNotRetryUnauthorized(t *testing.T) {
	p := &retryMockProvider{name: "p1", modelName: "m1", failCount: 5, failWithErr: ErrUnauthorized}
	svc := New(&Dependencies{
		Config: &config.Config{Retry: config.RetryConfig{MaxRetries: 3, InitialDelayMs: 10, MaxDelayMs: 100, BackoffFactor: 2}},
	})
	exec := &execution{provider: p, upstreamRequest: &provider.ResponseRequest{Model: "m1"}}

	_, retries, err := svc.callWithRetry(context.Background(), nil, exec)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("callWithRetry() err = %v, want ErrUnauthorized", err)
	}
	if retries != 0 {
		t.Fatalf("callWithRetry() retries = %d, want 0 for unauthorized", retries)
	}
}

func TestCallWithRetryRespectsContextCancellation(t *testing.T) {
	p := &retryMockProvider{name: "p1", modelName: "m1", failCount: 10}
	svc := New(&Dependencies{
		Config: &config.Config{Retry: config.RetryConfig{MaxRetries: 5, InitialDelayMs: 200, MaxDelayMs: 500, BackoffFactor: 2}},
	})
	exec := &execution{provider: p, upstreamRequest: &provider.ResponseRequest{Model: "m1"}}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, _, err := svc.callWithRetry(ctx, nil, exec)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("callWithRetry() err = %v, want context.Canceled", err)
	}
}

func TestIsRetryableReturnsTrueForNil(t *testing.T) {
	if !isRetryable(nil) {
		t.Fatal("isRetryable(nil) = false, want true")
	}
}

func TestIsRetryableReturnsFalseForUnauthorized(t *testing.T) {
	if isRetryable(ErrUnauthorized) {
		t.Fatal("isRetryable(ErrUnauthorized) = true, want false")
	}
}

func TestIsRetryableReturnsFalseForForbidden(t *testing.T) {
	if isRetryable(ErrForbidden) {
		t.Fatal("isRetryable(ErrForbidden) = true, want false")
	}
}

func TestIsRetryableReturnsFalseFor400(t *testing.T) {
	if isRetryable(errors.New("upstream returned 400")) {
		t.Fatal("isRetryable(400) = true, want false")
	}
}

func TestIsRetryableReturnsTrueFor429(t *testing.T) {
	if !isRetryable(errors.New("upstream returned 429")) {
		t.Fatal("isRetryable(429) = false, want true")
	}
}

func TestIsRetryableReturnsTrueFor500(t *testing.T) {
	if !isRetryable(errors.New("upstream returned 500")) {
		t.Fatal("isRetryable(500) = false, want true")
	}
}

func TestCreateUsesFallbackWhenFirstProviderFails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-fb","object":"chat.completion","created":1700000000,"model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"fallback"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
		providers:   []string{"fail-openai", "test-openai"},
		providerConfigs: []config.ProviderConfig{
			{
				Name:      "fail-openai",
				Type:      "openai",
				BaseURL:   "http://127.0.0.1:1",
				Endpoint:  "chat",
				APIKey:    "k",
				Model:     "m1",
				Timeout:   1,
				Enabled:   true,
				MaxTokens: 256,
			},
			{
				Name:      "test-openai",
				Type:      "openai",
				BaseURL:   upstream.URL,
				Endpoint:  "chat",
				APIKey:    "k",
				Model:     "provider-model",
				Timeout:   5,
				Enabled:   true,
				MaxTokens: 256,
			},
		},
	})

	result, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model",
		Input: "hello",
	}, "")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if result.ProviderName != "test-openai" {
		t.Fatalf("Create() provider = %q, want test-openai", result.ProviderName)
	}
	if result.Fallback != 1 {
		t.Fatalf("Create() fallback = %d, want 1", result.Fallback)
	}
}
