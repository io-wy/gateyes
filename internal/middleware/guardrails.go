package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"gateyes/internal/guardrails"
)

// GuardrailsMiddleware provides security and safety checks for Agent requests
type GuardrailsMiddleware struct {
	guardrails *guardrails.Guardrails
	config     guardrails.GuardrailsConfig
}

// NewGuardrailsMiddleware creates a new guardrails middleware
func NewGuardrailsMiddleware(config guardrails.GuardrailsConfig) (*GuardrailsMiddleware, error) {
	if !config.Enabled {
		return &GuardrailsMiddleware{
			config: config,
		}, nil
	}

	g, err := guardrails.NewGuardrails(config)
	if err != nil {
		return nil, err
	}

	return &GuardrailsMiddleware{
		guardrails: g,
		config:     config,
	}, nil
}

// Middleware returns the HTTP middleware handler
func (gm *GuardrailsMiddleware) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !gm.config.Enabled || gm.guardrails == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Read request body
			body, err := io.ReadAll(r.Body)
			if err != nil {
				slog.Error("failed to read request body", "error", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			r.Body = io.NopCloser(bytes.NewBuffer(body))

			// Extract content from request
			content := string(body)
			metadata := map[string]interface{}{
				"method": r.Method,
				"path":   r.URL.Path,
				"user":   r.Header.Get("Authorization"),
			}

			// Check request
			ctx := r.Context()
			result, err := gm.guardrails.CheckRequest(ctx, content, metadata)
			if err != nil {
				slog.Error("guardrails check failed", "error", err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}

			// Handle result
			switch result.Action {
			case "block":
				slog.Warn("request blocked by guardrails",
					"violations", len(result.Violations),
					"message", result.Message,
				)
				http.Error(w, result.Message, http.StatusForbidden)
				return

			case "redact":
				// Replace request body with redacted content
				r.Body = io.NopCloser(bytes.NewBufferString(result.Redacted))
				slog.Info("request content redacted",
					"violations", len(result.Violations),
				)

			case "warn":
				slog.Warn("guardrails warning",
					"violations", len(result.Violations),
					"message", result.Message,
				)
				// Continue processing but log warning
			}

			// Wrap response writer to check response
			rw := &guardrailsResponseWriter{
				ResponseWriter: w,
				guardrails:     gm.guardrails,
				ctx:            ctx,
			}

			next.ServeHTTP(rw, r)
		})
	}
}

// guardrailsResponseWriter wraps http.ResponseWriter to check responses
type guardrailsResponseWriter struct {
	http.ResponseWriter
	guardrails *guardrails.Guardrails
	ctx        context.Context
	body       bytes.Buffer
	statusCode int
}

func (rw *guardrailsResponseWriter) Write(b []byte) (int, error) {
	// Capture response body
	rw.body.Write(b)

	// Check response with guardrails
	result, err := rw.guardrails.CheckResponse(rw.ctx, rw.body.String(), nil)
	if err != nil {
		slog.Error("response guardrails check failed", "error", err)
		return rw.ResponseWriter.Write(b)
	}

	// Handle result
	switch result.Action {
	case "block":
		slog.Warn("response blocked by guardrails",
			"violations", len(result.Violations),
		)
		// Return error response instead
		errorResponse := map[string]interface{}{
			"error": result.Message,
		}
		errorBody, _ := json.Marshal(errorResponse)
		rw.ResponseWriter.Header().Set("Content-Type", "application/json")
		rw.ResponseWriter.WriteHeader(http.StatusForbidden)
		return rw.ResponseWriter.Write(errorBody)

	case "redact":
		slog.Info("response content redacted",
			"violations", len(result.Violations),
		)
		// Write redacted content instead
		return rw.ResponseWriter.Write([]byte(result.Redacted))

	case "warn":
		slog.Warn("response guardrails warning",
			"violations", len(result.Violations),
		)
	}

	return rw.ResponseWriter.Write(b)
}

func (rw *guardrailsResponseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}
