package responses

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

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

func (s *Service) runStream(ctx context.Context, identity *repository.AuthIdentity, exec *execution, out chan<- provider.ResponseEvent, errCh chan<- error) {
	defer close(out)
	defer close(errCh)

	s.providerMgr.Stats.IncrementLoad(exec.provider.Name())
	defer func() {
		s.providerMgr.Stats.DecrementLoad(exec.provider.Name())
	}()

	out <- provider.ResponseEvent{
		Type: provider.EventResponseStarted,
		Response: &provider.Response{
			ID:      exec.responseID,
			Object:  "response",
			Created: exec.startedAt.Unix(),
			Model:   exec.requestedModel,
			Status:  "in_progress",
		},
	}

	streamCtx, streamCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
	defer streamCancel()
	stream, upstreamErrCh := exec.provider.StreamResponse(streamCtx, exec.upstreamRequest)
	var finalResponse *provider.Response
	var assistantText string
	hasSentPayload := false
	var streamUsage *provider.Usage
	var streamedOutputs []provider.ResponseOutput

	for {
		select {
		case event, ok := <-stream:
			if !ok {
				finalResponse = s.recoverStreamResponse(ctx, identity, exec, assistantText, streamedOutputs, finalResponse, hasSentPayload)
				applyRecoveredStreamUsage(finalResponse, streamUsage)
				finalResponse = s.normalizeResponse(exec, finalResponse)
				latencyMs := time.Since(exec.startedAt).Milliseconds()
				if err := s.persistSuccess(ctx, identity, exec, finalResponse, latencyMs); err != nil {
					errCh <- err
					return
				}
				if !hasSentPayload {
					s.emitStreamPayloadFromResponse(out, finalResponse)
				}
				out <- provider.ResponseEvent{Type: provider.EventResponseCompleted, Response: finalResponse}
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
			latencyMs := time.Since(exec.startedAt).Milliseconds()
			s.providerMgr.Stats.RecordRequest(exec.provider.Name(), false, 0, latencyMs)
			_ = s.markError(ctx, identity, exec, latencyMs)
			errCh <- err
			return
		case <-ctx.Done():
			s.drainWithSemaphore(stream, upstreamErrCh, &finalResponse, &streamUsage, &streamedOutputs, &assistantText)
			s.handleStreamCancellation(ctx, identity, exec.upstreamRequest, exec.responseID, exec.provider, exec.routeTrace, finalResponse, assistantText, streamedOutputs, streamUsage, exec.startedAt)
			return
		}
	}
}
