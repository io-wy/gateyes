package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/provider"
)

func TestHydratePreviousResponseBuildsChronologicalTranscript(t *testing.T) {
	env := newResponsesTestEnv(t, responsesTestEnvConfig{providers: []string{"test-openai"}})
	persistResponseTurn(t, env, "resp-a", "", "first question", "first answer")
	persistResponseTurn(t, env, "resp-b", "resp-a", "second question", "second answer")

	req := &provider.ResponseRequest{
		Model:              "public-model",
		PreviousResponseID: "resp-b",
		Input:              "third question",
	}
	if err := env.service.hydratePreviousResponse(context.Background(), env.identity, req); err != nil {
		t.Fatalf("hydratePreviousResponse() error: %v", err)
	}

	messages := req.InputMessages()
	want := []string{"first question", "first answer", "second question", "second answer", "third question"}
	if len(messages) != len(want) {
		t.Fatalf("hydrated messages len = %d, want %d", len(messages), len(want))
	}
	for i, text := range want {
		if got := messageText(messages[i]); got != text {
			t.Fatalf("messages[%d] = %q, want %q", i, got, text)
		}
	}
}

func TestCreateSendsHydratedTranscriptAndPersistsChainReference(t *testing.T) {
	var upstreamBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode upstream body: %v", err)
		}
		raw, _ := json.Marshal(body)
		upstreamBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-c","object":"chat.completion","created":1,"model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"third answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`))
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})
	persistResponseTurn(t, env, "resp-a", "", "first question", "first answer")
	persistResponseTurn(t, env, "resp-b", "resp-a", "second question", "second answer")

	result, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model", PreviousResponseID: "resp-b", Input: "third question",
	}, "")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if result.Response.OutputText() != "third answer" {
		t.Fatalf("Create() output = %q, want third answer", result.Response.OutputText())
	}
	for _, text := range []string{"first question", "first answer", "second question", "second answer", "third question"} {
		if !strings.Contains(upstreamBody, text) {
			t.Fatalf("upstream body %s does not contain %q", upstreamBody, text)
		}
	}

	record, err := env.store.GetResponse(context.Background(), env.identity.TenantID, result.Response.ID)
	if err != nil {
		t.Fatalf("GetResponse() error: %v", err)
	}
	var stored provider.ResponseRequest
	if err := json.Unmarshal(record.RequestBody, &stored); err != nil {
		t.Fatalf("decode stored request: %v", err)
	}
	if stored.PreviousResponseID != "resp-b" {
		t.Fatalf("stored previous_response_id = %q, want resp-b", stored.PreviousResponseID)
	}
	if got := stored.InputText(); strings.Contains(got, "first question") || !strings.Contains(got, "third question") {
		t.Fatalf("stored request input = %q, want only current turn", got)
	}
}

func TestCreateStreamHydratesPreviousResponse(t *testing.T) {
	env := newResponsesTestEnv(t, responsesTestEnvConfig{providers: []string{"test-openai"}})
	_, err := env.service.CreateStream(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model", PreviousResponseID: "missing", Input: "next", Stream: true,
	}, "")
	if !errors.Is(err, ErrInvalidPreviousResponse) {
		t.Fatalf("CreateStream() error = %v, want ErrInvalidPreviousResponse", err)
	}
}

func TestCreateRechecksQuotaAfterPreviousResponseHydration(t *testing.T) {
	var upstreamCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"unexpected","object":"chat.completion","created":1,"model":"provider-model","choices":[]}`))
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL, endpoint: "chat", providers: []string{"test-openai"},
	})
	persistResponseTurn(t, env, "resp-a", "", strings.Repeat("history ", 200), "answer")
	wireReq := &provider.ResponseRequest{Input: "next", MaxOutputTokens: 1}
	env.identity.Quota = wireReq.EstimateAdmissionTokens() + 10
	env.identity.Used = 0

	_, err := env.service.Create(WithAdmissionChecked(context.Background()), env.identity, &provider.ResponseRequest{
		Model: "public-model", PreviousResponseID: "resp-a", Input: "next", MaxOutputTokens: 1,
	}, "")
	if !errors.Is(err, auth.ErrQuotaExceeded) {
		t.Fatalf("Create() error = %v, want ErrQuotaExceeded", err)
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstream calls = %d, want 0", upstreamCalls)
	}
}

func TestHydratePreviousResponseRejectsOutOfScopeResponse(t *testing.T) {
	env := newResponsesTestEnv(t, responsesTestEnvConfig{providers: []string{"test-openai"}})
	persistResponseTurn(t, env, "resp-a", "", "question", "answer")

	tests := []struct {
		name     string
		identity repository.AuthIdentity
	}{
		{name: "different tenant", identity: repository.AuthIdentity{TenantID: "tenant-b", ProjectID: env.identity.ProjectID}},
		{name: "different project", identity: repository.AuthIdentity{TenantID: env.identity.TenantID, ProjectID: "project-b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := env.service.hydratePreviousResponse(context.Background(), &tt.identity, &provider.ResponseRequest{
				PreviousResponseID: "resp-a", Input: "next",
			})
			if !errors.Is(err, ErrInvalidPreviousResponse) {
				t.Fatalf("hydratePreviousResponse() error = %v, want ErrInvalidPreviousResponse", err)
			}
		})
	}
}

func TestHydratePreviousResponseRejectsInvalidRecord(t *testing.T) {
	env := newResponsesTestEnv(t, responsesTestEnvConfig{providers: []string{"test-openai"}})
	tests := []struct {
		name         string
		status       string
		requestBody  []byte
		responseBody []byte
	}{
		{name: "incomplete", status: "in_progress", requestBody: []byte(`{"input":"question"}`), responseBody: []byte(`{}`)},
		{name: "invalid request shape", status: "completed", requestBody: []byte(`[]`), responseBody: []byte(`{}`)},
		{name: "invalid response shape", status: "completed", requestBody: []byte(`{}`), responseBody: []byte(`[]`)},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := fmt.Sprintf("resp-invalid-%d", i)
			if err := env.store.CreateResponse(context.Background(), repository.ResponseRecord{
				ID: id, TenantID: env.identity.TenantID, ProjectID: env.identity.ProjectID,
				Status: tt.status, RequestBody: tt.requestBody, ResponseBody: tt.responseBody,
			}); err != nil {
				t.Fatalf("CreateResponse() error: %v", err)
			}
			err := env.service.hydratePreviousResponse(context.Background(), env.identity, &provider.ResponseRequest{
				PreviousResponseID: id, Input: "next",
			})
			if !errors.Is(err, ErrInvalidPreviousResponse) {
				t.Fatalf("hydratePreviousResponse() error = %v, want ErrInvalidPreviousResponse", err)
			}
		})
	}
}

func TestHydratePreviousResponseRejectsCycle(t *testing.T) {
	env := newResponsesTestEnv(t, responsesTestEnvConfig{providers: []string{"test-openai"}})
	persistResponseTurn(t, env, "resp-a", "resp-b", "a", "answer a")
	persistResponseTurn(t, env, "resp-b", "resp-a", "b", "answer b")

	err := env.service.hydratePreviousResponse(context.Background(), env.identity, &provider.ResponseRequest{
		PreviousResponseID: "resp-a", Input: "next",
	})
	if !errors.Is(err, ErrInvalidPreviousResponse) || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("hydratePreviousResponse() error = %v, want cycle error", err)
	}
}

func TestHydratePreviousResponseRejectsExcessiveDepth(t *testing.T) {
	env := newResponsesTestEnv(t, responsesTestEnvConfig{providers: []string{"test-openai"}})
	previousID := ""
	for i := 0; i <= maxPreviousResponseDepth; i++ {
		id := fmt.Sprintf("resp-%02d", i)
		persistResponseTurn(t, env, id, previousID, id, "answer")
		previousID = id
	}

	err := env.service.hydratePreviousResponse(context.Background(), env.identity, &provider.ResponseRequest{
		PreviousResponseID: previousID, Input: "next",
	})
	if !errors.Is(err, ErrInvalidPreviousResponse) || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("hydratePreviousResponse() error = %v, want depth error", err)
	}
}

func TestHydratedCacheKeyIncludesConversationHistory(t *testing.T) {
	env := newResponsesTestEnv(t, responsesTestEnvConfig{providers: []string{"test-openai"}})
	persistResponseTurn(t, env, "resp-a", "", "topic a", "answer a")
	persistResponseTurn(t, env, "resp-b", "", "topic b", "answer b")

	reqA := &provider.ResponseRequest{Model: "m", PreviousResponseID: "resp-a", Input: "continue"}
	reqB := &provider.ResponseRequest{Model: "m", PreviousResponseID: "resp-b", Input: "continue"}
	if err := env.service.hydratePreviousResponse(context.Background(), env.identity, reqA); err != nil {
		t.Fatalf("hydrate A: %v", err)
	}
	if err := env.service.hydratePreviousResponse(context.Background(), env.identity, reqB); err != nil {
		t.Fatalf("hydrate B: %v", err)
	}
	if keyA, keyB := env.service.buildCacheKey(context.Background(), env.identity, reqA), env.service.buildCacheKey(context.Background(), env.identity, reqB); keyA == keyB {
		t.Fatalf("cache keys are equal for different histories: %q", keyA)
	}
}

func persistResponseTurn(t *testing.T, env *responsesTestEnv, id, previousID, input, output string) {
	t.Helper()
	requestBody, _ := json.Marshal(map[string]any{
		"model": "public-model", "previous_response_id": previousID, "input": input,
	})
	responseBody, _ := json.Marshal(provider.Response{
		ID: id, Status: "completed",
		Output: []provider.ResponseOutput{{
			Type: "message", Role: "assistant",
			Content: []provider.ResponseContent{{Type: "output_text", Text: output}},
		}},
	})
	if err := env.store.CreateResponse(context.Background(), repository.ResponseRecord{
		ID: id, TenantID: env.identity.TenantID, ProjectID: env.identity.ProjectID,
		Status: "completed", RequestBody: requestBody, ResponseBody: responseBody,
	}); err != nil {
		t.Fatalf("CreateResponse(%s) error: %v", id, err)
	}
}

func messageText(message provider.Message) string {
	var result string
	for _, content := range message.Content {
		result += content.Text
	}
	return result
}
