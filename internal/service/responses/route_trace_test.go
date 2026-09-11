package responses

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	pluginSvc "github.com/gateyes/gateway/internal/domain/plugin"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
	routeSvc "github.com/gateyes/gateway/internal/service/router"
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

type captureRouterPlugin struct {
	candidates []pluginSvc.CandidateInfo
	routeCtx   pluginSvc.RouteContext
}

func (p *captureRouterPlugin) Name() string { return "capture-router" }
func (p *captureRouterPlugin) Type() string { return "router" }
func (p *captureRouterPlugin) Health() pluginSvc.HealthStatus {
	return pluginSvc.HealthHealthy
}
func (p *captureRouterPlugin) Close() error { return nil }
func (p *captureRouterPlugin) OrderCandidates(_ context.Context, candidates []pluginSvc.CandidateInfo, routeCtx pluginSvc.RouteContext) ([]string, bool) {
	p.candidates = append([]pluginSvc.CandidateInfo(nil), candidates...)
	p.routeCtx = routeCtx
	return []string{"p-a"}, true
}

func TestTryPluginRouterPassesRuntimeSignals(t *testing.T) {
	metricsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`vllm:num_requests_running 4
vllm:num_requests_waiting 7
vllm:gpu_cache_usage_perc 0.62
vllm:cpu_cache_usage_perc 0.10
cache_query_total 100
cache_query_hit 73
`))
	}))
	defer metricsSrv.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		providers: []string{"p-a"},
		providerConfigs: []config.ProviderConfig{{
			Name:       "p-a",
			Type:       "openai",
			BaseURL:    "http://127.0.0.1:1",
			Endpoint:   "chat",
			APIKey:     "k",
			Model:      "m1",
			Timeout:    5,
			Enabled:    true,
			MaxTokens:  256,
			MetricsURL: metricsSrv.URL,
		}},
	})
	env.providerMgr.Stats.RecordRequest("p-a", true, 300, 250)
	env.providerMgr.Stats.RecordTTFT("p-a", 75)

	scraper := routeSvc.NewInferenceScraper(map[string]string{"p-a": metricsSrv.URL}, 10*time.Millisecond)
	scraper.Start(context.Background())
	defer scraper.Stop()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if state, ok := scraper.Get("p-a"); ok && state.NumRequestsRunning == 4 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	env.service.router.SetInferenceScraper(scraper)

	plugin := &captureRouterPlugin{}
	candidates := env.providerMgr.ListByNames([]string{"p-a"})
	ordered := env.service.tryPluginRouter(context.Background(), plugin, candidates, routeSvc.RouteContext{
		Model:     "m1",
		SessionID: "s1",
	})
	if len(ordered) != 1 || ordered[0].Name() != "p-a" {
		t.Fatalf("tryPluginRouter() = %v, want [p-a]", providerNames(ordered))
	}
	if len(plugin.candidates) != 1 {
		t.Fatalf("captured candidates len = %d, want 1", len(plugin.candidates))
	}
	got := plugin.candidates[0]
	if got.AvgLatencyMs != 250 || got.AvgTTFTMs != 75 {
		t.Fatalf("latency signals = %+v, want avg latency 250 and avg TTFT 75", got)
	}
	if got.QueueRunning != 4 || got.QueueWaiting != 7 {
		t.Fatalf("queue signals = %+v, want running 4 waiting 7", got)
	}
	if got.GPUKVCacheUsagePerc != 0.62 || got.CPUKVCacheUsagePerc != 0.10 || got.PrefixCacheHitRate != 0.73 {
		t.Fatalf("cache signals = %+v, want gpu 0.62 cpu 0.10 hit 0.73", got)
	}
	if got.SignalsUpdatedAtUnixMs == 0 {
		t.Fatalf("SignalsUpdatedAtUnixMs = 0, want scrape timestamp")
	}
}

type countingRouterPlugin struct {
	calls int
}

func (p *countingRouterPlugin) Name() string                   { return "counting-router" }
func (p *countingRouterPlugin) Type() string                   { return "router" }
func (p *countingRouterPlugin) Health() pluginSvc.HealthStatus { return pluginSvc.HealthHealthy }
func (p *countingRouterPlugin) Close() error                   { return nil }
func (p *countingRouterPlugin) OrderCandidates(context.Context, []pluginSvc.CandidateInfo, pluginSvc.RouteContext) ([]string, bool) {
	p.calls++
	return []string{"other"}, true
}

type testRouterPluginManager struct {
	router pluginSvc.Router
}

func (m testRouterPluginManager) Router() pluginSvc.Router                       { return m.router }
func (m testRouterPluginManager) GetByPhase(pluginSvc.Phase) []pluginSvc.Gateway { return nil }
func (m testRouterPluginManager) Close() error                                   { return nil }

func TestPlanCandidatesPhysicalModelBypassSkipsRouterPlugin(t *testing.T) {
	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		providers: []string{"physical", "other"},
		providerConfigs: []config.ProviderConfig{
			{Name: "physical", Type: "openai", BaseURL: "http://127.0.0.1:1", Endpoint: "chat", APIKey: "k", Model: "model-physical", Timeout: 5, Enabled: true, MaxTokens: 256},
			{Name: "other", Type: "openai", BaseURL: "http://127.0.0.1:1", Endpoint: "chat", APIKey: "k", Model: "other-model", Timeout: 5, Enabled: true, MaxTokens: 256},
		},
	})
	plugin := &countingRouterPlugin{}
	env.service.SetPluginManager(testRouterPluginManager{router: plugin})

	candidates, trace := env.service.planCandidates(context.Background(), env.identity, "s1", &provider.ResponseRequest{Model: "model-physical", Surface: "chat"})
	if got := providerNames(candidates); len(got) != 1 || got[0] != "physical" {
		t.Fatalf("planCandidates() = %v, want [physical]", got)
	}
	if plugin.calls != 0 {
		t.Fatalf("router plugin calls = %d, want 0", plugin.calls)
	}
	if !trace.Router.Bypass || trace.Router.BypassProvider != "physical" {
		t.Fatalf("router trace = %+v, want physical bypass", trace.Router)
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

func TestPlanCandidatesReportsDisabledRegistryProvider(t *testing.T) {
	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: "http://127.0.0.1:1",
		providers:   []string{"p-disabled"},
		providerConfigs: []config.ProviderConfig{
			{Name: "p-disabled", Type: "openai", BaseURL: "http://127.0.0.1:1", Endpoint: "chat", APIKey: "k", Model: "m1", Timeout: 5, Enabled: false, MaxTokens: 256},
		},
	})

	candidates, trace := env.service.planCandidates(context.Background(), env.identity, "s1", &provider.ResponseRequest{Model: "m1", Surface: "chat"})
	if candidates != nil {
		t.Fatalf("planCandidates() = %v, want nil", providerNames(candidates))
	}
	if trace == nil || len(trace.FilteredOut) != 1 || trace.FilteredOut[0].Reason != "provider_disabled" {
		t.Fatalf("planCandidates() trace.FilteredOut = %+v, want provider_disabled", trace.FilteredOut)
	}
}

func TestPlanCandidatesPrefersRequestedModelMatch(t *testing.T) {
	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: "http://127.0.0.1:1",
		providers:   []string{"p-a", "p-b"},
		providerConfigs: []config.ProviderConfig{
			{Name: "p-a", Type: "openai", BaseURL: "http://127.0.0.1:1", Endpoint: "chat", APIKey: "k", Model: "m1", Timeout: 5, Enabled: true, MaxTokens: 256},
			{Name: "p-b", Type: "openai", BaseURL: "http://127.0.0.1:1", Endpoint: "chat", APIKey: "k", Model: "m2", Timeout: 5, Enabled: true, MaxTokens: 256},
		},
	})

	candidates, trace := env.service.planCandidates(context.Background(), env.identity, "s1", &provider.ResponseRequest{Model: "m1", Surface: "chat"})
	if len(candidates) != 1 || candidates[0].Name() != "p-a" {
		t.Fatalf("planCandidates() = %v, want [p-a]", providerNames(candidates))
	}
	if len(trace.FilteredOut) != 1 || trace.FilteredOut[0].Provider != "p-b" || trace.FilteredOut[0].Reason != "model_mismatch" {
		t.Fatalf("planCandidates() trace.FilteredOut = %+v, want p-b model_mismatch", trace.FilteredOut)
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
