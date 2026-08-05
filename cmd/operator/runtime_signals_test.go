package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/service/platform"
)

func TestHTTPRuntimeSignalProviderCollectsPrometheusMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`
# HELP gateyes_inference_queue_depth queue depth
gateyes_inference_queue_depth 12
gateyes_inference_running_requests 3
gateyes_inference_ttft_ms 120
gateyes_inference_gpu_utilization 0.91
gateyes_inference_tpm 2048
`))
	}))
	defer server.Close()

	provider := newHTTPRuntimeSignalProvider()
	provider.urlForService = func(platform.InferenceService, string) string {
		return server.URL
	}
	signals, err := provider.Collect(context.Background(), []platform.InferenceService{{
		Metadata: platform.ObjectMeta{Name: "qwen", Namespace: "llm"},
		Spec:     platform.InferenceServiceSpec{Runtime: "vllm", Model: "Qwen/Qwen3"},
	}}, "llm")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	signal := signals["llm/qwen"]
	if signal.QueueDepth != 12 || signal.RunningRequests != 3 || signal.TTFTMs != 120 || signal.GPUUtilization != 0.91 || signal.TPM != 2048 {
		t.Fatalf("signals = %#v", signal)
	}
}

func TestParsePrometheusRuntimeSignalsSupportsVLLMNames(t *testing.T) {
	signals := parsePrometheusRuntimeSignals([]byte(`
vllm:num_requests_waiting 4
vllm:num_requests_running 2
vllm:gpu_cache_usage_perc 87
`))
	if signals.QueueDepth != 4 || signals.RunningRequests != 2 {
		t.Fatalf("signals = %#v", signals)
	}
	if signals.GPUCacheUsage != 0.87 {
		t.Fatalf("GPUCacheUsage = %v, want 0.87", signals.GPUCacheUsage)
	}
}
