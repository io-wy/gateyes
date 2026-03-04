package handler

import (
	"net/http"
	"time"
)

type HealthHandler struct {
	buildInfo BuildInfo
}

func NewHealthHandler(buildInfo BuildInfo) *HealthHandler {
	return &HealthHandler{buildInfo: buildInfo}
}

func (h *HealthHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed", TypeInvalidRequest, "")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
		"build":  h.buildInfo,
	})
}
