package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"gateyes/internal/concurrency"
	"gateyes/internal/requestctx"
	"gateyes/internal/scheduler"
)

type GatewayHandlers struct {
	selector scheduler.Selector
	limiter  concurrency.Manager
	models   []string
}

func NewGatewayHandlers(selector scheduler.Selector, limiter concurrency.Manager, models []string) *GatewayHandlers {
	return &GatewayHandlers{
		selector: selector,
		limiter:  limiter,
		models:   models,
	}
}

func (h *GatewayHandlers) Models(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed", TypeInvalidRequest, "")
		return
	}

	items := make([]map[string]any, 0, len(h.models))
	for _, model := range h.models {
		items = append(items, map[string]any{
			"id":       model,
			"object":   "model",
			"created":  time.Now().Unix(),
			"owned_by": "gateway",
		})
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   items,
	})
}

type chatCompletionRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Stream bool `json:"stream"`
}

func (h *GatewayHandlers) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed", TypeInvalidRequest, "")
		return
	}

	var req chatCompletionRequest
	if err := DecodeJSONStrict(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error(), TypeInvalidRequest, "")
		return
	}
	if req.Model == "" {
		WriteError(w, http.StatusBadRequest, "model is required", TypeInvalidRequest, "")
		return
	}
	if len(req.Messages) == 0 {
		WriteError(w, http.StatusBadRequest, "messages is required", TypeInvalidRequest, "")
		return
	}

	decision, err := h.selector.Select(r.Context(), scheduler.Request{
		Model:     req.Model,
		TokenID:   requestctx.TokenID(r.Context()),
		SessionID: requestctx.SessionID(r.Context()),
	})
	if err != nil {
		if errors.Is(err, scheduler.ErrNoAvailableChannel) {
			WriteError(w, http.StatusServiceUnavailable, "no available channel", TypeServiceUnavailable, "")
			return
		}
		WriteError(w, http.StatusInternalServerError, "scheduler failed", TypeInternalError, "")
		return
	}

	release, err := h.limiter.Acquire(r.Context(), concurrency.AcquireKeys{
		ChannelID: decision.ChannelID,
		TokenID:   requestctx.TokenID(r.Context()),
	})
	if err != nil {
		WriteError(w, http.StatusTooManyRequests, err.Error(), TypeRateLimitError, "")
		return
	}
	defer release()

	if req.Stream {
		h.writeStreamPlaceholder(w, req.Model, decision)
		return
	}

	// TODO(io): implement provider request conversion + upstream forwarding + error mapping.
	WriteJSON(w, http.StatusOK, map[string]any{
		"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "TODO: upstream forwarding not implemented yet",
				},
				"finish_reason": "stop",
			},
		},
		"gateway_debug": map[string]any{
			"channel_id":     decision.ChannelID,
			"provider":       decision.Provider,
			"upstream_model": decision.UpstreamModel,
		},
	})
}

func (h *GatewayHandlers) writeStreamPlaceholder(w http.ResponseWriter, model string, decision scheduler.Decision) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "stream is not supported by this server", TypeInternalError, "")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	_, _ = io.WriteString(w, "data: {\"object\":\"chat.completion.chunk\",\"model\":\""+model+"\",\"choices\":[{\"delta\":{\"content\":\"TODO\"}}]}\n\n")
	flusher.Flush()
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	flusher.Flush()

	_ = decision // TODO(io): include decision metadata in structured logs instead of inline response.
}

type embeddingsRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

func (h *GatewayHandlers) Embeddings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed", TypeInvalidRequest, "")
		return
	}

	var req embeddingsRequest
	if err := DecodeJSONStrict(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error(), TypeInvalidRequest, "")
		return
	}
	if req.Model == "" {
		WriteError(w, http.StatusBadRequest, "model is required", TypeInvalidRequest, "")
		return
	}
	if req.Input == nil {
		WriteError(w, http.StatusBadRequest, "input is required", TypeInvalidRequest, "")
		return
	}

	decision, err := h.selector.Select(r.Context(), scheduler.Request{
		Model:     req.Model,
		TokenID:   requestctx.TokenID(r.Context()),
		SessionID: requestctx.SessionID(r.Context()),
	})
	if err != nil {
		WriteError(w, http.StatusServiceUnavailable, "no available channel", TypeServiceUnavailable, "")
		return
	}

	release, err := h.limiter.Acquire(r.Context(), concurrency.AcquireKeys{
		ChannelID: decision.ChannelID,
		TokenID:   requestctx.TokenID(r.Context()),
	})
	if err != nil {
		WriteError(w, http.StatusTooManyRequests, err.Error(), TypeRateLimitError, "")
		return
	}
	defer release()

	// TODO(io): implement upstream embeddings call and normalize provider-specific response.
	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data": []map[string]any{
			{
				"object":    "embedding",
				"index":     0,
				"embedding": []float64{0, 0, 0, 0, 0, 0, 0, 0},
			},
		},
		"model": req.Model,
		"usage": map[string]int{
			"prompt_tokens": 0,
			"total_tokens":  0,
		},
	})
}
