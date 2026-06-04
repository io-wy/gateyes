package responses

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

func TestRegistryFilterReasonDisabled(t *testing.T) {
	record := repository.ProviderRegistryRecord{Enabled: false}
	reason, _ := registryFilterReason(record, nil)
	if reason != "provider_disabled" {
		t.Fatalf("registryFilterReason() = %q, want provider_disabled", reason)
	}
}

func TestRegistryFilterReasonDrain(t *testing.T) {
	record := repository.ProviderRegistryRecord{Enabled: true, Drain: true}
	reason, _ := registryFilterReason(record, nil)
	if reason != "provider_drain" {
		t.Fatalf("registryFilterReason() = %q, want provider_drain", reason)
	}
}

func TestRegistryFilterReasonUnhealthy(t *testing.T) {
	record := repository.ProviderRegistryRecord{Enabled: true, Drain: false, HealthStatus: provider.ProviderHealthUnhealthy}
	reason, detail := registryFilterReason(record, nil)
	if reason != "provider_unhealthy" || detail != provider.ProviderHealthUnhealthy {
		t.Fatalf("registryFilterReason() = (%q, %q), want (provider_unhealthy, %s)", reason, detail, provider.ProviderHealthUnhealthy)
	}
}

func TestRegistryFilterReasonSurfaceCapability(t *testing.T) {
	record := repository.ProviderRegistryRecord{Enabled: true, Drain: false, HealthStatus: provider.ProviderHealthHealthy, SupportsChat: false}
	req := &provider.ResponseRequest{Surface: "chat"}
	reason, detail := registryFilterReason(record, req)
	if reason != "capability_surface" || detail != "chat" {
		t.Fatalf("registryFilterReason() = (%q, %q), want (capability_surface, chat)", reason, detail)
	}
}

func TestRegistryFilterReasonStreamCapability(t *testing.T) {
	record := repository.ProviderRegistryRecord{Enabled: true, Drain: false, HealthStatus: provider.ProviderHealthHealthy, SupportsChat: true, SupportsStream: false}
	req := &provider.ResponseRequest{Surface: "chat", Stream: true}
	reason, _ := registryFilterReason(record, req)
	if reason != "capability_stream" {
		t.Fatalf("registryFilterReason() = %q, want capability_stream", reason)
	}
}

func TestRegistryFilterReasonToolsCapability(t *testing.T) {
	record := repository.ProviderRegistryRecord{Enabled: true, Drain: false, HealthStatus: provider.ProviderHealthHealthy, SupportsChat: true, SupportsTools: false}
	req := &provider.ResponseRequest{Surface: "chat", Tools: []any{map[string]any{"type": "function"}}}
	reason, _ := registryFilterReason(record, req)
	if reason != "capability_tools" {
		t.Fatalf("registryFilterReason() = %q, want capability_tools", reason)
	}
}

func TestRegistryFilterReasonImagesCapability(t *testing.T) {
	record := repository.ProviderRegistryRecord{Enabled: true, Drain: false, HealthStatus: provider.ProviderHealthHealthy, SupportsChat: true, SupportsImages: false}
	req := &provider.ResponseRequest{
		Surface: "chat",
		Messages: []provider.Message{{
			Role: "user",
			Content: []provider.ContentBlock{
				{Type: "image", Image: &provider.ContentImage{URL: "https://example.com/a.png"}},
			},
		}},
	}
	reason, _ := registryFilterReason(record, req)
	if reason != "capability_images" {
		t.Fatalf("registryFilterReason() = %q, want capability_images", reason)
	}
}

func TestRegistryFilterReasonStructuredOutputCapability(t *testing.T) {
	record := repository.ProviderRegistryRecord{Enabled: true, Drain: false, HealthStatus: provider.ProviderHealthHealthy, SupportsChat: true, SupportsStructuredOutput: false}
	req := &provider.ResponseRequest{Surface: "chat", OutputFormat: &provider.OutputFormat{Type: "json_schema"}}
	reason, _ := registryFilterReason(record, req)
	if reason != "capability_structured_output" {
		t.Fatalf("registryFilterReason() = %q, want capability_structured_output", reason)
	}
}

func TestRegistryFilterReasonEmptyForHealthyMatch(t *testing.T) {
	record := repository.ProviderRegistryRecord{
		Enabled: true, Drain: false, HealthStatus: provider.ProviderHealthHealthy,
		SupportsChat: true, SupportsStream: true, SupportsTools: true,
		SupportsImages: true, SupportsStructuredOutput: true,
	}
	req := &provider.ResponseRequest{Surface: "chat"}
	reason, _ := registryFilterReason(record, req)
	if reason != "" {
		t.Fatalf("registryFilterReason() = %q, want empty", reason)
	}
}

func TestProviderNamesFromSlice(t *testing.T) {
	items := []provider.Provider{
		&retryMockProvider{name: "a", modelName: "m1"},
		&retryMockProvider{name: "b", modelName: "m2"},
	}
	names := providerNamesFromSlice(items)
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("providerNamesFromSlice() = %v, want [a b]", names)
	}
}

func TestFinalizeRouteTraceSetsFields(t *testing.T) {
	trace := &routeTrace{Status: "planned"}
	finalizeRouteTrace(trace, "p1", "success", nil)
	if trace.FinalProvider != "p1" || trace.Status != "success" || trace.Error != "" {
		t.Fatalf("finalizeRouteTrace() = %+v, want FinalProvider=p1 Status=success", trace)
	}
}

func TestFinalizeRouteTraceSetsError(t *testing.T) {
	trace := &routeTrace{}
	finalizeRouteTrace(trace, "p1", "error", errors.New("boom"))
	if trace.Error != "boom" {
		t.Fatalf("finalizeRouteTrace() error = %q, want boom", trace.Error)
	}
}

func TestFinalizeRouteTraceNoOpForNil(t *testing.T) {
	finalizeRouteTrace(nil, "p1", "success", nil)
}

func TestRouteTraceBytesReturnsNilForNil(t *testing.T) {
	if routeTraceBytes(nil) != nil {
		t.Fatal("routeTraceBytes(nil) != nil")
	}
}

func TestRouteTraceBytesReturnsJSON(t *testing.T) {
	trace := &routeTrace{Status: "planned", FinalProvider: "p1"}
	b := routeTraceBytes(trace)
	if len(b) == 0 {
		t.Fatal("routeTraceBytes() returned empty")
	}
	if string(b) == "" {
		t.Fatal("routeTraceBytes() returned empty string")
	}
}

func TestAppendRouteAttemptAddsEntry(t *testing.T) {
	trace := &routeTrace{}
	appendRouteAttempt(trace, "p1", 2, "success", nil)
	if len(trace.Attempts) != 1 {
		t.Fatalf("len(Attempts) = %d, want 1", len(trace.Attempts))
	}
	if trace.Attempts[0].Provider != "p1" || trace.Attempts[0].Retries != 2 {
		t.Fatalf("Attempts[0] = %+v, want Provider=p1 Retries=2", trace.Attempts[0])
	}
}

func TestAppendRouteAttemptSetsError(t *testing.T) {
	trace := &routeTrace{}
	appendRouteAttempt(trace, "p1", 0, "error", errors.New("fail"))
	if trace.Attempts[0].Error != "fail" {
		t.Fatalf("Attempts[0].Error = %q, want fail", trace.Attempts[0].Error)
	}
}

func TestAppendRouteAttemptNoOpForNil(t *testing.T) {
	appendRouteAttempt(nil, "p1", 0, "success", nil)
}

func TestPlanCandidatesReturnsErrorWhenStoreFails(t *testing.T) {
	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: "http://127.0.0.1:1",
		providers:   []string{"test-openai"},
	})
	ctx := context.Background()
	_, trace := env.service.planCandidates(ctx, env.identity, "s1", &provider.ResponseRequest{Model: "m1"})
	if trace == nil || trace.Status != "planned" {
		t.Fatalf("planCandidates() trace = %+v", trace)
	}
}

func TestPlanCandidatesFiltersByPreferredProvider(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"p-a", "p-b"},
		providerConfigs: []config.ProviderConfig{
			{Name: "p-a", Type: "openai", BaseURL: up.URL, Endpoint: "chat", APIKey: "k", Model: "m1", Timeout: 5, Enabled: true, MaxTokens: 256},
			{Name: "p-b", Type: "openai", BaseURL: up.URL, Endpoint: "chat", APIKey: "k", Model: "m2", Timeout: 5, Enabled: true, MaxTokens: 256},
		},
	})

	candidates, trace := env.service.planCandidates(context.Background(), env.identity, "s1", &provider.ResponseRequest{
		Model:             "public-model",
		PreferredProvider: "p-b",
	})
	if len(candidates) != 1 || candidates[0].Name() != "p-b" {
		t.Fatalf("planCandidates() = %v, want [p-b]", providerNames(candidates))
	}
	if len(trace.FilteredOut) != 1 || trace.FilteredOut[0].Reason != "preferred_provider" {
		t.Fatalf("planCandidates() trace.FilteredOut = %+v", trace.FilteredOut)
	}
}

func TestPlanCandidatesReturnsNilWhenNoCandidates(t *testing.T) {
	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: "http://127.0.0.1:1",
		providers:   []string{},
	})
	candidates, trace := env.service.planCandidates(context.Background(), env.identity, "s1", &provider.ResponseRequest{Model: "m1"})
	if candidates != nil {
		t.Fatalf("planCandidates() = %v, want nil", providerNames(candidates))
	}
	if trace == nil || trace.Status != "no_provider" {
		t.Fatalf("planCandidates() trace.Status = %q, want no_provider", trace.Status)
	}
}

func TestPlanCandidatesReturnsRoutableWhenNoRouter(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"p-a"},
		providerConfigs: []config.ProviderConfig{
			{Name: "p-a", Type: "openai", BaseURL: up.URL, Endpoint: "chat", APIKey: "k", Model: "m1", Timeout: 5, Enabled: true, MaxTokens: 256},
		},
	})
	env.service.router = nil

	candidates, trace := env.service.planCandidates(context.Background(), env.identity, "s1", &provider.ResponseRequest{Model: "m1"})
	if len(candidates) != 1 || candidates[0].Name() != "p-a" {
		t.Fatalf("planCandidates() = %v, want [p-a]", providerNames(candidates))
	}
	if len(trace.OrderedCandidates) != 1 {
		t.Fatalf("planCandidates() OrderedCandidates = %v", trace.OrderedCandidates)
	}
}
