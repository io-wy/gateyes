package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gateyes/gateway/internal/service/platform"
)

type runtimeSignalProvider interface {
	Collect(context.Context, []platform.InferenceService, string) (map[string]platform.RuntimeSignals, error)
}

type httpRuntimeSignalProvider struct {
	client        *http.Client
	urlForService func(platform.InferenceService, string) string
}

func newHTTPRuntimeSignalProvider() *httpRuntimeSignalProvider {
	provider := &httpRuntimeSignalProvider{
		client: &http.Client{Timeout: 2 * time.Second},
	}
	provider.urlForService = provider.defaultURLForService
	return provider
}

func (p *httpRuntimeSignalProvider) Collect(ctx context.Context, services []platform.InferenceService, defaultNamespace string) (map[string]platform.RuntimeSignals, error) {
	signals := map[string]platform.RuntimeSignals{}
	var errs []error
	for _, service := range services {
		if strings.EqualFold(service.Spec.Runtime, "external") {
			continue
		}
		url := p.urlForService(service, defaultNamespace)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", service.Metadata.Name, err))
			continue
		}
		resp, err := p.client.Do(req)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", service.Metadata.Name, err))
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			errs = append(errs, fmt.Errorf("%s: %w", service.Metadata.Name, readErr))
			continue
		}
		if closeErr != nil {
			errs = append(errs, fmt.Errorf("%s: %w", service.Metadata.Name, closeErr))
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errs = append(errs, fmt.Errorf("%s: metrics status %d", service.Metadata.Name, resp.StatusCode))
			continue
		}
		key := defaultNamespaceName(service.Metadata.Namespace, defaultNamespace) + "/" + service.Metadata.Name
		signals[key] = parsePrometheusRuntimeSignals(body)
	}
	return signals, errors.Join(errs...)
}

func (p *httpRuntimeSignalProvider) defaultURLForService(service platform.InferenceService, defaultNamespace string) string {
	namespace := defaultNamespaceName(service.Metadata.Namespace, defaultNamespace)
	port := service.Spec.Serving.Port
	if port <= 0 {
		port = 8000
	}
	metricsPath := service.Spec.Serving.MetricsPath
	if strings.TrimSpace(metricsPath) == "" {
		metricsPath = "/metrics"
	}
	if !strings.HasPrefix(metricsPath, "/") {
		metricsPath = "/" + metricsPath
	}
	return fmt.Sprintf("http://%s.%s.svc:%d%s", service.Metadata.Name, namespace, port, metricsPath)
}

func parsePrometheusRuntimeSignals(body []byte) platform.RuntimeSignals {
	var signals platform.RuntimeSignals
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		name, value, ok := prometheusSample(scanner.Text())
		if !ok {
			continue
		}
		switch name {
		case "gateyes_inference_queue_depth", "vllm_num_requests_waiting", "vllm:num_requests_waiting":
			signals.QueueDepth = math.Max(signals.QueueDepth, value)
		case "gateyes_inference_running_requests", "vllm_num_requests_running", "vllm:num_requests_running":
			signals.RunningRequests = math.Max(signals.RunningRequests, value)
		case "gateyes_inference_ttft_ms":
			signals.TTFTMs = math.Max(signals.TTFTMs, value)
		case "gateyes_inference_p95_latency_ms":
			signals.P95LatencyMs = math.Max(signals.P95LatencyMs, value)
		case "gateyes_inference_gpu_utilization":
			signals.GPUUtilization = math.Max(signals.GPUUtilization, ratioValue(value))
		case "gateyes_inference_gpu_cache_usage", "vllm_gpu_cache_usage_perc", "vllm:gpu_cache_usage_perc":
			signals.GPUCacheUsage = math.Max(signals.GPUCacheUsage, ratioValue(value))
		case "gateyes_inference_cpu_cache_usage":
			signals.CPUCacheUsage = math.Max(signals.CPUCacheUsage, ratioValue(value))
		case "gateyes_inference_tpm":
			signals.TPM = math.Max(signals.TPM, value)
		case "gateyes_inference_rpm":
			signals.RPM = math.Max(signals.RPM, value)
		}
	}
	return signals
}

func prometheusSample(line string) (string, float64, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", 0, false
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", 0, false
	}
	name := fields[0]
	if idx := strings.IndexByte(name, '{'); idx >= 0 {
		name = name[:idx]
	}
	value, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return "", 0, false
	}
	return name, value, true
}

func ratioValue(value float64) float64 {
	if value > 1 {
		return value / 100
	}
	return value
}

func defaultNamespaceName(namespace string, fallback string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace != "" {
		return namespace
	}
	if strings.TrimSpace(fallback) != "" {
		return strings.TrimSpace(fallback)
	}
	return "default"
}
