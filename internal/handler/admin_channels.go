package handler

import (
	"fmt"
	"net/http"
	"time"
)

type AdminHandlers struct{}

func NewAdminHandlers() *AdminHandlers {
	return &AdminHandlers{}
}

type createChannelRequest struct {
	Type           string   `json:"type"`
	Key            string   `json:"key"`
	BaseURL        string   `json:"base_url"`
	Models         []string `json:"models"`
	ModelMapping   []string `json:"model_mapping"`
	Priority       int      `json:"priority"`
	Weight         int      `json:"weight"`
	MaxConcurrency int      `json:"max_concurrency"`
	Enabled        bool     `json:"enabled"`
	ResponseFormat string   `json:"response_format"`
}

func (h *AdminHandlers) CreateChannel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed", TypeInvalidRequest, "")
		return
	}

	var req createChannelRequest
	if err := DecodeJSONStrict(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error(), TypeInvalidRequest, "")
		return
	}
	if req.Type == "" {
		WriteError(w, http.StatusBadRequest, "type is required", TypeInvalidRequest, "")
		return
	}
	if req.Key == "" {
		WriteError(w, http.StatusBadRequest, "key is required", TypeInvalidRequest, "")
		return
	}

	channelID := fmt.Sprintf("ch_%d", time.Now().UnixNano())

	// TODO(io): persist channel with storage layer and redact sensitive fields at query time.
	WriteJSON(w, http.StatusCreated, map[string]any{
		"id":              channelID,
		"type":            req.Type,
		"base_url":        req.BaseURL,
		"models":          req.Models,
		"model_mapping":   req.ModelMapping,
		"priority":        req.Priority,
		"weight":          req.Weight,
		"max_concurrency": req.MaxConcurrency,
		"enabled":         req.Enabled,
		"response_format": req.ResponseFormat,
		"created_at":      time.Now().UTC().Format(time.RFC3339),
		"key_masked":      maskSecret(req.Key),
	})
}

func maskSecret(secret string) string {
	if len(secret) <= 6 {
		return "******"
	}
	return secret[:3] + "..." + secret[len(secret)-3:]
}
