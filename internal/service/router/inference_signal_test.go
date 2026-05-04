package router

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sampleVLLMMetrics = `# HELP vllm:num_requests_running Number of requests currently running on GPU
# TYPE vllm:num_requests_running gauge
vllm:num_requests_running{model_name="llama"} 4
# HELP vllm:num_requests_waiting Number of requests waiting in the queue
# TYPE vllm:num_requests_waiting gauge
vllm:num_requests_waiting 7
# HELP vllm:gpu_cache_usage_perc GPU KV-cache usage. 1 means 100 percent usage
# TYPE vllm:gpu_cache_usage_perc gauge
vllm:gpu_cache_usage_perc 0.62
unrelated_metric 9999
`

func TestParseVLLMMetricsExtractsGauges(t *testing.T) {
	state, err := parseVLLMMetrics(strings.NewReader(sampleVLLMMetrics))
	if err != nil {
		t.Fatalf("parseVLLMMetrics() error: %v", err)
	}
	if state.NumRequestsRunning != 4 {
		t.Fatalf("NumRequestsRunning = %v, want 4", state.NumRequestsRunning)
	}
	if state.NumRequestsWaiting != 7 {
		t.Fatalf("NumRequestsWaiting = %v, want 7", state.NumRequestsWaiting)
	}
	if state.GPUCacheUsagePerc != 0.62 {
		t.Fatalf("GPUCacheUsagePerc = %v, want 0.62", state.GPUCacheUsagePerc)
	}
}

func TestInferenceScraperRoundtrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, sampleVLLMMetrics)
	}))
	defer srv.Close()

	scraper := NewInferenceScraper(map[string]string{"vllm-1": srv.URL}, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scraper.Start(ctx)
	defer scraper.Stop()

	// Wait for first scrape.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if state, ok := scraper.Get("vllm-1"); ok && state.NumRequestsRunning == 4 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("did not see scraped state within 1s")
}

func TestInferenceScraperMarksStaleOnFailure(t *testing.T) {
	// Server that always 500s.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	scraper := NewInferenceScraper(map[string]string{"bad": srv.URL}, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scraper.Start(ctx)
	defer scraper.Stop()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if state, ok := scraper.Get("bad"); ok && state.Stale {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if state, ok := scraper.Get("bad"); ok {
		t.Fatalf("expected stale=true, got %+v", state)
	} else {
		t.Fatal("scraper never registered failure entry")
	}
}

func TestInferenceStateLoadScoreReflectsWeights(t *testing.T) {
	a := InferenceState{NumRequestsWaiting: 1}
	b := InferenceState{NumRequestsRunning: 1}
	c := InferenceState{GPUCacheUsagePerc: 1}
	if a.LoadScore() <= b.LoadScore() {
		t.Fatalf("waiting should outweigh running: a=%v b=%v", a.LoadScore(), b.LoadScore())
	}
	if c.LoadScore() <= b.LoadScore() {
		t.Fatalf("cache util should outweigh running for tie-break: c=%v b=%v", c.LoadScore(), b.LoadScore())
	}
	stale := InferenceState{NumRequestsWaiting: 100, Stale: true}
	if stale.LoadScore() != 0 {
		t.Fatalf("stale state should score 0, got %v", stale.LoadScore())
	}
}

func TestNewInferenceScraperFiltersEmptyEndpoints(t *testing.T) {
	scraper := NewInferenceScraper(map[string]string{"a": "", "b": "http://example/metrics"}, 0)
	if len(scraper.endpoints) != 1 {
		t.Fatalf("len(endpoints) = %d, want 1", len(scraper.endpoints))
	}
	if _, ok := scraper.endpoints["b"]; !ok {
		t.Fatal("expected only 'b' to remain")
	}
}
