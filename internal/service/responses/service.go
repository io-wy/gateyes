package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/alert"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/provider"
	"github.com/gateyes/gateway/internal/service/router"
)

var (
	ErrNoProvider   = errors.New("no provider available")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
)

type Dependencies struct {
	Config      *config.Config
	Store       repository.Store
	Auth        *auth.Auth
	ProviderMgr *provider.Manager
	Router      *router.Router
	Alert       *alert.AlertService
}

type Service struct {
	cfg            *config.Config
	store          repository.Store
	auth           *auth.Auth
	providerMgr    *provider.Manager
	router         *router.Router
	alert          *alert.AlertService
	circuitBreaker *CircuitBreaker
}

type CreateResult struct {
	Response         *provider.Response
	ProviderName     string
	LatencyMs        int64
	PromptTokens     int
	CompletionTokens int
	Retries          int // 本次请求的重试次数
	Fallback         int // 本次请求的 fallback 次数
}

type Stream struct {
	ResponseID   string
	ProviderName string
	StartedAt    time.Time
	Events       <-chan provider.ResponseEvent
	Errors       <-chan error
}

type execution struct {
	provider              provider.Provider
	requestedModel        string
	upstreamRequest       *provider.ResponseRequest
	responseID            string
	tenantID              string
	requestBody           []byte
	startedAt             time.Time
	estimatedPromptTokens int
}

func New(deps *Dependencies) *Service {
	return &Service{
		cfg:            deps.Config,
		store:          deps.Store,
		auth:           deps.Auth,
		providerMgr:    deps.ProviderMgr,
		router:         deps.Router,
		alert:          deps.Alert,
		circuitBreaker: NewCircuitBreaker(deps.Config.CircuitBreaker),
	}
}

func (s *Service) Create(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*CreateResult, error) {
	req.Normalize()

	candidates := s.getCandidateProviders(ctx, identity, sessionID, req.Model)
	if len(candidates) == 0 {
		return nil, ErrNoProvider
	}

	// 先创建一条 in_progress 记录，使用第一个候选 provider
	firstProvider := candidates[0]
	responseID := uuid.NewString()
	requestBody, _ := json.Marshal(req)
	if err := s.store.CreateResponse(ctx, repository.ResponseRecord{
		ID:           responseID,
		TenantID:     identity.TenantID,
		UserID:       identity.UserID,
		APIKeyID:     identity.APIKeyID,
		ProviderName: firstProvider.Name(),
		Model:        req.Model,
		Status:       "in_progress",
		RequestBody:  requestBody,
	}); err != nil {
		return nil, err
	}

	var lastErr error
	var totalRetries int
	fallbackCount := 0

	for _, p := range candidates {
		tenantID := identity.TenantID
		providerName := p.Name()

		// 跳过熔断中的 provider 时计数
		if s.circuitBreaker != nil && !s.circuitBreaker.IsAvailable(tenantID, providerName) {
			continue
		}

		// 如果这不是第一个候选 provider，说明发生了 fallback
		if fallbackCount > 0 {
			// 更新 response 记录的 provider 名称
			_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
				ID:           responseID,
				TenantID:     tenantID,
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
			startedAt:             time.Now(),
			estimatedPromptTokens: req.EstimatePromptTokens(),
		}

		s.router.IncLoad(providerName)
		s.providerMgr.Stats.IncrementLoad(providerName)

		resp, retries, err := s.callWithRetry(ctx, identity, exec)
		totalRetries += retries
		latencyMs := time.Since(exec.startedAt).Milliseconds()

		if err != nil {
			s.router.DecLoad(providerName)
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

		s.router.DecLoad(providerName)
		s.providerMgr.Stats.DecrementLoad(providerName)
		if s.circuitBreaker != nil {
			s.circuitBreaker.RecordSuccess(tenantID, providerName)
		}

		resp = s.normalizeResponse(exec, resp)
		if err := s.persistSuccess(ctx, identity, exec, resp, latencyMs); err != nil {
			return nil, err
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

	return nil, lastErr
}

func buildUpstreamRequest(req *provider.ResponseRequest) *provider.ResponseRequest {
	return &provider.ResponseRequest{
		Model:           req.Model,
		Input:           req.InputMessages(),
		Messages:        req.InputMessages(),
		Stream:          req.Stream,
		MaxOutputTokens: req.MaxOutputTokens,
		MaxTokens:       req.MaxTokens,
		Tools:           req.Tools,
	}
}

func (s *Service) CreateStream(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*Stream, error) {
	req.Normalize()

	candidates := s.getCandidateProviders(ctx, identity, sessionID, req.Model)
	if len(candidates) == 0 {
		return nil, ErrNoProvider
	}

	events := make(chan provider.ResponseEvent)
	errCh := make(chan error, 1)

	// 先创建 response 记录，使用第一个成功响应的 provider
	responseID := uuid.NewString()
	requestBody, _ := json.Marshal(req)
	if err := s.store.CreateResponse(ctx, repository.ResponseRecord{
		ID:           responseID,
		TenantID:     identity.TenantID,
		UserID:       identity.UserID,
		APIKeyID:     identity.APIKeyID,
		ProviderName: "", // 先不填，等确定 provider 后更新
		Model:        req.Model,
		Status:       "in_progress",
		RequestBody:  requestBody,
	}); err != nil {
		return nil, err
	}

	go s.runStreamWithFallback(ctx, identity, req, sessionID, candidates, responseID, events, errCh)

	return &Stream{
		ResponseID:   responseID,
		ProviderName: "", // 异步确定
		StartedAt:    time.Now(),
		Events:       events,
		Errors:       errCh,
	}, nil
}

func (s *Service) runStreamWithFallback(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string, candidates []provider.Provider, responseID string, out chan<- provider.ResponseEvent, errCh chan<- error) {
	defer close(out)
	defer close(errCh)

	tenantID := identity.TenantID
	retryCfg := s.cfg.Retry
	startedAt := time.Now()
	firstResponseSent := false
	hasSentContent := false // 标记是否已经发送了内容给客户端

	for _, p := range candidates {
		providerName := p.Name()

		// 检查 circuit breaker
		if s.circuitBreaker != nil && !s.circuitBreaker.IsAvailable(tenantID, providerName) {
			continue
		}

		s.router.IncLoad(providerName)
		s.providerMgr.Stats.IncrementLoad(providerName)

		// 更新 response 记录的 provider
		_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
			ID:           responseID,
			TenantID:     tenantID,
			ProviderName: providerName,
			Model:        req.Model,
			Status:       "in_progress",
		})

		// 只在第一次真正开始流式响应时发送 response.created
		if !firstResponseSent {
			out <- provider.ResponseEvent{
				Type: "response.created",
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
			Input:           req.InputMessages(),
			Messages:        req.InputMessages(),
			Stream:          true,
			MaxOutputTokens: req.MaxOutputTokens,
			MaxTokens:       req.MaxTokens,
			Tools:           req.Tools,
		}

		stream, upstreamErrCh := p.StreamResponse(ctx, upstreamReq)
		var finalResponse *provider.Response
		var assistantText string

		for {
			select {
			case event, ok := <-stream:
				if !ok {
					if finalResponse == nil {
						finalResponse = provider.NewTextResponse(responseID, req.Model, assistantText, provider.Usage{
							PromptTokens:     req.EstimatePromptTokens(),
							CompletionTokens: provider.RoughTokenCount(assistantText),
							TotalTokens:      req.EstimatePromptTokens() + provider.RoughTokenCount(assistantText),
						})
					}
					latencyMs := time.Since(startedAt).Milliseconds()
					s.finalizeStream(ctx, identity, responseID, providerName, req.Model, finalResponse, latencyMs, out)
					s.router.DecLoad(providerName)
					s.providerMgr.Stats.DecrementLoad(providerName)
					if s.circuitBreaker != nil {
						s.circuitBreaker.RecordSuccess(tenantID, providerName)
					}
					return
				}

				switch event.Type {
				case "response.output_text.delta", "chat.delta":
					// 一旦发送了内容给客户端，就不能再进行 fallback
					hasSentContent = true
					assistantText += event.Delta
					out <- event
				case "response.output_item.done":
					out <- event
				case "response.completed":
					finalResponse = event.Response
				}
			case err := <-upstreamErrCh:
				if err == nil {
					continue
				}
				latencyMs := time.Since(startedAt).Milliseconds()
				s.handleStreamError(ctx, identity, responseID, providerName, req.Model, latencyMs, err)
				s.router.DecLoad(providerName)
				s.providerMgr.Stats.DecrementLoad(providerName)
				if s.circuitBreaker != nil {
					s.circuitBreaker.RecordFailure(tenantID, providerName)
				}

				// 只有在还没有发送内容给客户端时，才能进行重试
				if s.isStreamRetryable(err) && !hasSentContent {
					for i := 0; i < retryCfg.MaxRetries; i++ {
						delay := float64(retryCfg.InitialDelayMs) * math.Pow(retryCfg.BackoffFactor, float64(i))
						delay = math.Min(delay, float64(retryCfg.MaxDelayMs))
						select {
						case <-ctx.Done():
							errCh <- ctx.Err()
							return
						case <-time.After(time.Duration(delay) * time.Millisecond):
						}

						// 重试
						stream, upstreamErrCh = p.StreamResponse(ctx, upstreamReq)
						assistantText = ""
						goto retryLoop
					}
				}

				// 只有在还没有发送内容给客户端时，才能 fallback 到下一个 provider
				if !hasSentContent {
					// 当前 provider 失败，尝试下一个
					goto nextProvider
				}

				// 已经发送了内容给客户端，不能 fallback，直接返回错误
				errCh <- err
				return
			case <-ctx.Done():
				s.router.DecLoad(providerName)
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
						if finalResponse == nil {
							finalResponse = provider.NewTextResponse(responseID, req.Model, assistantText, provider.Usage{
								PromptTokens:     req.EstimatePromptTokens(),
								CompletionTokens: provider.RoughTokenCount(assistantText),
								TotalTokens:      req.EstimatePromptTokens() + provider.RoughTokenCount(assistantText),
							})
						}
						latencyMs := time.Since(startedAt).Milliseconds()
						s.finalizeStream(ctx, identity, responseID, providerName, req.Model, finalResponse, latencyMs, out)
						s.router.DecLoad(providerName)
						s.providerMgr.Stats.DecrementLoad(providerName)
						if s.circuitBreaker != nil {
							s.circuitBreaker.RecordSuccess(tenantID, providerName)
						}
						return
					}

					switch event.Type {
					case "response.output_text.delta", "chat.delta":
						// 一旦发送了内容给客户端，就不能再进行 fallback
						hasSentContent = true
						assistantText += event.Delta
						out <- event
					case "response.output_item.done":
						out <- event
					case "response.completed":
						finalResponse = event.Response
					}
				case err := <-upstreamErrCh:
					if err == nil {
						continue
					}
					latencyMs := time.Since(startedAt).Milliseconds()
					s.handleStreamError(ctx, identity, responseID, providerName, req.Model, latencyMs, err)
					s.router.DecLoad(providerName)
					s.providerMgr.Stats.DecrementLoad(providerName)
					if s.circuitBreaker != nil {
						s.circuitBreaker.RecordFailure(tenantID, providerName)
					}

					// 只有在还没有发送内容给客户端时，才能继续 fallback
					if !hasSentContent && s.isStreamRetryable(err) {
						goto nextProvider
					}

					// 已经发送了内容给客户端，不能 fallback，直接返回错误
					errCh <- err
					return
				case <-ctx.Done():
					s.router.DecLoad(providerName)
					s.providerMgr.Stats.DecrementLoad(providerName)
					errCh <- ctx.Err()
					return
				}
			}

		nextProvider:
			s.router.DecLoad(providerName)
			s.providerMgr.Stats.DecrementLoad(providerName)
			continue
		}
	}

	// 所有 provider 都失败，最后发送错误
	errCh <- ErrNoProvider
}

func (s *Service) isStreamRetryable(err error) bool {
	return isRetryable(err)
}

func (s *Service) finalizeStream(ctx context.Context, identity *repository.AuthIdentity, responseID, providerName, model string, resp *provider.Response, latencyMs int64, out chan<- provider.ResponseEvent) {
	if resp == nil {
		resp = provider.NewTextResponse(responseID, model, "", provider.Usage{})
	}
	resp.ID = responseID
	resp.Model = model
	resp.Created = time.Now().Unix()
	resp.Status = "completed"

	body, _ := json.Marshal(resp)
	_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
		ID:           responseID,
		TenantID:     identity.TenantID,
		ProviderName: providerName,
		Model:        model,
		Status:       "completed",
		ResponseBody: body,
	})

	_ = s.auth.RecordUsage(ctx, identity, providerName, model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens, 0, latencyMs, "success", "")

	s.providerMgr.Stats.RecordRequest(providerName, true, resp.Usage.TotalTokens, latencyMs)

	out <- provider.ResponseEvent{Type: "response.completed", Response: resp}
}

func (s *Service) handleStreamError(ctx context.Context, identity *repository.AuthIdentity, responseID, providerName, model string, latencyMs int64, streamErr error) {
	_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
		ID:           responseID,
		TenantID:     identity.TenantID,
		ProviderName: providerName,
		Model:        model,
		Status:       "error",
	})
	_ = s.auth.RecordUsage(ctx, identity, providerName, model, 0, 0, 0, 0, latencyMs, "error", "upstream_error")
}

func (s *Service) prepare(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*execution, error) {
	selected, err := s.selectProvider(ctx, identity, sessionID, req.Model)
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
		Model:           req.Model, // 透传请求中的模型名
		Input:           req.InputMessages(),
		Messages:        req.InputMessages(),
		Stream:          req.Stream,
		MaxOutputTokens: req.MaxOutputTokens,
		MaxTokens:       req.MaxTokens,
		Tools:           req.Tools,
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

	s.router.IncLoad(exec.provider.Name())
	s.providerMgr.Stats.IncrementLoad(exec.provider.Name())
	defer func() {
		s.router.DecLoad(exec.provider.Name())
		s.providerMgr.Stats.DecrementLoad(exec.provider.Name())
	}()

	out <- provider.ResponseEvent{
		Type: "response.created",
		Response: &provider.Response{
			ID:      exec.responseID,
			Object:  "response",
			Created: exec.startedAt.Unix(),
			Model:   exec.requestedModel,
			Status:  "in_progress",
		},
	}

	stream, upstreamErrCh := exec.provider.StreamResponse(ctx, exec.upstreamRequest)
	var finalResponse *provider.Response
	var assistantText string

	for {
		select {
		case event, ok := <-stream:
			if !ok {
				if finalResponse == nil {
					finalResponse = provider.NewTextResponse(exec.responseID, exec.requestedModel, assistantText, provider.Usage{
						PromptTokens:     exec.estimatedPromptTokens,
						CompletionTokens: provider.RoughTokenCount(assistantText),
						TotalTokens:      exec.estimatedPromptTokens + provider.RoughTokenCount(assistantText),
					})
				}
				finalResponse = s.normalizeResponse(exec, finalResponse)
				latencyMs := time.Since(exec.startedAt).Milliseconds()
				if err := s.persistSuccess(ctx, identity, exec, finalResponse, latencyMs); err != nil {
					errCh <- err
					return
				}
				out <- provider.ResponseEvent{Type: "response.completed", Response: finalResponse}
				return
			}

			switch event.Type {
			case "response.output_text.delta", "chat.delta":
				assistantText += event.Delta
				out <- event
			case "response.output_item.done":
				out <- event
			case "response.completed":
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
			return
		}
	}
}

func (s *Service) persistSuccess(ctx context.Context, identity *repository.AuthIdentity, exec *execution, resp *provider.Response, latencyMs int64) error {
	cost := exec.provider.Cost(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	if err := s.ensureQuotaAvailable(ctx, identity, exec, resp, latencyMs, exec.provider.Name(), cost); err != nil {
		return err
	}

	body, _ := json.Marshal(resp)
	if err := s.store.UpdateResponse(ctx, repository.ResponseRecord{
		ID:           exec.responseID,
		TenantID:     identity.TenantID,
		ProviderName: exec.provider.Name(),
		Model:        exec.requestedModel,
		Status:       "completed",
		ResponseBody: body,
	}); err != nil {
		return err
	}

	if err := s.auth.RecordUsage(
		ctx,
		identity,
		exec.provider.Name(),
		exec.requestedModel,
		resp.Usage.PromptTokens,
		resp.Usage.CompletionTokens,
		resp.Usage.TotalTokens,
		cost,
		latencyMs,
		"success",
		"",
	); err != nil {
		if errors.Is(err, auth.ErrQuotaExceeded) {
			_ = s.recordQuotaExceeded(ctx, identity, exec, resp, latencyMs, exec.provider.Name(), cost)
		}
		return err
	}

	s.providerMgr.Stats.RecordRequest(exec.provider.Name(), true, resp.Usage.TotalTokens, latencyMs)

	// 检查配额使用情况并发送预警
	if s.alert != nil {
		s.alert.CheckQuotaUsage(ctx, identity)
	}

	return nil
}

func (s *Service) ensureQuotaAvailable(ctx context.Context, identity *repository.AuthIdentity, exec *execution, resp *provider.Response, latencyMs int64, providerName string, cost float64) error {
	if s.auth.HasQuota(identity, resp.Usage.TotalTokens) {
		return nil
	}
	return s.recordQuotaExceeded(ctx, identity, exec, resp, latencyMs, providerName, cost)
}

func (s *Service) recordQuotaExceeded(ctx context.Context, identity *repository.AuthIdentity, exec *execution, resp *provider.Response, latencyMs int64, providerName string, cost float64) error {
	_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
		ID:           exec.responseID,
		TenantID:     identity.TenantID,
		ProviderName: providerName,
		Model:        exec.requestedModel,
		Status:       "error",
		ResponseBody: nil,
	})
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

func (s *Service) markError(ctx context.Context, identity *repository.AuthIdentity, exec *execution, latencyMs int64) error {
	_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
		ID:           exec.responseID,
		TenantID:     identity.TenantID,
		ProviderName: exec.provider.Name(),
		Model:        exec.requestedModel,
		Status:       "error",
	})
	return s.auth.RecordUsage(ctx, identity, exec.provider.Name(), exec.requestedModel, 0, 0, 0, 0, latencyMs, "error", "upstream_error")
}

func (s *Service) markErrorWithProvider(ctx context.Context, identity *repository.AuthIdentity, exec *execution, latencyMs int64, providerName string) error {
	_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
		ID:           exec.responseID,
		TenantID:     identity.TenantID,
		ProviderName: providerName,
		Model:        exec.requestedModel,
		Status:       "error",
	})
	return s.auth.RecordUsage(ctx, identity, providerName, exec.requestedModel, 0, 0, 0, 0, latencyMs, "error", "upstream_error")
}

func (s *Service) normalizeResponse(exec *execution, resp *provider.Response) *provider.Response {
	if resp == nil {
		resp = provider.NewTextResponse(exec.responseID, exec.requestedModel, "", provider.Usage{})
	}
	if resp.ID == "" {
		resp.ID = exec.responseID
	} else {
		resp.ID = exec.responseID
	}
	if resp.Object == "" {
		resp.Object = "response"
	}
	if resp.Created == 0 {
		resp.Created = time.Now().Unix()
	}
	resp.Model = exec.requestedModel
	if resp.Status == "" {
		resp.Status = "completed"
	}
	if resp.Usage.PromptTokens == 0 {
		resp.Usage.PromptTokens = exec.estimatedPromptTokens
	}
	if resp.Usage.CompletionTokens == 0 {
		resp.Usage.CompletionTokens = provider.RoughTokenCount(resp.Signature())
	}
	if resp.Usage.TotalTokens == 0 {
		resp.Usage.TotalTokens = resp.Usage.PromptTokens + resp.Usage.CompletionTokens
	}
	return resp
}

func (s *Service) selectProvider(ctx context.Context, identity *repository.AuthIdentity, sessionID string, model string) (provider.Provider, error) {
	providerNames, err := s.store.ListTenantProviders(ctx, identity.TenantID)
	if err != nil {
		return nil, err
	}
	return s.router.SelectFromWithModel(s.providerMgr.ListByNames(providerNames), sessionID, model), nil
}

func isRetryable(err error) bool {
	if err == nil {
		return true
	}

	// 检查 sentinel errors
	if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrForbidden) {
		return false
	}

	// 检查 UpstreamError（provider 返回的结构化错误）
	var upstreamErr *provider.UpstreamError
	if errors.As(err, &upstreamErr) {
		return upstreamErr.IsRetryable()
	}

	// 回退到字符串匹配（旧版 provider 错误）
	errMsg := err.Error()

	// 不重试客户端错误（4xx 除了 429）
	if strings.Contains(errMsg, "401") || strings.Contains(errMsg, "403") || strings.Contains(errMsg, "400") ||
		strings.Contains(errMsg, "422") || strings.Contains(errMsg, "404") {
		return false
	}

	// 429 可以重试
	// 5xx 服务端错误应该重试
	return true
}

func (s *Service) callWithRetry(ctx context.Context, identity *repository.AuthIdentity, exec *execution) (*provider.Response, int, error) {
	retryCfg := s.cfg.Retry
	var lastErr error
	retryCount := 0

	for i := 0; i <= retryCfg.MaxRetries; i++ {
		resp, err := exec.provider.CreateResponse(ctx, exec.upstreamRequest)

		if err == nil {
			return resp, retryCount, nil
		}
		lastErr = err

		if !isRetryable(err) {
			return nil, retryCount, err
		}

		if i < retryCfg.MaxRetries {
			retryCount++
			delay := float64(retryCfg.InitialDelayMs) * math.Pow(retryCfg.BackoffFactor, float64(i))
			delay = math.Min(delay, float64(retryCfg.MaxDelayMs))
			// 使用 select 响应 ctx 取消
			select {
			case <-ctx.Done():
				return nil, retryCount, ctx.Err()
			case <-time.After(time.Duration(delay) * time.Millisecond):
			}
		}
	}

	if lastErr != nil {
		return nil, retryCount, fmt.Errorf("all retries exhausted: %w", lastErr)
	}
	return nil, retryCount, fmt.Errorf("all retries exhausted")
}

func (s *Service) getCandidateProviders(ctx context.Context, identity *repository.AuthIdentity, sessionID string, model string) []provider.Provider {
	providerNames, err := s.store.ListTenantProviders(ctx, identity.TenantID)
	if err != nil {
		return nil
	}
	candidates := s.providerMgr.ListByNames(providerNames)
	if len(candidates) == 0 {
		return nil
	}
	// 使用 router 策略排序候选 providers
	if s.router != nil {
		return s.sortCandidatesByStrategy(candidates, sessionID, model)
	}
	return candidates
}

func (s *Service) sortCandidatesByStrategy(candidates []provider.Provider, sessionID string, model string) []provider.Provider {
	if len(candidates) <= 1 {
		return candidates
	}

	// 复制一份避免修改原列表
	result := make([]provider.Provider, len(candidates))
	copy(result, candidates)

	// 使用 router 的策略来排序
	// 先按 router 策略选出主 provider，然后按优先级排序
	switch s.router.Strategy() {
	case "round_robin":
		// round_robin 已经通过 index 维护，直接按原始顺序
		return result
	case "least_load":
		// 按负载升序排序
		sort.Slice(result, func(i, j int) bool {
			loadI := s.router.Load(result[i].Name())
			loadJ := s.router.Load(result[j].Name())
			return loadI < loadJ
		})
	case "cost_based":
		// 按成本升序排序
		sort.Slice(result, func(i, j int) bool {
			return result[i].UnitCost() < result[j].UnitCost()
		})
	case "sticky":
		// 按 session 哈希排序
		if sessionID != "" {
			hash := 0
			for _, ch := range sessionID {
				hash = hash*31 + int(ch)
			}
			// 让同一个 session 映射到固定的顺序
			sort.Slice(result, func(i, j int) bool {
				return (hash+i)%len(result) < (hash+j)%len(result)
			})
		}
	case "random":
		// 随机打乱
		rand.Shuffle(len(result), func(i, j int) {
			result[i], result[j] = result[j], result[i]
		})
	default:
		// 默认 round_robin，保持原顺序
	}

	return result
}

func WrapError(err error) ginError {
	switch {
	case errors.Is(err, auth.ErrModelNotAllowed):
		return ginError{Status: 403, Message: err.Error(), Type: "invalid_request_error"}
	case errors.Is(err, auth.ErrQuotaExceeded):
		return ginError{Status: 429, Message: err.Error(), Type: "rate_limit_error"}
	case errors.Is(err, ErrNoProvider):
		return ginError{Status: 503, Message: err.Error(), Type: "internal_error"}
	default:
		return ginError{Status: 500, Message: err.Error(), Type: "internal_error"}
	}
}

type ginError struct {
	Status  int
	Message string
	Type    string
}

func (e ginError) Error() string {
	return fmt.Sprintf("%d %s", e.Status, e.Message)
}

// GetCircuitBreakerStates returns all circuit breaker states for metrics collection
func (s *Service) GetCircuitBreakerStates() map[string]int {
	if s.circuitBreaker == nil {
		return nil
	}
	return s.circuitBreaker.GetAllStates()
}
