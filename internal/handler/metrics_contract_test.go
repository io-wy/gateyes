package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsContractRecordsSuccessAndRetry(t *testing.T) {
	attempt := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt <= 2 {
			http.Error(w, `{"error":"boom"}`, http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"id":"chatcmpl-upstream","object":"chat.completion","created":1700000000,"model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"metrics hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
	})

	rec := performJSONRequest(t, env, http.MethodPost, "/v1/responses", "test-key:test-secret", `{"model":"public-model","input":"hello"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/responses status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	metricsRec := performJSONRequest(t, env, http.MethodGet, "/metrics", "", "")
	body := metricsRec.Body.String()
	if !strings.Contains(body, `llm_requests_total{provider="test-openai",result="success",surface="responses"} 1`) {
		t.Fatalf("/metrics missing success request counter: %s", body)
	}
	if !strings.Contains(body, `provider_requests_total{provider="test-openai",result="success"} 1`) {
		t.Fatalf("/metrics missing provider success counter: %s", body)
	}
	if !strings.Contains(body, `llm_retries_total{provider="test-openai"} 2`) {
		t.Fatalf("/metrics missing retry counter: %s", body)
	}
	if !strings.Contains(body, `llm_tokens_total{provider="test-openai",token_type="total"} 5`) {
		t.Fatalf("/metrics missing token counter: %s", body)
	}
}

func TestMetricsContractRecordsUpstreamErrors(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusBadGateway)
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
	})

	rec := performJSONRequest(t, env, http.MethodPost, "/v1/responses", "test-key:test-secret", `{"model":"public-model","input":"hello"}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("POST /v1/responses upstream error status = %d, want %d: %s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}

	metricsRec := performJSONRequest(t, env, http.MethodGet, "/metrics", "", "")
	body := metricsRec.Body.String()
	if !strings.Contains(body, `llm_requests_total{provider="none",result="upstream_error",surface="responses"} 1`) {
		t.Fatalf("/metrics missing upstream error counter: %s", body)
	}
	if !strings.Contains(body, `llm_errors_total{error_class="upstream_5xx",provider="none",surface="responses"} 1`) {
		t.Fatalf("/metrics missing upstream_5xx error counter: %s", body)
	}
}
