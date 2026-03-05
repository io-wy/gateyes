package handler

import (
	"net/http"

	"gateyes/internal/service"
)

type HealthHandler struct {
	service *service.HealthService
}

func NewHealthHandler(healthService *service.HealthService) *HealthHandler {
	return &HealthHandler{service: healthService}
}

func (h *HealthHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "method not allowed", TypeInvalidRequest, "")
		return
	}

	WriteJSON(w, http.StatusOK, h.service.Status(r.Context()))
}
