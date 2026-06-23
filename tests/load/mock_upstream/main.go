// mock_upstream is a lightweight OpenAI-compatible upstream server for load
// testing Gateyes without hitting real LLM providers.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultAddr         = ":18080"
	defaultOutputTokens = 64
	defaultTokensPerSec = 20
)

type config struct {
	addr         string
	delay        time.Duration
	outputTokens int
	tokensPerSec float64
	failRate     float64
	streamJitter time.Duration
}

type chatRequest struct {
	Model     string    `json:"model"`
	Messages  []message `json:"messages"`
	Stream    bool      `json:"stream,omitempty"`
	MaxTokens int       `json:"max_tokens,omitempty"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
}

type choice struct {
	Index        int     `json:"index"`
	Message      message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type streamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []streamChoice `json:"choices"`
}

type streamChoice struct {
	Index        int         `json:"index"`
	Delta        streamDelta `json:"delta"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

type streamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Object string      `json:"object"`
	Data   []embedding `json:"data"`
	Model  string      `json:"model"`
	Usage  usage       `json:"usage"`
}

type embedding struct {
	Object    string    `json:"object"`
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type imageRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	N      int    `json:"n"`
	Size   string `json:"size"`
}

type imageResponse struct {
	Created int64  `json:"created"`
	Data    []image `json:"data"`
}

type image struct {
	URL string `json:"url"`
}

func (s *server) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req embeddingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if s.cfg.failRate > 0 && rand.Float64() < s.cfg.failRate {
		http.Error(w, "mock upstream failure", http.StatusInternalServerError)
		return
	}

	vector := make([]float64, 1536)
	for i := range vector {
		vector[i] = 0.001 * float64(i)
	}

	data := make([]embedding, len(req.Input))
	for i := range req.Input {
		data[i] = embedding{Object: "embedding", Embedding: vector, Index: i}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(embeddingResponse{
		Object: "list",
		Data:   data,
		Model:  req.Model,
		Usage: usage{
			PromptTokens:     len(req.Input) * 4,
			CompletionTokens: 0,
			TotalTokens:      len(req.Input) * 4,
		},
	})
}

func (s *server) handleImageGenerations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req imageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if s.cfg.failRate > 0 && rand.Float64() < s.cfg.failRate {
		http.Error(w, "mock upstream failure", http.StatusInternalServerError)
		return
	}

	n := req.N
	if n <= 0 {
		n = 1
	}
	data := make([]image, n)
	for i := range data {
		data[i] = image{URL: fmt.Sprintf("https://mock.gateyes.test/image-%d.png?prompt=%s&size=%s", i, req.Prompt, req.Size)}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(imageResponse{
		Created: time.Now().Unix(),
		Data:    data,
	})
}

func main() {
	cfg := parseConfig()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	srv := newServer(cfg)

	http.HandleFunc("/health", srv.handleHealth)
	http.HandleFunc("/v1/chat/completions", srv.handleChatCompletions)
	http.HandleFunc("/chat/completions", srv.handleChatCompletions)
	http.HandleFunc("/v1/embeddings", srv.handleEmbeddings)
	http.HandleFunc("/embeddings", srv.handleEmbeddings)
	http.HandleFunc("/v1/images/generations", srv.handleImageGenerations)
	http.HandleFunc("/images/generations", srv.handleImageGenerations)
	http.HandleFunc("/v1/models", srv.handleModels)
	http.HandleFunc("/models", srv.handleModels)

	slog.Info("mock upstream listening",
		"addr", cfg.addr,
		"delay", cfg.delay,
		"output_tokens", cfg.outputTokens,
		"tokens_per_sec", cfg.tokensPerSec,
		"fail_rate", cfg.failRate,
	)

	if err := http.ListenAndServe(cfg.addr, nil); err != nil {
		slog.Error("mock upstream stopped", "error", err)
		os.Exit(1)
	}
}

func parseConfig() config {
	var cfg config
	flag.StringVar(&cfg.addr, "addr", defaultAddr, "listen address")
	flag.DurationVar(&cfg.delay, "delay", 50*time.Millisecond, "fixed latency before the first token (TTFT)")
	flag.IntVar(&cfg.outputTokens, "output-tokens", defaultOutputTokens, "number of completion tokens to generate")
	flag.Float64Var(&cfg.tokensPerSec, "tokens-per-sec", defaultTokensPerSec, "streaming token generation rate")
	flag.Float64Var(&cfg.failRate, "fail-rate", 0, "probability of returning a 500 error (0-1)")
	flag.DurationVar(&cfg.streamJitter, "stream-jitter", 5*time.Millisecond, "max random delay added between stream chunks")
	flag.Parse()
	return cfg
}

type server struct {
	cfg config
}

func newServer(cfg config) *server {
	return &server{cfg: cfg}
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"mock-model","object":"model","created":0,"owned_by":"mock"}]}`))
}

func (s *server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if s.cfg.failRate > 0 && rand.Float64() < s.cfg.failRate {
		http.Error(w, "mock upstream failure", http.StatusInternalServerError)
		return
	}

	promptTokens := estimatePromptTokens(req.Messages)
	outputTokens := s.cfg.outputTokens
	if req.MaxTokens > 0 && req.MaxTokens < outputTokens {
		outputTokens = req.MaxTokens
	}

	if req.Stream {
		s.writeStream(w, r, req.Model, promptTokens, outputTokens)
		return
	}

	time.Sleep(s.cfg.delay)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(chatResponse{
		ID:      fmt.Sprintf("mock-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []choice{{
			Index:        0,
			Message:      message{Role: "assistant", Content: generateText(outputTokens)},
			FinishReason: "stop",
		}},
		Usage: usage{
			PromptTokens:     promptTokens,
			CompletionTokens: outputTokens,
			TotalTokens:      promptTokens + outputTokens,
		},
	})
}

func (s *server) writeStream(w http.ResponseWriter, r *http.Request, model string, promptTokens, outputTokens int) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	responseID := fmt.Sprintf("mock-%d", time.Now().UnixNano())
	text := generateText(outputTokens)

	// Simulate time-to-first-token.
	time.Sleep(s.cfg.delay)

	chunkCount := outputTokens
	if chunkCount < 1 {
		chunkCount = 1
	}
	chunkSize := len(text) / chunkCount
	if chunkSize < 1 {
		chunkSize = 1
	}

	baseInterval := time.Duration(float64(time.Second) / s.cfg.tokensPerSec)

	// First chunk emits the assistant role with empty content.
	writeSSE(w, streamChunk{
		ID:      responseID,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []streamChoice{{
			Index: 0,
			Delta: streamDelta{Role: "assistant"},
		}},
	})
	flusher.Flush()

	for i := 0; i < len(text); i += chunkSize {
		end := i + chunkSize
		if end > len(text) {
			end = len(text)
		}

		finishReason := ""
		if end >= len(text) {
			finishReason = "stop"
		}

		writeSSE(w, streamChunk{
			ID:      responseID,
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   model,
			Choices: []streamChoice{{
				Index:        0,
				Delta:        streamDelta{Content: text[i:end]},
				FinishReason: finishReason,
			}},
		})
		flusher.Flush()

		if r.Context().Err() != nil {
			return
		}

		if end < len(text) {
			sleep := baseInterval
			if s.cfg.streamJitter > 0 {
				sleep += time.Duration(rand.Int63n(int64(s.cfg.streamJitter)))
			}
			time.Sleep(sleep)
		}
	}

	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func writeSSE(w http.ResponseWriter, chunk streamChunk) {
	data, _ := json.Marshal(chunk)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}

// estimatePromptTokens uses a rough heuristic: ~1 token per 4 characters of
// English text, or ~1 token per Chinese character. This is good enough for
// load testing metrics; it is not meant to match a real tokenizer.
func estimatePromptTokens(messages []message) int {
	total := 0
	for _, m := range messages {
		text := m.Content
		if text == "" {
			continue
		}
		// Add a small per-message overhead.
		total += 4
		for _, r := range text {
			if r > 127 {
				total += 1
			}
		}
		total += len([]rune(text)) / 4
	}
	if total == 0 {
		return 1
	}
	return total
}

func generateText(tokens int) string {
	words := []string{
		"mock", "upstream", "response", "used", "for", "load", "testing",
		"gateyes", "gateway", "performance", "analysis", "baseline", "stress",
	}
	var sb strings.Builder
	for i := 0; i < tokens; i++ {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(words[i%len(words)])
	}
	return sb.String()
}
