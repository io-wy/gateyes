package responses

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/provider"
	"github.com/gateyes/gateway/internal/service/router"
)

// drainStreamForUsage continues reading from a provider's stream channel
// after the caller's ctx has been cancelled, so the gateway can capture
// the final usage chunk that providers typically emit at end-of-stream.
//
// The drain stops on stream close, upstream error, or after
// streamCancelDrainTimeout — whichever comes first.
//
// Mutates the supplied accumulators in place, mirroring what the main
// stream loop does for non-cancelled events. Visible payload (out chan)
// is intentionally NOT updated — the client is gone.
func (s *Service) drainStreamForUsage(
	stream <-chan provider.ResponseEvent,
	upstreamErrCh <-chan error,
	finalResponse **provider.Response,
	streamUsage **provider.Usage,
	streamedOutputs *[]provider.ResponseOutput,
	assistantText *string,
) {
	deadline := time.NewTimer(streamCancelDrainTimeout)
	defer deadline.Stop()
	quiet := time.NewTimer(streamCancelDrainQuiet)
	defer quiet.Stop()
	for {
		select {
		case event, ok := <-stream:
			if !ok {
				return
			}
			switch event.Type {
			case provider.EventContentDelta:
				if event.Usage != nil {
					cp := *event.Usage
					*streamUsage = &cp
				}
				if len(event.ToolCalls) > 0 {
					*streamedOutputs = appendStreamedToolCalls(*streamedOutputs, event.ToolCalls)
				}
				if isRenderableStreamEvent(event) {
					*assistantText += event.Text()
				}
			case provider.EventToolCallDone:
				*streamedOutputs = appendStreamOutput(*streamedOutputs, event.Output)
			case provider.EventResponseCompleted:
				if event.Response != nil {
					*finalResponse = event.Response
				}
			}
			if !quiet.Stop() {
				select {
				case <-quiet.C:
				default:
				}
			}
			quiet.Reset(streamCancelDrainQuiet)
		case <-upstreamErrCh:
			return
		case <-quiet.C:
			return
		case <-deadline.C:
			return
		}
	}
}

func (s *Service) isStreamRetryable(err error) bool {
	return isRetryable(err)
}

func (s *Service) CreateStream(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*Stream, error) {
	req.Normalize()

	candidates, trace := s.planCandidates(ctx, identity, sessionID, req)
	if len(candidates) == 0 {
		return nil, ErrNoProvider
	}

	events := make(chan provider.ResponseEvent)
	errCh := make(chan error, 1)

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
		ProviderName:   "",
		Model:          req.Model,
		Status:         "in_progress",
		RequestBody:    requestBody,
		RouteTraceBody: routeTraceBytes(trace),
	}); err != nil {
		return nil, err
	}

	go s.runStreamWithFallback(ctx, identity, req, sessionID, candidates, responseID, trace, events, errCh)

	firstProviderName := ""
	if len(candidates) > 0 {
		firstProviderName = candidates[0].Name()
	}
	return &Stream{
		ResponseID:   responseID,
		ProviderName: firstProviderName,
		StartedAt:    time.Now(),
		Events:       events,
		Errors:       errCh,
	}, nil
}

func (s *Service) runStreamWithFallback(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string, candidates []provider.Provider, responseID string, trace *routeTrace, out chan<- provider.ResponseEvent, errCh chan<- error) {
	defer close(out)
	defer close(errCh)

	if entry, hit := s.lookupCache(ctx, identity, req); hit {
		s.replayCachedStream(ctx, identity, req, entry, responseID, out, errCh)
		return
	}

	tenantID := identity.TenantID
	retryCfg := s.cfg.Retry
	startedAt := time.Now()
	firstResponseSent := false
	hasSentPayload := false

	for _, p := range candidates {
		providerName := p.Name()

		if s.limiter != nil && !s.limiter.CheckProvider(providerName, req.EstimateAdmissionTokens()) {
			appendRouteAttempt(trace, providerName, 0, "rate_limited", fmt.Errorf("provider rate limited"))
			continue
		}

		if s.circuitBreaker != nil && !s.circuitBreaker.IsAvailable(tenantID, providerName) {
			continue
		}

		s.providerMgr.Stats.IncrementLoad(providerName)

		_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
			ID:             responseID,
			TenantID:       tenantID,
			ProjectID:      identity.ProjectID,
			ProviderName:   providerName,
			Model:          req.Model,
			Status:         "in_progress",
			RouteTraceBody: routeTraceBytes(trace),
		})

		if !firstResponseSent {
			out <- provider.ResponseEvent{
				Type: provider.EventResponseStarted,
				Response: &provider.Response{
					ID:      responseID,
					Object:  "response",
					Created: startedAt.Unix(),
					Model:   req.Model,
					Status:  "in_progress",
				},
			}
			firstResponseSent = true
		}

		upstreamReq := &provider.ResponseRequest{
			Model:           req.Model,
			Surface:         req.Surface,
			Input:           req.InputMessages(),
			Messages:        req.InputMessages(),
			Stream:          true,
			MaxOutputTokens: req.MaxOutputTokens,
			MaxTokens:       req.MaxTokens,
			Tools:           req.Tools,
			OutputFormat:    cloneOutputFormat(req.OutputFormat),
			Options:         provider.CloneRequestOptions(req.Options),
		}

		streamCtx, streamCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
		stream, upstreamErrCh := p.StreamResponse(streamCtx, upstreamReq)
		var finalResponse *provider.Response
		var assistantText string
		var streamUsage *provider.Usage
		var streamedOutputs []provider.ResponseOutput

		for {
			select {
			case event, ok := <-stream:
				if !ok {
					streamCancel()
					fallbackExec := &execution{
						provider:              p,
						requestedModel:        req.Model,
						upstreamRequest:       upstreamReq,
						responseID:            responseID,
						tenantID:              tenantID,
						routeTrace:            trace,
						startedAt:             startedAt,
						estimatedPromptTokens: req.EstimatePromptTokens(),
					}
					finalResponse = s.recoverStreamResponse(ctx, identity, fallbackExec, assistantText, streamedOutputs, finalResponse, hasSentPayload)
					applyRecoveredStreamUsage(finalResponse, streamUsage)
					latencyMs := time.Since(startedAt).Milliseconds()
					if budgetErr := validateVisibleOutputBudget(fallbackExec, finalResponse); budgetErr != nil && !hasSentPayload {
						_ = s.recordOutputBudgetError(ctx, identity, fallbackExec, finalResponse, latencyMs, providerName)
						s.providerMgr.Stats.RecordRequest(providerName, true, finalResponse.Usage.TotalTokens, latencyMs)
						s.providerMgr.Stats.DecrementLoad(providerName)
						if s.circuitBreaker != nil {
							s.circuitBreaker.RecordSuccess(tenantID, providerName)
						}
						errCh <- budgetErr
						return
					}
					body, _ := json.Marshal(finalResponse)
					s.writeCache(ctx, identity, req, &cache.Entry{
						Response:  body,
						Stream:    true,
						Model:     req.Model,
						Provider:  providerName,
						Usage: cache.Usage{
							PromptTokens:     finalResponse.Usage.PromptTokens,
							CompletionTokens: finalResponse.Usage.CompletionTokens,
							TotalTokens:      finalResponse.Usage.TotalTokens,
							CachedTokens:     finalResponse.Usage.CachedTokens,
						},
						CreatedAt: time.Now().Unix(),
					})
					s.finalizeStream(ctx, identity, responseID, providerName, req.Model, finalResponse, latencyMs, trace, out, !hasSentPayload)
					s.providerMgr.Stats.DecrementLoad(providerName)
					if s.circuitBreaker != nil {
						s.circuitBreaker.RecordSuccess(tenantID, providerName)
					}
					return
				}

				switch event.Type {
				case provider.EventContentDelta:
					if event.Usage != nil {
						usageCopy := *event.Usage
						streamUsage = &usageCopy
					}
					if len(event.ToolCalls) > 0 {
						streamedOutputs = appendStreamedToolCalls(streamedOutputs, event.ToolCalls)
					}
					if isRenderableStreamEvent(event) {
						hasSentPayload = true
						assistantText += event.Text()
						out <- event
					}
				case provider.EventToolCallDone:
					hasSentPayload = true
					streamedOutputs = appendStreamOutput(streamedOutputs, event.Output)
					out <- event
				case provider.EventResponseCompleted:
					finalResponse = event.Response
				}
			case err := <-upstreamErrCh:
				if err == nil {
					continue
				}
				streamCancel()
				appendRouteAttempt(trace, providerName, 0, "error", err)
				latencyMs := time.Since(startedAt).Milliseconds()
				s.handleStreamError(ctx, identity, responseID, providerName, req.Model, latencyMs, err)
				s.providerMgr.Stats.DecrementLoad(providerName)
				if s.circuitBreaker != nil {
					s.circuitBreaker.RecordFailure(tenantID, providerName)
				}

				if s.isStreamRetryable(err) && !hasSentPayload {
					for i := 0; i < retryCfg.MaxRetries; i++ {
						delay := float64(retryCfg.InitialDelayMs) * math.Pow(retryCfg.BackoffFactor, float64(i))
						delay = math.Min(delay, float64(retryCfg.MaxDelayMs))
						select {
						case <-ctx.Done():
							errCh <- ctx.Err()
							return
						case <-time.After(time.Duration(delay) * time.Millisecond):
						}

						streamCtx, streamCancel = context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
						stream, upstreamErrCh = p.StreamResponse(streamCtx, upstreamReq)
						assistantText = ""
						goto retryLoop
					}
				}

				if !hasSentPayload {
					goto nextProvider
				}

				errCh <- err
				return
			case <-ctx.Done():
				s.drainStreamForUsage(stream, upstreamErrCh, &finalResponse, &streamUsage, &streamedOutputs, &assistantText)
				streamCancel()
				s.handleStreamCancellation(ctx, identity, req, responseID, p, trace, finalResponse, assistantText, streamedOutputs, streamUsage, startedAt)
				s.providerMgr.Stats.DecrementLoad(providerName)
				errCh <- ctx.Err()
				return
			}
			continue

		retryLoop:
			for {
				select {
				case event, ok := <-stream:
					if !ok {
						streamCancel()
						fallbackExec := &execution{
							provider:              p,
							requestedModel:        req.Model,
							upstreamRequest:       upstreamReq,
							responseID:            responseID,
							tenantID:              tenantID,
							routeTrace:            trace,
							startedAt:             startedAt,
							estimatedPromptTokens: req.EstimatePromptTokens(),
						}
						finalResponse = s.recoverStreamResponse(ctx, identity, fallbackExec, assistantText, streamedOutputs, finalResponse, hasSentPayload)
						applyRecoveredStreamUsage(finalResponse, streamUsage)
						latencyMs := time.Since(startedAt).Milliseconds()
						if budgetErr := validateVisibleOutputBudget(fallbackExec, finalResponse); budgetErr != nil && !hasSentPayload {
							appendRouteAttempt(trace, providerName, retryCfg.MaxRetries, "budget_rejected", budgetErr)
							_ = s.recordOutputBudgetError(ctx, identity, fallbackExec, finalResponse, latencyMs, providerName)
							s.providerMgr.Stats.RecordRequest(providerName, true, finalResponse.Usage.TotalTokens, latencyMs)
							s.providerMgr.Stats.DecrementLoad(providerName)
							if s.circuitBreaker != nil {
								s.circuitBreaker.RecordSuccess(tenantID, providerName)
							}
							errCh <- budgetErr
							return
						}
						appendRouteAttempt(trace, providerName, retryCfg.MaxRetries, "success", nil)
						body, _ := json.Marshal(finalResponse)
						s.writeCache(ctx, identity, req, &cache.Entry{
							Response:  body,
							Stream:    true,
							Model:     req.Model,
							Provider:  providerName,
							Usage: cache.Usage{
								PromptTokens:     finalResponse.Usage.PromptTokens,
								CompletionTokens: finalResponse.Usage.CompletionTokens,
								TotalTokens:      finalResponse.Usage.TotalTokens,
								CachedTokens:     finalResponse.Usage.CachedTokens,
							},
							CreatedAt: time.Now().Unix(),
						})
						s.finalizeStream(ctx, identity, responseID, providerName, req.Model, finalResponse, latencyMs, trace, out, !hasSentPayload)
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
						s.providerMgr.Stats.DecrementLoad(providerName)
						if s.circuitBreaker != nil {
							s.circuitBreaker.RecordSuccess(tenantID, providerName)
						}
						return
					}

					switch event.Type {
					case provider.EventContentDelta:
						if event.Usage != nil {
							usageCopy := *event.Usage
							streamUsage = &usageCopy
						}
						if len(event.ToolCalls) > 0 {
							streamedOutputs = appendStreamedToolCalls(streamedOutputs, event.ToolCalls)
						}
						if isRenderableStreamEvent(event) {
							hasSentPayload = true
							assistantText += event.Text()
							out <- event
						}
					case provider.EventToolCallDone:
						hasSentPayload = true
						streamedOutputs = appendStreamOutput(streamedOutputs, event.Output)
						out <- event
					case provider.EventResponseCompleted:
						finalResponse = event.Response
					}
				case err := <-upstreamErrCh:
					if err == nil {
						continue
					}
					streamCancel()
					appendRouteAttempt(trace, providerName, retryCfg.MaxRetries, "error", err)
					latencyMs := time.Since(startedAt).Milliseconds()
					s.handleStreamError(ctx, identity, responseID, providerName, req.Model, latencyMs, err)
					s.providerMgr.Stats.DecrementLoad(providerName)
					if s.circuitBreaker != nil {
						s.circuitBreaker.RecordFailure(tenantID, providerName)
					}

					if !hasSentPayload && s.isStreamRetryable(err) {
						goto nextProvider
					}

					errCh <- err
					return
				case <-ctx.Done():
					s.drainWithSemaphore(stream, upstreamErrCh, &finalResponse, &streamUsage, &streamedOutputs, &assistantText)
					streamCancel()
					s.handleStreamCancellation(ctx, identity, req, responseID, p, trace, finalResponse, assistantText, streamedOutputs, streamUsage, startedAt)
					s.providerMgr.Stats.DecrementLoad(providerName)
					errCh <- ctx.Err()
					return
				}
			}

		nextProvider:
			s.providerMgr.Stats.DecrementLoad(providerName)
			break
		}
	}

	finalizeRouteTrace(trace, "", "no_provider", ErrNoProvider)
	_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
		ID:             responseID,
		TenantID:       tenantID,
		ProjectID:      identity.ProjectID,
		Model:          req.Model,
		Status:         "error",
		RouteTraceBody: routeTraceBytes(trace),
	})
	errCh <- ErrNoProvider
}

func (s *Service) prepare(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*execution, error) {
	selected, err := s.selectProvider(ctx, identity, sessionID, req)
	if err != nil {
		return nil, err
	}
	if selected == nil {
		return nil, ErrNoProvider
	}
	return s.prepareWithProvider(ctx, identity, req, sessionID, selected)
}

func (s *Service) prepareWithProvider(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string, selected provider.Provider) (*execution, error) {
	responseID := uuid.NewString()
	requestBody, _ := json.Marshal(req)
	if err := s.store.CreateResponse(ctx, repository.ResponseRecord{
		ID:           responseID,
		TenantID:     identity.TenantID,
		ProjectID:    identity.ProjectID,
		UserID:       identity.UserID,
		APIKeyID:     identity.APIKeyID,
		ProviderName: selected.Name(),
		Model:        req.Model,
		Status:       "in_progress",
		RequestBody:  requestBody,
	}); err != nil {
		return nil, err
	}

	upstreamReq := &provider.ResponseRequest{
		Model:             req.Model,
		PreferredProvider: req.PreferredProvider,
		Surface:           req.Surface,
		Input:             req.InputMessages(),
		Messages:          req.InputMessages(),
		Stream:            req.Stream,
		MaxOutputTokens:   req.MaxOutputTokens,
		MaxTokens:         req.MaxTokens,
		Tools:             req.Tools,
		OutputFormat:      cloneOutputFormat(req.OutputFormat),
		Options:           provider.CloneRequestOptions(req.Options),
	}

	return &execution{
		provider:              selected,
		requestedModel:        req.Model,
		upstreamRequest:       upstreamReq,
		responseID:            responseID,
		tenantID:              identity.TenantID,
		requestBody:           requestBody,
		startedAt:             time.Now(),
		estimatedPromptTokens: req.EstimatePromptTokens(),
	}, nil
}
