package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/app/config"
)

var (
	updateStreamContracts = flag.Bool("update-stream-contracts", false, "update SSE contract fixtures")
	generatedUUIDPattern  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	generatedMessageID    = regexp.MustCompile(`^msg_[0-9a-f]{8}$`)
)

func TestContractResponsesStream(t *testing.T) {
	contract := streamContractCase{
		fixture:  "responses_text_tool.sse",
		path:     "/v1/responses",
		request:  `{"model":"public-model","input":"hello","stream":true,"tools":[{"type":"function","name":"weather","parameters":{"type":"object"}}]}`,
		endpoint: "responses",
		upstream: strings.Join([]string{
			`data: {"type":"response.output_text.delta","delta":"hello","item_id":"item-text","output_index":0,"content_index":0,"sequence_number":1}`,
			`data: {"type":"response.output_item.done","output_index":1,"sequence_number":2,"item":{"id":"call-weather","type":"function_call","status":"completed","call_id":"call-weather","name":"weather","arguments":"{\"city\":\"Shanghai\"}"}}`,
			`data: {"type":"response.completed","sequence_number":3,"response":{"id":"resp-upstream","object":"response","created_at":1700000000,"model":"provider-model","status":"completed","output":[{"id":"item-text","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello"}]},{"id":"call-weather","type":"function_call","status":"completed","call_id":"call-weather","name":"weather","arguments":"{\"city\":\"Shanghai\"}"}],"usage":{"input_tokens":4,"output_tokens":3,"total_tokens":7,"input_tokens_details":{"cached_tokens":1}}}}`,
			`data: [DONE]`,
		}, "\n\n") + "\n\n",
	}

	body := runStreamContract(t, contract)
	if strings.Contains(body, "thinking_delta") {
		t.Fatalf("Responses stream exposed internal thinking event:\n%s", body)
	}
	assertStreamContractFixture(t, contract.fixture, body)
}

func TestContractChatStream(t *testing.T) {
	tests := []streamContractCase{
		{
			fixture:  "chat_usage_tool.sse",
			path:     "/v1/chat/completions",
			request:  `{"model":"public-model","messages":[{"role":"user","content":"hello"}],"stream":true,"tools":[{"type":"function","function":{"name":"weather","parameters":{"type":"object"}}}]}`,
			endpoint: "chat",
			upstream: strings.Join([]string{
				`data: {"id":"chat-upstream","object":"chat.completion.chunk","created":1700000000,"model":"provider-model","choices":[{"index":0,"delta":{"content":"hello"}}]}`,
				`data: {"id":"chat-upstream","object":"chat.completion.chunk","created":1700000000,"model":"provider-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-weather","type":"function","function":{"name":"weather","arguments":"{\"city\":\"Shanghai\"}"}}]}}]}`,
				`data: {"id":"chat-upstream","object":"chat.completion.chunk","created":1700000000,"model":"provider-model","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`,
				`data: {"id":"chat-upstream","object":"chat.completion.chunk","created":1700000000,"model":"provider-model","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
				`data: [DONE]`,
			}, "\n\n") + "\n\n",
		},
		{
			fixture:  "chat_finish_only.sse",
			path:     "/v1/chat/completions",
			request:  `{"model":"public-model","messages":[{"role":"user","content":"hello"}],"stream":true}`,
			endpoint: "chat",
			upstream: strings.Join([]string{
				`data: {"id":"chat-finish","object":"chat.completion.chunk","created":1700000000,"model":"provider-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`,
				`data: [DONE]`,
			}, "\n\n") + "\n\n",
		},
	}

	for _, contract := range tests {
		contract := contract
		t.Run(strings.TrimSuffix(contract.fixture, ".sse"), func(t *testing.T) {
			body := runStreamContract(t, contract)
			assertStreamContractFixture(t, contract.fixture, body)
		})
	}
}

func TestContractMessagesStream(t *testing.T) {
	contract := streamContractCase{
		fixture: "messages_thinking_tool.sse",
		path:    "/v1/messages",
		request: `{"model":"claude-public","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":true,"thinking":{"type":"enabled","budget_tokens":32},"tools":[{"name":"weather","input_schema":{"type":"object"}}]}`,
		provider: config.ProviderConfig{
			Name: "test-anthropic", Type: "anthropic", APIKey: "anthropic-key", Model: "claude-provider", Timeout: 5, Enabled: true, MaxTokens: 256,
		},
		upstream: strings.Join([]string{
			`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"msg-upstream","type":"message","role":"assistant","model":"claude-provider","content":[],"usage":{"input_tokens":5,"output_tokens":0}}}`,
			`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":"sig"}}`,
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"inspect weather"}}`,
			`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}`,
			`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":"hello"}}`,
			`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":1}`,
			`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"tool-weather","name":"weather","input":{"city":"Shanghai"}}}`,
			`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":2}`,
			`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}`,
			`event: message_stop` + "\n" + `data: {"type":"message_stop"}`,
		}, "\n\n") + "\n\n",
	}

	body := runStreamContract(t, contract)
	if strings.Contains(body, "inspect weather") {
		t.Fatalf("public Messages stream exposed upstream thinking payload:\n%s", body)
	}
	assertStreamContractFixture(t, contract.fixture, body)
}

func TestContractFlushAndClientDisconnectStream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	requestStarted := make(chan struct{})
	upstreamCancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chat-upstream\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"provider-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"first\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(upstreamCancelled)
	}))
	t.Cleanup(upstream.Close)
	t.Cleanup(upstream.CloseClientConnections)

	env := newHandlerTestEnv(t, handlerTestEnvConfig{upstreamURL: upstream.URL, endpoint: "chat"})
	gateway := httptest.NewServer(env.server.engine)
	t.Cleanup(gateway.Close)
	t.Cleanup(gateway.CloseClientConnections)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	if err != nil {
		t.Fatalf("create streaming request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")

	resp, err := gateway.Client().Do(req)
	if err != nil {
		t.Fatalf("start streaming request: %v", err)
	}
	reader := bufio.NewReader(resp.Body)
	frame, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read first flushed frame before upstream completion: %v", err)
	}
	if !strings.HasPrefix(frame, "data: ") {
		t.Fatalf("first flushed line = %q, want SSE data frame", frame)
	}
	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request did not start")
	}

	cancel()
	_ = resp.Body.Close()
	select {
	case <-upstreamCancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("client disconnect was not propagated to the upstream stream")
	}
}

type streamContractCase struct {
	fixture  string
	path     string
	request  string
	endpoint string
	provider config.ProviderConfig
	upstream string
}

func runStreamContract(t *testing.T, contract streamContractCase) string {
	t.Helper()
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, contract.upstream)
	}))
	defer upstream.Close()

	envConfig := handlerTestEnvConfig{upstreamURL: upstream.URL, endpoint: contract.endpoint}
	if contract.provider.Name != "" {
		contract.provider.BaseURL = upstream.URL
		envConfig.providerConfigs = []config.ProviderConfig{contract.provider}
	}
	env := newHandlerTestEnv(t, envConfig)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, contract.path, strings.NewReader(contract.request))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("POST %s status = %d, want %d: %s", contract.path, recorder.Code, http.StatusOK, recorder.Body.String())
	}
	assertContractStreamHeaders(t, recorder.Header())
	if !recorder.Flushed {
		t.Fatalf("POST %s did not flush its SSE response", contract.path)
	}
	return normalizeContractSSE(t, recorder.Body.String())
}

func assertContractStreamHeaders(t *testing.T, headers http.Header) {
	t.Helper()

	checks := map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"Connection":        "keep-alive",
		"X-Accel-Buffering": "no",
	}
	for name, want := range checks {
		if got := headers.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func normalizeContractSSE(t *testing.T, body string) string {
	t.Helper()

	if !strings.HasSuffix(body, "\n\n") {
		t.Fatalf("SSE body does not end with an empty-line frame delimiter: %q", body)
	}
	frames := strings.Split(strings.TrimSuffix(body, "\n\n"), "\n\n")
	var normalized strings.Builder
	for frameIndex, frame := range frames {
		lines := strings.Split(frame, "\n")
		eventType := ""
		dataIndex := 0
		if strings.HasPrefix(lines[0], "event: ") {
			if len(lines) != 2 {
				t.Fatalf("SSE frame %d has event field but %d lines, want event/data pair: %q", frameIndex, len(lines), frame)
			}
			eventType = strings.TrimPrefix(lines[0], "event: ")
			dataIndex = 1
		} else if len(lines) != 1 {
			t.Fatalf("SSE frame %d has %d lines without event field, want one data line: %q", frameIndex, len(lines), frame)
		}
		if !strings.HasPrefix(lines[dataIndex], "data: ") {
			t.Fatalf("SSE frame %d data line = %q, want data prefix", frameIndex, lines[dataIndex])
		}

		data := strings.TrimPrefix(lines[dataIndex], "data: ")
		if data == "[DONE]" {
			if eventType != "" {
				t.Fatalf("SSE [DONE] frame unexpectedly has event type %q", eventType)
			}
			if frameIndex != len(frames)-1 {
				t.Fatalf("SSE [DONE] frame is %d of %d, want terminal frame", frameIndex+1, len(frames))
			}
			normalized.WriteString("data: [DONE]\n\n")
			continue
		}

		decoder := json.NewDecoder(strings.NewReader(data))
		decoder.UseNumber()
		var payload any
		if err := decoder.Decode(&payload); err != nil {
			t.Fatalf("decode SSE frame %d JSON: %v", frameIndex, err)
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			t.Fatalf("SSE frame %d has trailing JSON content: %v", frameIndex, err)
		}
		if eventType != "" {
			object, ok := payload.(map[string]any)
			if !ok || object["type"] != eventType {
				t.Fatalf("SSE frame %d event = %q but JSON type = %#v", frameIndex, eventType, object["type"])
			}
		}
		normalizeContractJSON(t, payload)
		var encoded bytes.Buffer
		encoder := json.NewEncoder(&encoded)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(payload); err != nil {
			t.Fatalf("encode normalized SSE frame %d: %v", frameIndex, err)
		}
		if eventType != "" {
			normalized.WriteString("event: ")
			normalized.WriteString(eventType)
			normalized.WriteByte('\n')
		}
		normalized.WriteString("data: ")
		normalized.Write(bytes.TrimSuffix(encoded.Bytes(), []byte{'\n'}))
		normalized.WriteString("\n\n")
	}
	return normalized.String()
}

func normalizeContractJSON(t *testing.T, value any) {
	t.Helper()

	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch key {
			case "created", "created_at":
				if _, ok := child.(json.Number); !ok {
					t.Fatalf("dynamic timestamp field %q has type %T, want JSON number", key, child)
				}
				typed[key] = json.Number("0")
			case "id":
				if id, ok := child.(string); ok && isGeneratedContractID(id) {
					typed[key] = "<generated-id>"
				}
			default:
				normalizeContractJSON(t, child)
			}
		}
	case []any:
		for _, child := range typed {
			normalizeContractJSON(t, child)
		}
	}
}

func isGeneratedContractID(id string) bool {
	return generatedUUIDPattern.MatchString(id) || generatedMessageID.MatchString(id)
}

func assertStreamContractFixture(t *testing.T, name, got string) {
	t.Helper()

	path := streamContractFixturePath(t, name)
	if *updateStreamContracts {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create stream fixture directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write stream contract fixture: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stream contract fixture %s: %v (regenerate explicitly with -update-stream-contracts)", name, err)
	}
	if !bytes.Equal(want, []byte(got)) {
		t.Fatalf("stream contract %s changed:\nwant:\n%s\ngot:\n%s", name, want, got)
	}
}

func streamContractFixturePath(t *testing.T, name string) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate stream contract test file")
	}
	return filepath.Join(filepath.Dir(currentFile), "..", "..", "testdata", "contracts", "http", "stream", name)
}
