package responses

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/guardrail"
	"github.com/gateyes/gateway/internal/service/provider"
	"github.com/gateyes/gateway/internal/service/router"
)

func buildUpstreamRequest(req *provider.ResponseRequest) *provider.ResponseRequest {
	messages := req.InputMessages()
	return &provider.ResponseRequest{
		Model:             req.Model,
		PreferredProvider: req.PreferredProvider,
		Surface:           req.Surface,
		Input:             messages,
		Messages:          messages,
		Stream:            req.Stream,
		MaxOutputTokens:   req.MaxOutputTokens,
		MaxTokens:         req.MaxTokens,
		Tools:             req.Tools,
		OutputFormat:      cloneOutputFormat(req.OutputFormat),
		Options:           provider.CloneRequestOptions(req.Options),
	}
}

func cloneOutputFormat(value *provider.OutputFormat) *provider.OutputFormat {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Schema = cloneStringAnyMap(value.Schema)
	cloned.Raw = cloneStringAnyMap(value.Raw)
	return &cloned
}

func cloneStringAnyMap(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		cloned := make(map[string]any, len(value))
		for key, item := range value {
			cloned[key] = item
		}
		return cloned
	}
	var cloned map[string]any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		fallback := make(map[string]any, len(value))
		for key, item := range value {
			fallback[key] = item
		}
		return fallback
	}
	return cloned
}

func (s *Service) Create(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*CreateResult, error) {
	req.Normalize()
	createStart := time.Now()

	// Run pre-call guardrails before any cache lookup or provider call.
	if s.guardrails != nil {
		pre := s.guardrails.PreCall(ctx, req)
		if pre.Verdict == guardrail.Block {
			return nil, fmt.Errorf("%w: %s", ErrGuardrailBlocked, pre.Reason)
		}
		if pre.Verdict == guardrail.Transform && pre.Request != nil {
			req = pre.Request
		}
	}

	// L1 cache fast path
	if entry, hit := s.lookupCache(ctx, identity, req); hit {
		var resp provider.Response
		if err := json.Unmarshal(entry.Response, &resp); err == nil {
			return &CreateResult{
				Response:         &resp,
				ProviderName:     entry.Provider,
				LatencyMs:        time.Since(createStart).Milliseconds(),
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: resp.Usage.CompletionTokens,
				Retries:          0,
				Fallback:         0,
			}, nil
		}
	}

	candidates, trace := s.planCandidates(ctx, identity, sessionID, req)
	if len(candidates) == 0 {
		return nil, ErrNoProvider
	}

	firstProvider := candidates[0]
	responseID := uuid.NewString()
	if trace != nil {
		trace.ResponseID = responseID
		trace.touch()
	}
	requestBody, _ := json.Marshal(req)
	if err := s.store.CreateResponse(ctx, repository.ResponseRecord{
		ID:             responseID,
		TenantID:       identity.TenantID,
		ProjectID:      identity.ProjectID,
		UserID:         identity.UserID,
		APIKeyID:       identity.APIKeyID,
		ProviderName:   firstProvider.Name(),
		Model:          req.Model,
		Status:         "in_progress",
		RequestBody:    requestBody,
		RouteTraceBody: routeTraceBytes(trace),
	}); err != nil {
		return nil, err
	}

	var lastErr error
	var totalRetries int
	fallbackCount := 0

	for _, p := range candidates {
		tenantID := identity.TenantID
		providerName := p.Name()

		if s.limiter != nil && !s.limiter.CheckProvider(providerName, req.EstimateAdmissionTokens()) {
			appendRouteAttempt(trace, providerName, 0, "rate_limited", fmt.Errorf("provider rate limited"))
			continue
		}

		if s.circuitBreaker != nil && !s.circuitBreaker.IsAvailable(tenantID, providerName) {
			continue
		}

		if fallbackCount > 0 {
			_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
				ID:           responseID,
				TenantID:     tenantID,
				ProjectID:    identity.ProjectID,
				ProviderName: providerName,
				Model:        req.Model,
				Status:       "in_progress",
			})
		}

		exec := &execution{
			provider:              p,
			requestedModel:        req.Model,
			upstreamRequest:       buildUpstreamRequest(req),
			responseID:            responseID,
			tenantID:              tenantID,
			requestBody:           requestBody,
			routeTrace:            trace,
			startedAt:             time.Now(),
			estimatedPromptTokens: req.EstimatePromptTokens(),
		}

		s.providerMgr.Stats.IncrementLoad(providerName)

		resp, retries, err := s.callWithRetrySF(ctx, identity, exec, identity.TenantID, req)
		totalRetries += retries
		latencyMs := time.Since(exec.startedAt).Milliseconds()

		if err != nil {
			appendRouteAttempt(exec.routeTrace, providerName, retries, "error", err)
			s.providerMgr.Stats.DecrementLoad(providerName)
			if s.circuitBreaker != nil {
				s.circuitBreaker.RecordFailure(tenantID, providerName)
			}
			s.providerMgr.Stats.RecordRequest(providerName, false, 0, latencyMs)
			_ = s.markErrorWithProvider(ctx, identity, exec, latencyMs, providerName)
			lastErr = err
			fallbackCount++
			continue
		}

		s.providerMgr.Stats.DecrementLoad(providerName)
		if s.circuitBreaker != nil {
			s.circuitBreaker.RecordSuccess(tenantID, providerName)
		}

		resp = s.normalizeResponse(exec, resp)
		if budgetErr := validateVisibleOutputBudget(exec, resp); budgetErr != nil {
			appendRouteAttempt(exec.routeTrace, providerName, retries, "budget_rejected", budgetErr)
			if s.circuitBreaker != nil {
				s.circuitBreaker.RecordSuccess(tenantID, providerName)
			}
			_ = s.recordOutputBudgetError(ctx, identity, exec, resp, latencyMs, providerName)
			s.providerMgr.Stats.RecordRequest(providerName, true, resp.Usage.TotalTokens, latencyMs)
			return nil, budgetErr
		}
		appendRouteAttempt(exec.routeTrace, providerName, retries, "success", nil)

		if s.guardrails != nil {
			post := s.guardrails.PostCall(ctx, resp)
			if post.Verdict == guardrail.Block {
				_ = s.persistSuccess(ctx, identity, exec, resp, latencyMs)
				return nil, fmt.Errorf("%w: %s", ErrGuardrailBlocked, post.Reason)
			}
			if post.Verdict == guardrail.Transform && post.Response != nil {
				resp = post.Response
			}
		}

		if err := s.persistSuccess(ctx, identity, exec, resp, latencyMs); err != nil {
			return nil, err
		}
		body, _ := json.Marshal(resp)
		s.writeCache(ctx, identity, req, &cache.Entry{
			Response:  body,
			Model:     req.Model,
			Provider:  providerName,
			Usage: cache.Usage{
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: resp.Usage.CompletionTokens,
				TotalTokens:      resp.Usage.TotalTokens,
				CachedTokens:     resp.Usage.CachedTokens,
			},
			CreatedAt: time.Now().Unix(),
		})
		if s.router != nil {
			s.router.PromoteAffinity(router.RouteContext{
				Model:        req.Model,
				SessionID:    sessionID,
				InputText:    req.InputText(),
				PromptTokens: req.EstimatePromptTokens(),
				Stream:       req.Stream,
				HasTools:     len(req.Tools) > 0,
			}, providerName)
		}

		return &CreateResult{
			Response:         resp,
			ProviderName:     providerName,
			LatencyMs:        latencyMs,
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			Retries:          totalRetries,
			Fallback:         fallbackCount,
		}, nil
	}

	if lastErr == nil {
		return nil, ErrNoProvider
	}
	return nil, lastErr
}
