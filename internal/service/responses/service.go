package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/alert"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/provider"
	"github.com/gateyes/gateway/internal/service/router"
)

var ErrNoProvider = errors.New("no provider available")

type Dependencies struct {
	Config      *config.Config
	Store       repository.Store
	Auth        *auth.Auth
	ProviderMgr *provider.Manager
	Router      *router.Router
	Cache       *cache.Cache
	Alert       *alert.AlertService
}

type Service struct {
	cfg         *config.Config
	store       repository.Store
	auth        *auth.Auth
	providerMgr *provider.Manager
	router      *router.Router
	cache       *cache.Cache
	alert       *alert.AlertService
}

type CreateResult struct {
	Response         *provider.Response
	ProviderName     string
	LatencyMs        int64
	CacheHit         bool
	PromptTokens     int
	CompletionTokens int
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
		cfg:         deps.Config,
		store:       deps.Store,
		auth:        deps.Auth,
		providerMgr: deps.ProviderMgr,
		router:      deps.Router,
		cache:       deps.Cache,
		alert:       deps.Alert,
	}
}

func (s *Service) Create(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*CreateResult, error) {
	req.Normalize()

	exec, err := s.prepare(ctx, identity, req, sessionID)
	if err != nil {
		return nil, err
	}

	if s.cfg.Cache.Enabled && s.cache != nil && !req.Stream {
		if cached, ok := s.cache.Get(req.CacheKey()); ok {
			return s.useCachedResponse(ctx, identity, exec, cached)
		}
	}

	s.router.IncLoad(exec.provider.Name())
	s.providerMgr.Stats.IncrementLoad(exec.provider.Name())
	defer func() {
		s.router.DecLoad(exec.provider.Name())
		s.providerMgr.Stats.DecrementLoad(exec.provider.Name())
	}()

	resp, err := exec.provider.CreateResponse(ctx, exec.upstreamRequest)
	latencyMs := time.Since(exec.startedAt).Milliseconds()
	if err != nil {
		s.providerMgr.Stats.RecordRequest(exec.provider.Name(), false, 0, latencyMs)
		_ = s.markError(ctx, identity, exec, latencyMs)
		return nil, err
	}

	resp = s.normalizeResponse(exec, resp)
	if err := s.persistSuccess(ctx, identity, exec, resp, latencyMs); err != nil {
		return nil, err
	}

	if s.cfg.Cache.Enabled && s.cache != nil {
		body, _ := json.Marshal(resp)
		s.cache.Set(req.CacheKey(), string(body))
	}

	return &CreateResult{
		Response:         resp,
		ProviderName:     exec.provider.Name(),
		LatencyMs:        latencyMs,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
	}, nil
}

func (s *Service) CreateStream(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*Stream, error) {
	req.Normalize()

	exec, err := s.prepare(ctx, identity, req, sessionID)
	if err != nil {
		return nil, err
	}

	events := make(chan provider.ResponseEvent)
	errCh := make(chan error, 1)

	go s.runStream(ctx, identity, exec, events, errCh)

	return &Stream{
		ResponseID:   exec.responseID,
		ProviderName: exec.provider.Name(),
		StartedAt:    exec.startedAt,
		Events:       events,
		Errors:       errCh,
	}, nil
}

func (s *Service) prepare(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*execution, error) {
	selected, err := s.selectProvider(ctx, identity, sessionID, req.Model)
	if err != nil {
		return nil, err
	}
	if selected == nil {
		return nil, ErrNoProvider
	}

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

func (s *Service) useCachedResponse(ctx context.Context, identity *repository.AuthIdentity, exec *execution, cached string) (*CreateResult, error) {
	var resp provider.Response
	if err := json.Unmarshal([]byte(cached), &resp); err != nil {
		return nil, err
	}

	resp.ID = exec.responseID
	resp.Model = exec.requestedModel
	resp.Created = time.Now().Unix()
	resp.Status = "completed"

	if err := s.ensureQuotaAvailable(ctx, identity, exec, &resp, 0, "cache", 0); err != nil {
		return nil, err
	}

	body, _ := json.Marshal(resp)
	if err := s.store.UpdateResponse(ctx, repository.ResponseRecord{
		ID:           exec.responseID,
		TenantID:     exec.tenantID,
		ProviderName: "cache",
		Model:        exec.requestedModel,
		Status:       "completed",
		ResponseBody: body,
	}); err != nil {
		return nil, err
	}

	if err := s.auth.RecordUsage(
		ctx,
		identity,
		"cache",
		exec.requestedModel,
		resp.Usage.PromptTokens,
		resp.Usage.CompletionTokens,
		resp.Usage.TotalTokens,
		0,
		0,
		"success",
		"",
	); err != nil {
		if errors.Is(err, auth.ErrQuotaExceeded) {
			_ = s.recordQuotaExceeded(ctx, identity, exec, &resp, 0, "cache", 0)
		}
		return nil, err
	}

	return &CreateResult{
		Response:         &resp,
		ProviderName:     "cache",
		LatencyMs:        0,
		CacheHit:         true,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
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
