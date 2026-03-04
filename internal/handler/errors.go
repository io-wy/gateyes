package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const (
	TypeInvalidRequest      = "invalid_request_error"
	TypeAuthenticationError = "authentication_error"
	TypePermissionError     = "permission_error"
	TypeRateLimitError      = "rate_limit_error"
	TypeUpstreamError       = "upstream_error"
	TypeServiceUnavailable  = "service_unavailable"
	TypeWaitPlan            = "wait_plan"
	TypeInternalError       = "internal_error"
)

type errorEnvelope struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

func WriteError(w http.ResponseWriter, status int, message, errorType, code string) {
	WriteJSON(w, status, errorEnvelope{
		Error: apiError{
			Message: message,
			Type:    errorType,
			Code:    code,
		},
	})
}

func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func DecodeJSONStrict(r *http.Request, dst any) error {
	defer r.Body.Close()

	const maxBodySize = 2 << 20
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBodySize))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("decode request json: %w", err)
	}

	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("invalid trailing json: %w", err)
	}
	if trailing != nil {
		return errors.New("request body must contain a single json object")
	}
	return nil
}
