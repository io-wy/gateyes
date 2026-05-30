package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/alert"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/provider"
)

func detachedPersistenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.WithoutCancel(ctx)
	return context.WithTimeout(base, terminalPersistenceTimeout)
}

// publishOrInline runs the given handler on the eventBus when configured.
//
// Three modes:
//   - eventBus configured + Publish accepts → async on bus worker pool
//   - eventBus configured + Publish drops (channel full) → spawn a detached
//     goroutine. We never drop billing-relevant work; the bus just shapes
//     back-pressure when the worker pool is saturated.
//   - eventBus == nil → run inline synchronously. This preserves
//     deterministic semantics for tests that read the DB right after
//     Create returns. Production must wire an event bus to get the
//     hot-path win.
//
// The handler always runs with a freshly detached context (independent of
// the caller's request ctx); cancellation of the request does not cancel
// the bookkeeping work.
func (s *Service) publishOrInline(h func(ctx context.Context)) {
	if h == nil {
		return
	}
	if s.eventBus != nil {
		if s.eventBus.Publish(h) {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), terminalPersistenceTimeout)
			defer cancel()
			h(ctx)
		}()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), terminalPersistenceTimeout)
	defer cancel()
	h(ctx)
}

func (s *Service) persistSuccess(ctx context.Context, identity *repository.AuthIdentity, exec *execution, resp *provider.Response, latencyMs int64) error {
	providerName := exec.provider.Name()
	model := exec.requestedModel
	cost := s.computeCost(exec.provider, exec.requestedModel, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	if err := s.ensureQuotaAvailable(ctx, identity, exec, resp, latencyMs, providerName, cost); err != nil {
		return err
	}

	if err := s.auth.RecordUsage(
		ctx,
		identity,
		providerName,
		model,
		resp.Usage.PromptTokens,
		resp.Usage.CompletionTokens,
		resp.Usage.TotalTokens,
		cost,
		latencyMs,
		"success",
		"",
	); err != nil {
		if errors.Is(err, auth.ErrQuotaExceeded) {
			_ = s.recordQuotaExceeded(ctx, identity, exec, resp, latencyMs, providerName, cost)
		}
		if errors.Is(err, auth.ErrBudgetExceeded) {
			_ = s.recordBudgetExceeded(ctx, identity, exec, resp, latencyMs, providerName, cost)
		}
		return err
	}

	s.providerMgr.Stats.RecordRequest(providerName, true, resp.Usage.TotalTokens, latencyMs)

	finalizeRouteTrace(exec.routeTrace, providerName, "success", nil)
	body, _ := json.Marshal(resp)
	traceBytes := routeTraceBytes(exec.routeTrace)
	responseID := exec.responseID
	tenantID := identity.TenantID
	projectID := identity.ProjectID
	apiKeyID := identity.APIKeyID
	virtualKeyID := identity.VirtualKeyID
	totalTokens := resp.Usage.TotalTokens
	promptTokens := resp.Usage.PromptTokens
	completionTokens := resp.Usage.CompletionTokens
	callbackURL := identity.CallbackURL
	identitySnap := *identity
	alertSvc := s.alert

	s.publishOrInline(func(asyncCtx context.Context) {
		_ = s.store.UpdateResponse(asyncCtx, repository.ResponseRecord{
			ID:             responseID,
			TenantID:       tenantID,
			ProjectID:      projectID,
			ProviderName:   providerName,
			Model:          model,
			Status:         "completed",
			ResponseBody:   body,
			RouteTraceBody: traceBytes,
		})

		if alertSvc != nil {
			alertSvc.CheckQuotaUsage(asyncCtx, &identitySnap)
			alertSvc.NotifyRequestEvent(asyncCtx, map[string]any{
				"tenant_id":      tenantID,
				"project_id":     projectID,
				"api_key_id":     apiKeyID,
				"provider_name":  providerName,
				"model":          model,
				"status":         "success",
				"latency_ms":     latencyMs,
				"total_tokens":   totalTokens,
				"total_cost_usd": cost,
			})
		}

		if callbackURL != "" {
			s.sendCallback(callbackURL, map[string]any{
				"event":          "request.completed",
				"response_id":    responseID,
				"tenant_id":      tenantID,
				"api_key_id":     apiKeyID,
				"virtual_key_id": virtualKeyID,
				"provider_name":  providerName,
				"model":          model,
				"status":         "success",
				"latency_ms":     latencyMs,
				"cost_usd":       cost,
				"usage": map[string]any{
					"prompt_tokens":     promptTokens,
					"completion_tokens": completionTokens,
					"total_tokens":      totalTokens,
				},
			})
		}
	})

	return nil
}

func (s *Service) ensureQuotaAvailable(ctx context.Context, identity *repository.AuthIdentity, exec *execution, resp *provider.Response, latencyMs int64, providerName string, cost float64) error {
	if s.auth.HasQuota(identity, resp.Usage.TotalTokens) {
		return nil
	}
	return s.recordQuotaExceeded(ctx, identity, exec, resp, latencyMs, providerName, cost)
}

func (s *Service) recordQuotaExceeded(ctx context.Context, identity *repository.AuthIdentity, exec *execution, resp *provider.Response, latencyMs int64, providerName string, cost float64) error {
	finalizeRouteTrace(exec.routeTrace, providerName, "quota_exceeded", auth.ErrQuotaExceeded)
	_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
		ID:             exec.responseID,
		TenantID:       identity.TenantID,
		ProjectID:      identity.ProjectID,
		ProviderName:   providerName,
		Model:          exec.requestedModel,
		Status:         "error",
		ResponseBody:   nil,
		RouteTraceBody: routeTraceBytes(exec.routeTrace),
	})
	if s.alert != nil {
		s.alert.NotifyBudgetExhausted(ctx, alert.BudgetExhausted{
			TenantID:     identity.TenantID,
			ProjectID:    identity.ProjectID,
			APIKeyID:     identity.APIKeyID,
			ProviderName: providerName,
			Model:        exec.requestedModel,
			CostUSD:      cost,
			BudgetScope:  "quota",
		})
	}
	_ = s.auth.RecordUsage(
		ctx,
		identity,
		providerName,
		exec.requestedModel,
		resp.Usage.PromptTokens,
		resp.Usage.CompletionTokens,
		resp.Usage.TotalTokens,
		cost,
		latencyMs,
		"error",
		"quota_exceeded",
	)
	return auth.ErrQuotaExceeded
}

func (s *Service) recordBudgetExceeded(ctx context.Context, identity *repository.AuthIdentity, exec *execution, resp *provider.Response, latencyMs int64, providerName string, cost float64) error {
	var body []byte
	if resp != nil {
		body, _ = json.Marshal(resp)
	}
	finalizeRouteTrace(exec.routeTrace, providerName, "budget_exceeded", auth.ErrBudgetExceeded)
	_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
		ID:             exec.responseID,
		TenantID:       identity.TenantID,
		ProjectID:      identity.ProjectID,
		ProviderName:   providerName,
		Model:          exec.requestedModel,
		Status:         "error",
		ResponseBody:   body,
		RouteTraceBody: routeTraceBytes(exec.routeTrace),
	})
	if s.alert != nil {
		scope := "api_key"
		if identity.ProjectID != "" {
			scope = "project_or_api_key"
		}
		s.alert.NotifyBudgetExhausted(ctx, alert.BudgetExhausted{
			TenantID:     identity.TenantID,
			ProjectID:    identity.ProjectID,
			APIKeyID:     identity.APIKeyID,
			ProviderName: providerName,
			Model:        exec.requestedModel,
			CostUSD:      cost,
			BudgetScope:  scope,
		})
		s.alert.NotifyErrorEvent(ctx, map[string]any{
			"tenant_id":     identity.TenantID,
			"project_id":    identity.ProjectID,
			"api_key_id":    identity.APIKeyID,
			"provider_name": providerName,
			"model":         exec.requestedModel,
			"status":        "budget_exceeded",
			"latency_ms":    latencyMs,
		})
	}
	return auth.ErrBudgetExceeded
}

func (s *Service) markError(ctx context.Context, identity *repository.AuthIdentity, exec *execution, latencyMs int64) error {
	finalizeRouteTrace(exec.routeTrace, exec.provider.Name(), "error", nil)
	_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
		ID:             exec.responseID,
		TenantID:       identity.TenantID,
		ProjectID:      identity.ProjectID,
		ProviderName:   exec.provider.Name(),
		Model:          exec.requestedModel,
		Status:         "error",
		RouteTraceBody: routeTraceBytes(exec.routeTrace),
	})
	if s.alert != nil {
		s.alert.NotifyErrorEvent(ctx, map[string]any{
			"tenant_id":     identity.TenantID,
			"project_id":    identity.ProjectID,
			"api_key_id":    identity.APIKeyID,
			"provider_name": exec.provider.Name(),
			"model":         exec.requestedModel,
			"status":        "upstream_error",
			"latency_ms":    latencyMs,
		})
	}
	return s.auth.RecordUsage(ctx, identity, exec.provider.Name(), exec.requestedModel, 0, 0, 0, 0, latencyMs, "error", "upstream_error")
}

func (s *Service) markErrorWithProvider(ctx context.Context, identity *repository.AuthIdentity, exec *execution, latencyMs int64, providerName string) error {
	finalizeRouteTrace(exec.routeTrace, providerName, "error", nil)
	_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
		ID:             exec.responseID,
		TenantID:       identity.TenantID,
		ProjectID:      identity.ProjectID,
		ProviderName:   providerName,
		Model:          exec.requestedModel,
		Status:         "error",
		RouteTraceBody: routeTraceBytes(exec.routeTrace),
	})
	if s.alert != nil {
		s.alert.NotifyErrorEvent(ctx, map[string]any{
			"tenant_id":     identity.TenantID,
			"project_id":    identity.ProjectID,
			"api_key_id":    identity.APIKeyID,
			"provider_name": providerName,
			"model":         exec.requestedModel,
			"status":        "upstream_error",
			"latency_ms":    latencyMs,
		})
	}
	return s.auth.RecordUsage(ctx, identity, providerName, exec.requestedModel, 0, 0, 0, 0, latencyMs, "error", "upstream_error")
}

func (s *Service) recordOutputBudgetError(ctx context.Context, identity *repository.AuthIdentity, exec *execution, resp *provider.Response, latencyMs int64, providerName string) error {
	var body []byte
	if resp != nil {
		body, _ = json.Marshal(resp)
	}
	finalizeRouteTrace(exec.routeTrace, providerName, "output_budget_too_low", ErrOutputBudgetTooLow)
	_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
		ID:             exec.responseID,
		TenantID:       identity.TenantID,
		ProjectID:      identity.ProjectID,
		ProviderName:   providerName,
		Model:          exec.requestedModel,
		Status:         "error",
		ResponseBody:   body,
		RouteTraceBody: routeTraceBytes(exec.routeTrace),
	})
	if s.alert != nil {
		s.alert.NotifyErrorEvent(ctx, map[string]any{
			"tenant_id":     identity.TenantID,
			"project_id":    identity.ProjectID,
			"api_key_id":    identity.APIKeyID,
			"provider_name": providerName,
			"model":         exec.requestedModel,
			"status":        "output_budget_too_low",
			"latency_ms":    latencyMs,
		})
	}
	if resp == nil {
		return s.auth.RecordUsage(ctx, identity, providerName, exec.requestedModel, 0, 0, 0, 0, latencyMs, "error", "output_budget_too_low")
	}
	cost := s.computeCost(exec.provider, exec.requestedModel, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	return s.auth.RecordUsage(
		ctx,
		identity,
		providerName,
		exec.requestedModel,
		resp.Usage.PromptTokens,
		resp.Usage.CompletionTokens,
		resp.Usage.TotalTokens,
		cost,
		latencyMs,
		"error",
		"output_budget_too_low",
	)
}

func (s *Service) handleStreamError(ctx context.Context, identity *repository.AuthIdentity, responseID, providerName, model string, latencyMs int64, streamErr error) {
	persistCtx, cancel := detachedPersistenceContext(ctx)
	defer cancel()

	_ = s.store.UpdateResponse(persistCtx, repository.ResponseRecord{
		ID:           responseID,
		TenantID:     identity.TenantID,
		ProjectID:    identity.ProjectID,
		ProviderName: providerName,
		Model:        model,
		Status:       "error",
	})
	_ = s.auth.RecordUsage(persistCtx, identity, providerName, model, 0, 0, 0, 0, latencyMs, "error", "upstream_error")
	if s.alert != nil {
		s.alert.NotifyErrorEvent(persistCtx, map[string]any{
			"tenant_id":     identity.TenantID,
			"project_id":    identity.ProjectID,
			"api_key_id":    identity.APIKeyID,
			"provider_name": providerName,
			"model":         model,
			"status":        "upstream_error",
			"latency_ms":    latencyMs,
			"error":         errorString(streamErr),
		})
	}
}

func (s *Service) finalizeStream(ctx context.Context, identity *repository.AuthIdentity, responseID, providerName, model string, resp *provider.Response, latencyMs int64, trace *routeTrace, out chan<- provider.ResponseEvent, emitOutputs bool) {
	if resp == nil {
		resp = provider.NewTextResponse(responseID, model, "", provider.Usage{})
	}
	resp.ID = responseID
	resp.Model = model
	resp.Created = time.Now().Unix()
	resp.Status = "completed"
	finalizeRouteTrace(trace, providerName, "success", nil)

	persistCtx, cancel := detachedPersistenceContext(ctx)
	defer cancel()

	body, _ := json.Marshal(resp)
	_ = s.store.UpdateResponse(persistCtx, repository.ResponseRecord{
		ID:             responseID,
		TenantID:       identity.TenantID,
		ProjectID:      identity.ProjectID,
		ProviderName:   providerName,
		Model:          model,
		Status:         "completed",
		ResponseBody:   body,
		RouteTraceBody: routeTraceBytes(trace),
	})

	_ = s.auth.RecordUsage(persistCtx, identity, providerName, model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens, 0, latencyMs, "success", "")
	if s.alert != nil {
		s.alert.CheckQuotaUsage(persistCtx, identity)
		s.alert.NotifyRequestEvent(persistCtx, map[string]any{
			"tenant_id":      identity.TenantID,
			"project_id":     identity.ProjectID,
			"api_key_id":     identity.APIKeyID,
			"provider_name":  providerName,
			"model":          model,
			"status":         "success",
			"latency_ms":     latencyMs,
			"total_tokens":   resp.Usage.TotalTokens,
			"total_cost_usd": 0,
		})
	}

	s.providerMgr.Stats.RecordRequest(providerName, true, resp.Usage.TotalTokens, latencyMs)

	if identity.CallbackURL != "" {
		go s.sendCallback(identity.CallbackURL, map[string]any{
			"event":          "request.completed",
			"response_id":    responseID,
			"tenant_id":      identity.TenantID,
			"api_key_id":     identity.APIKeyID,
			"virtual_key_id": identity.VirtualKeyID,
			"provider_name":  providerName,
			"model":          model,
			"status":         "success",
			"latency_ms":     latencyMs,
			"usage": map[string]any{
				"prompt_tokens":     resp.Usage.PromptTokens,
				"completion_tokens": resp.Usage.CompletionTokens,
				"total_tokens":      resp.Usage.TotalTokens,
			},
		})
	}

	if emitOutputs {
		s.emitStreamPayloadFromResponse(out, resp)
	}
	out <- provider.ResponseEvent{Type: provider.EventResponseCompleted, Response: resp}
}

func (s *Service) handleStreamCancellation(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, responseID string, currentProvider provider.Provider, trace *routeTrace, finalResponse *provider.Response, assistantText string, streamedOutputs []provider.ResponseOutput, streamUsage *provider.Usage, startedAt time.Time) {
	if identity == nil || req == nil || currentProvider == nil {
		return
	}

	exec := &execution{
		provider:              currentProvider,
		requestedModel:        req.Model,
		upstreamRequest:       buildUpstreamRequest(req),
		responseID:            responseID,
		tenantID:              identity.TenantID,
		routeTrace:            trace,
		startedAt:             startedAt,
		estimatedPromptTokens: req.EstimatePromptTokens(),
	}

	resp := finalResponse
	if resp == nil || !hasRenderableStreamPayload(resp) {
		resp = buildAccumulatedStreamResponse(responseID, req.Model, assistantText, streamedOutputs, exec.estimatedPromptTokens)
	}
	applyRecoveredStreamUsage(resp, streamUsage)
	resp = s.normalizeResponse(exec, resp)
	resp.Status = "cancelled"
	finalizeRouteTrace(trace, currentProvider.Name(), "cancelled", context.Canceled)

	latencyMs := time.Since(startedAt).Milliseconds()
	cost := s.computeCost(currentProvider, req.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)

	persistCtx, cancel := detachedPersistenceContext(ctx)
	defer cancel()

	body, _ := json.Marshal(resp)
	_ = s.store.UpdateResponse(persistCtx, repository.ResponseRecord{
		ID:             responseID,
		TenantID:       identity.TenantID,
		ProjectID:      identity.ProjectID,
		ProviderName:   currentProvider.Name(),
		Model:          req.Model,
		Status:         "cancelled",
		ResponseBody:   body,
		RouteTraceBody: routeTraceBytes(trace),
	})
	_ = s.auth.RecordBillableUsage(persistCtx, identity, currentProvider.Name(), req.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens, cost, latencyMs, "cancelled", "client_disconnect")
	s.providerMgr.Stats.RecordRequest(currentProvider.Name(), false, resp.Usage.TotalTokens, latencyMs)
}

func (s *Service) sendCallback(url string, payload map[string]any) {
	if url == "" {
		return
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}
