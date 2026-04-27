package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync/atomic"
	"time"
)

var (
	totalReqs  atomic.Int64
	startTime  time.Time
	requestIDs atomic.Int64
)

func main() {
	port := flag.Int("port", 19999, "mock server port")
	flag.Parse()
	startTime = time.Now()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", handleResponses)
	mux.HandleFunc("/v1/chat/completions", handleChatCompletions)
	mux.HandleFunc("/v1/messages", handleMessages)
	mux.HandleFunc("/v1/embeddings", handleEmbeddings)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	addr := fmt.Sprintf(":%d", *port)
	fmt.Printf("mock upstream listening on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func handleResponses(w http.ResponseWriter, r *http.Request) {
	n := totalReqs.Add(1)
	elapsed := time.Since(startTime).Seconds()
	if elapsed > 0 {
		fmt.Printf("\r  mock upstream: %d reqs, %.1f RPS   ", n, float64(n)/elapsed)
	}

	// Check if stream is requested
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	isStream := false
	if v, ok := body["stream"].(bool); ok && v {
		isStream = true
	}

	if isStream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(30 * time.Millisecond)
		fmt.Fprintf(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-mock-%d\",\"status\":\"in_progress\"}}\n\n", requestIDs.Add(1))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(50 * time.Millisecond)
		fmt.Fprint(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello \"}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(50 * time.Millisecond)
		fmt.Fprint(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"from mock upstream!\"}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(30 * time.Millisecond)
		fmt.Fprintf(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-mock-%d\",\"status\":\"completed\",\"output_text\":\"Hello from mock upstream!\",\"usage\":{\"input_tokens\":10,\"output_tokens\":5,\"total_tokens\":15}}}\n\n", requestIDs.Add(1))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}

	time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":      fmt.Sprintf("resp-mock-%d", requestIDs.Add(1)),
		"object":  "response",
		"status":  "completed",
		"model":   "mock-model",
		"output": []map[string]any{
			{
				"type": "message",
				"role": "assistant",
				"content": []map[string]any{
					{"type": "output_text", "text": "Hello from mock upstream!"},
				},
			},
		},
		"usage": map[string]any{
			"input_tokens":  10,
			"output_tokens": 5,
			"total_tokens":  15,
		},
	})
}

func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	n := totalReqs.Add(1)
	elapsed := time.Since(startTime).Seconds()
	if elapsed > 0 {
		fmt.Printf("\r  mock upstream: %d reqs, %.1f RPS   ", n, float64(n)/elapsed)
	}

	// Check if stream is requested
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	isStream := false
	if v, ok := body["stream"].(bool); ok && v {
		isStream = true
	}

	if isStream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		reqID := requestIDs.Add(1)
		fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-mock-%d\",\"object\":\"chat.completion.chunk\",\"created\":%d,\"model\":\"mock-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n", reqID, time.Now().Unix())
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(50 * time.Millisecond)
		fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-mock-%d\",\"object\":\"chat.completion.chunk\",\"created\":%d,\"model\":\"mock-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello \"},\"finish_reason\":null}]}\n\n", reqID, time.Now().Unix())
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(50 * time.Millisecond)
		fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-mock-%d\",\"object\":\"chat.completion.chunk\",\"created\":%d,\"model\":\"mock-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"from mock upstream!\"},\"finish_reason\":null}]}\n\n", reqID, time.Now().Unix())
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(30 * time.Millisecond)
		fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-mock-%d\",\"object\":\"chat.completion.chunk\",\"created\":%d,\"model\":\"mock-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n", reqID, time.Now().Unix())
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}

	// simulate 50-150ms upstream latency
	time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":      fmt.Sprintf("chatcmpl-mock-%d", requestIDs.Add(1)),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "mock-model",
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "Hello from mock upstream!",
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 8,
			"total_tokens":      18,
		},
	})
}

func handleMessages(w http.ResponseWriter, r *http.Request) {
	n := totalReqs.Add(1)
	elapsed := time.Since(startTime).Seconds()
	if elapsed > 0 {
		fmt.Printf("\r  mock upstream: %d reqs, %.1f RPS   ", n, float64(n)/elapsed)
	}

	// Check if stream is requested
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	isStream := false
	if v, ok := body["stream"].(bool); ok && v {
		isStream = true
	}

	if isStream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		reqID := requestIDs.Add(1)
		fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_%d\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"mock-model\"}}\n\n", reqID)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(50 * time.Millisecond)
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello \"}}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(50 * time.Millisecond)
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"from mock upstream!\"}}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(30 * time.Millisecond)
		fmt.Fprintf(w, "event: message_stop\ndata: {\"type\":\"message_stop\",\"message\":{\"id\":\"msg_%d\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"Hello from mock upstream!\"}],\"model\":\"mock-model\",\"stop_reason\":\"end_turn\",\"usage\":{\"input_tokens\":10,\"output_tokens\":8}}}\n\n", reqID)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		return
	}

	time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)

	reqID := fmt.Sprintf("msg_%d", requestIDs.Add(1))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":   reqID,
		"type": "message",
		"role": "assistant",
		"content": []map[string]any{
			{"type": "text", "text": "Hello from mock upstream!"},
		},
		"model":     "mock-model",
		"stop_reason": "end_turn",
		"usage": map[string]any{
			"input_tokens":  10,
			"output_tokens": 8,
		},
	})
}

func handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	n := totalReqs.Add(1)
	elapsed := time.Since(startTime).Seconds()
	if elapsed > 0 {
		fmt.Printf("\r  mock upstream: %d reqs, %.1f RPS   ", n, float64(n)/elapsed)
	}

	time.Sleep(time.Duration(10+rand.Intn(30)) * time.Millisecond)

	// read input count from body
	var body struct {
		Input any `json:"input"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	count := 1
	switch v := body.Input.(type) {
	case []any:
		count = len(v)
	case string:
		count = 1
	}

	dim := 1536
	embeddings := make([]map[string]any, count)
	for i := 0; i < count; i++ {
		vec := make([]float64, dim)
		for j := range vec {
			vec[j] = rand.NormFloat64()
		}
		embeddings[i] = map[string]any{"object": "embedding", "index": i, "embedding": vec}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   embeddings,
		"model":  "mock-embedding",
		"usage": map[string]any{
			"prompt_tokens": count * 5,
			"total_tokens":  count * 5,
		},
	})
}

// health check support
func init() {
	// catch-all for any health-like endpoints
}
