package handler

import (
	"io"
	"net/http"

	"gateyes/internal/pkg/apperror"
	"gateyes/internal/requestctx"
	"gateyes/internal/service"
)

type GatewayHandlers struct {
	service *service.GatewayService
}

func NewGatewayHandlers(gatewayService *service.GatewayService) *GatewayHandlers {
	return &GatewayHandlers{
		service: gatewayService,
	}
}

func (h *GatewayHandlers) Models(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed", TypeInvalidRequest, "")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   h.service.ListModels(r.Context()),
	})
}

type chatCompletionRequest struct {
	Model    string                `json:"model"`
	Messages []service.ChatMessage `json:"messages"`
	Stream   bool                  `json:"stream"`
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

	result, release, err := h.service.ChatCompletions(r.Context(), service.ChatCompletionInput{
		Model:     req.Model,
		Messages:  req.Messages,
		Stream:    req.Stream,
		TokenID:   requestctx.TokenID(r.Context()),
		SessionID: requestctx.SessionID(r.Context()),
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	defer release()

	if result.Stream {
		h.writeStreamPlaceholder(w, result.Model)
		return
	}

	WriteJSON(w, http.StatusOK, result)
}

func (h *GatewayHandlers) writeStreamPlaceholder(w http.ResponseWriter, model string) {
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

	result, release, err := h.service.Embeddings(r.Context(), service.EmbeddingsInput{
		Model:     req.Model,
		Input:     req.Input,
		TokenID:   requestctx.TokenID(r.Context()),
		SessionID: requestctx.SessionID(r.Context()),
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	defer release()

	WriteJSON(w, http.StatusOK, result)
}

func (h *GatewayHandlers) writeServiceError(w http.ResponseWriter, err error) {
	switch apperror.CodeOf(err) {
	case apperror.CodeInvalidArgument:
		WriteError(w, http.StatusBadRequest, err.Error(), TypeInvalidRequest, "")
	case apperror.CodeNoAvailableChannel:
		WriteError(w, http.StatusServiceUnavailable, "no available channel", TypeServiceUnavailable, "")
	case apperror.CodeRateLimited:
		WriteError(w, http.StatusTooManyRequests, err.Error(), TypeRateLimitError, "")
	default:
		WriteError(w, http.StatusInternalServerError, "internal service error", TypeInternalError, "")
	}
}
