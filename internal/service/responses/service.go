package responses

import (
	"context"
	"bytes"
	"net/http"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/alert"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/eventbus"
	"github.com/gateyes/gateway/internal/service/guardrail"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/pricing"
	"github.com/gateyes/gateway/internal/service/provider"
	"github.com/gateyes/gateway/internal/service/router"
	"golang.org/x/sync/singleflight"
	"go.opentelemetry.io/otel/attribute"
	"github.com/gateyes/gateway/internal/trace"
)

var (
	ErrNoProvider         = errors.New("no provider available")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrForbidden          = errors.New("forbidden")
	ErrOutputBudgetTooLow = errors.New("output budget too low")
)

// CacheMetrics is the subset of metrics methods used by the responses service.
// It is defined here to avoid an import cycle with internal/handler.
type CacheMetrics interface {
	RecordCacheLookup(layer, result string)
	RecordCacheWrite(layer, result string)
	ObserveCacheValueSize(layer string, size int)
	ObserveCacheGetDuration(layer string, d time.Duration)
}

type Dependencies struct {
	Config      *config.Config
	Store       repository.Store
	Auth        *auth.Auth
	ProviderMgr *provider.Manager
	Router      *router.Router
	Alert       *alert.AlertService
	Limiter     *limiter.Limiter
	Cache       cache.Cache
	Metrics     CacheMetrics
	// EventBus is optional. When set, post-response persistence work
	// (UpdateResponse body write, alert webhooks, callbacks) runs on the
	// bus's worker pool off the hot path. When nil, falls back to inline
	// detached goroutines.
	EventBus *eventbus.Bus
	// Guardrails is optional. When non-nil, PreCall runs before cache
	// lookup / provider call; PostCall runs before cache write / response
	// return. Block verdicts surface as errors to the client.
	Guardrails *guardrail.Manager
	// PricingFeed is optional. When provider.Cost() returns 0 (yaml has
	// no price configured), the service falls back to the model→price
	// feed before recording usage. yaml prices remain authoritative.
	PricingFeed *pricing.Feed
}

type Service struct {
	cfg            *config.Config
	store          repository.Store
	auth           *auth.Auth
	providerMgr    *provider.Manager
	router         *router.Router
	alert          *alert.AlertService
	limiter        *limiter.Limiter
	circuitBreaker *CircuitBreaker
	cache          cache.Cache
	metrics        CacheMetrics
	eventBus       *eventbus.Bus
	guardrails     *guardrail.Manager
	pricingFeed    *pricing.Feed
	// sfg deduplicates concurrent cache misses against the same key,
	// preventing thundering-herd upstream calls. The shared response is
	// returned to all waiters; only the first caller does the bookkeeping
	// (DB write + usage record). See cache.singleflight in config.
	sfg singleflight.Group
}

const terminalPersistenceTimeout = 5 * time.Second

// streamCancelDrainTimeout caps the absolute time we keep reading upstream
// events after the client disconnects, so we capture the final usage chunk
// for billing. LLM providers typically emit usage in the last chunk of a
// stream; bailing out on client ctx.Done() drops that data.
const streamCancelDrainTimeout = 5 * time.Second

// streamCancelDrainQuiet is the soft, "no-activity" cap. If the upstream
// goes silent for this long after a client disconnect, we assume nothing
// more is coming and exit drain — even if the absolute timeout has not
// fired. Keeps the cancellation path responsive when providers have
// already emitted their last frame.
const streamCancelDrainQuiet = 250 * time.Millisecond

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
	routeTrace            *routeTrace
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
		limiter:        deps.Limiter,
		circuitBreaker: NewCircuitBreaker(deps.Config.CircuitBreaker),
		cache:          deps.Cache,
		metrics:        deps.Metrics,
		eventBus:       deps.EventBus,
		guardrails:     deps.Guardrails,
		pricingFeed:    deps.PricingFeed,
	}
}

// computeCost returns the cost for a request. Provider yaml-config wins
// when it has a non-zero price; otherwise we consult the pricing feed
// keyed by the requested model name. When neither is set, returns 0
// and the request is recorded as zero-cost (which is correct — we have
// no basis to bill).
func (s *Service) computeCost(p provider.Provider, requestedModel string, promptTokens, completionTokens int) float64 {
	if cost := p.Cost(promptTokens, completionTokens); cost > 0 {
		return cost
	}
	if s.pricingFeed != nil {
		// Try requested model name first (what the user asked for);
		// fall back to provider's actual model identifier.
		for _, name := range []string{requestedModel, p.Model()} {
			if name == "" {
				continue
			}
			if mp, ok := s.pricingFeed.Get(name); ok {
				return float64(promptTokens)*mp.InputPerToken + float64(completionTokens)*mp.OutputPerToken
			}
		}
	}
	return 0
}

// ErrGuardrailBlocked is returned when a guardrail vetoes the request
// or response. The wrapped error includes the guardrail name + reason.
var ErrGuardrailBlocked = errors.New("blocked by guardrail")

// drainStreamForUsage continues reading from a provider's stream channel
// after the caller's ctx has been cancelled, so the gateway can capture
// the final usage chunk that providers typically emit at end-of-stream.
//
// The drain stops on stream close, upstream error, or after
// streamCancelDrainTimeout — whichever comes first. The caller must have
// already arranged for the provider's stream goroutine to be using a
// detached context (via context.WithoutCancel + WithTimeout) so it does
// not exit immediately when the client disconnected.
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
			// Activity — reset the quiet-period guard.
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

// sfCallResult is the shared payload returned to all singleflight waiters.
type sfCallResult struct {
	resp    *provider.Response
	retries int
}

// callWithRetrySF wraps callWithRetry with a singleflight group keyed by
// (tenant, model, prompt-canon, provider). When two requests arrive within
// the same upstream-call window with identical inputs and route to the
// same provider, only one HTTP roundtrip is made — waiters receive a copy
// of the same response.
//
// Each caller still performs its own bookkeeping (DB record, persistSuccess
// → RecordUsage → Stats). What we save is the network call to the model
// provider and one token-of-tokens of upstream cost.
//
// When cache or singleflight is disabled, the call is direct.
func (s *Service) callWithRetrySF(ctx context.Context, identity *repository.AuthIdentity, exec *execution, _ string, req *provider.ResponseRequest) (*provider.Response, int, error) {
	if s.cache == nil || !s.cfg.Cache.Enabled || !s.cfg.Cache.Singleflight {
		return s.callWithRetry(ctx, identity, exec)
	}
	if s.cfg.Cache.SkipTools && len(req.Tools) > 0 {
		return s.callWithRetry(ctx, identity, exec)
	}
	cacheKey := s.buildCacheKey(ctx, identity, req)
	if cacheKey == "" {
		return s.callWithRetry(ctx, identity, exec)
	}
	sfKey := cacheKey + "|" + exec.provider.Name()

	val, err, _ := s.sfg.Do(sfKey, func() (any, error) {
		resp, retries, err := s.callWithRetry(ctx, identity, exec)
		if err != nil {
			return nil, err
		}
		return &sfCallResult{resp: resp, retries: retries}, nil
	})
	if err != nil {
		return nil, 0, err
	}
	r := val.(*sfCallResult)
	// Return a defensive shallow clone so the second caller's downstream
	// mutations (e.g. normalizeResponse setting ID/Status) don't race
	// with the first caller. Output / Usage are read-only at this point
	// per provider contract; aliasing is acceptable.
	respCopy := *r.resp
	return &respCopy, r.retries, nil
}

// publishOrInline runs the given handler on the eventBus when configured.
//
// Three modes:
//   - eventBus configured + Publish accepts → async on bus worker pool
//     (production happy path — moves bookkeeping off the hot path)
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
		// Bus saturated — fall back to a detached goroutine so we don't
		// block the response and don't drop billing writes.
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

func (s *Service) cacheLayer(stream bool) string {
	if stream {
		return cache.LayerL1Stream
	}
	return cache.LayerL1
}

func (s *Service) shouldSkipCache(ctx context.Context, req *provider.ResponseRequest) bool {
	if s.cache == nil || !s.cfg.Cache.Enabled {
		return true
	}
	if s.cfg.Cache.SkipStream && req.Stream {
		return true
	}
	if s.cfg.Cache.SkipTools && len(req.Tools) > 0 {
		return true
	}
	if CacheHintsFrom(ctx).Skip {
		return true
	}
	return false
}

func (s *Service) buildCacheKey(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest) string {
	msgs := req.InputMessages()
	payload := map[string]any{
		"model":    req.Model,
		"messages": msgs,
		"stream":   req.Stream,
	}
	if req.MaxOutputTokens > 0 {
		payload["max_output_tokens"] = req.MaxOutputTokens
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		payload["tools"] = req.Tools
	}
	// TODO: include OutputFormat and Options in cache key
	canon, _ := cache.CanonicalizeJSON(payload)
	return cache.BuildKey(cache.KeyInput{
		TenantID:    identity.TenantID,
		Model:       req.Model,
		PromptCanon: string(canon),
		Stream:      req.Stream,
		Surface:     req.Surface,
		Bucket:      CacheHintsFrom(ctx).Bucket,
	})
}

func (s *Service) lookupCache(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest) (*cache.Entry, bool) {
	if s.shouldSkipCache(ctx, req) {
		return nil, false
	}
	cacheKey := s.buildCacheKey(ctx, identity, req)
	start := time.Now()
	entry, hit, err := s.cache.Get(ctx, cacheKey)
	layer := s.cacheLayer(req.Stream)
	if s.metrics != nil {
		s.metrics.ObserveCacheGetDuration(layer, time.Since(start))
		if hit {
			s.metrics.RecordCacheLookup(layer, "hit")
			s.metrics.ObserveCacheValueSize(layer, len(entry.Response)+len(entry.StreamRaw))
		} else if err != nil {
			s.metrics.RecordCacheLookup(layer, "error")
		} else {
			s.metrics.RecordCacheLookup(layer, "miss")
		}
	}
	return entry, hit
}

func (s *Service) writeCache(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, entry *cache.Entry) {
	if s.shouldSkipCache(ctx, req) || entry == nil {
		return
	}
	cacheKey := s.buildCacheKey(ctx, identity, req)
	layer := s.cacheLayer(req.Stream)
	var ttl time.Duration
	if s.cfg.Cache.DefaultTTL > 0 {
		ttl = time.Duration(s.cfg.Cache.DefaultTTL) * time.Second
	}
	// Header-driven TTL override beats yaml default. Zero means "use default".
	if hint := CacheHintsFrom(ctx).TTL; hint > 0 {
		ttl = hint
	}
	// fire-and-forget; cache write should not block the hot path
	go func() {
		if err := s.cache.Set(context.Background(), cacheKey, entry, ttl); err != nil {
			if s.metrics != nil {
				s.metrics.RecordCacheWrite(layer, "error")
			}
			return
		}
		if s.metrics != nil {
			s.metrics.RecordCacheWrite(layer, "success")
			s.metrics.ObserveCacheValueSize(layer, len(entry.Response)+len(entry.StreamRaw))
		}
	}()
}

func (s *Service) replayCachedStream(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, entry *cache.Entry, responseID string, out chan<- provider.ResponseEvent, errCh chan<- error) {
	var resp provider.Response
	if err := json.Unmarshal(entry.Response, &resp); err != nil {
		errCh <- err
		return
	}
	startedAt := time.Now()
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
	resp.ID = responseID
	resp.Created = startedAt.Unix()
	resp.Status = "completed"
	out <- provider.ResponseEvent{
		Type:     provider.EventResponseCompleted,
		Response: &resp,
	}
	body, _ := json.Marshal(resp)
	_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
		ID:           responseID,
		TenantID:     identity.TenantID,
		ProjectID:    identity.ProjectID,
		ProviderName: entry.Provider,
		Model:        req.Model,
		Status:       "completed",
		ResponseBody: body,
	})
}

func (s *Service) Create(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*CreateResult, error) {
	req.Normalize()
	createStart := time.Now()

	// Run pre-call guardrails before any cache lookup or provider call.
	// Block here means the request never reaches upstream — no billing.
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
		// unmarshal failed — treat as miss and continue
	}

	candidates, trace := s.planCandidates(ctx, identity, sessionID, req)
	if len(candidates) == 0 {
		return nil, ErrNoProvider
	}

	// 先创建一条 in_progress 记录，使用第一个候选 provider
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

		// provider 维度限流检查
		if s.limiter != nil && !s.limiter.CheckProvider(providerName, req.EstimateAdmissionTokens()) {
			appendRouteAttempt(trace, providerName, 0, "rate_limited", fmt.Errorf("provider rate limited"))
			continue
		}

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

		// Run post-call guardrails on the upstream response. Block here
		// means we still owe the upstream call (we paid for it) but we
		// won't return / cache the response — usage is recorded so the
		// gateway operator sees the spend.
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

func (s *Service) CreateStream(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*Stream, error) {
	req.Normalize()

	candidates, trace := s.planCandidates(ctx, identity, sessionID, req)
	if len(candidates) == 0 {
		return nil, ErrNoProvider
	}

	events := make(chan provider.ResponseEvent)
	errCh := make(chan error, 1)

	// 先创建 response 记录，使用第一个成功响应的 provider
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
		ProviderName:   "", // 先不填，等确定 provider 后更新
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

	// L1 cache fast path for streaming
	if entry, hit := s.lookupCache(ctx, identity, req); hit {
		s.replayCachedStream(ctx, identity, req, entry, responseID, out, errCh)
		return
	}

	tenantID := identity.TenantID
	retryCfg := s.cfg.Retry
	startedAt := time.Now()
	firstResponseSent := false
	hasSentPayload := false // 标记是否已经发送了可见 payload 给客户端

	for _, p := range candidates {
		providerName := p.Name()

		// provider 维度限流检查
		if s.limiter != nil && !s.limiter.CheckProvider(providerName, req.EstimateAdmissionTokens()) {
			appendRouteAttempt(trace, providerName, 0, "rate_limited", fmt.Errorf("provider rate limited"))
			continue
		}

		// 检查 circuit breaker
		if s.circuitBreaker != nil && !s.circuitBreaker.IsAvailable(tenantID, providerName) {
			continue
		}

		s.providerMgr.Stats.IncrementLoad(providerName)

		// 更新 response 记录的 provider
		_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
			ID:             responseID,
			TenantID:       tenantID,
			ProjectID:      identity.ProjectID,
			ProviderName:   providerName,
			Model:          req.Model,
			Status:         "in_progress",
			RouteTraceBody: routeTraceBytes(trace),
		})

		// 只在第一次真正开始流式响应时发送 response.created
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

		// Detach upstream stream from client ctx so client disconnect doesn't
		// kill it before the final usage chunk arrives. We still bound it
		// with a generous timeout to avoid runaway streams.
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
						// 一旦发送了可见内容给客户端，就不能再进行 fallback
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

				// 只有在还没有发送内容给客户端时，才能进行重试
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

						// Retry: open a new detached upstream ctx for this attempt.
						streamCtx, streamCancel = context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
						stream, upstreamErrCh = p.StreamResponse(streamCtx, upstreamReq)
						assistantText = ""
						goto retryLoop
					}
				}

				// 只有在还没有发送内容给客户端时，才能 fallback 到下一个 provider
				if !hasSentPayload {
					// 当前 provider 失败，尝试下一个
					goto nextProvider
				}

				// 已经发送了内容给客户端，不能 fallback，直接返回错误
				errCh <- err
				return
			case <-ctx.Done():
				// Client disconnected. Drain remaining upstream events for up
				// to streamCancelDrainTimeout to capture the final usage
				// chunk for billing — providers typically emit usage in the
				// last frame. The upstream call uses a detached ctx so it
				// keeps running while we drain.
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
							// 一旦发送了可见内容给客户端，就不能再进行 fallback
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

					// 只有在还没有发送内容给客户端时，才能继续 fallback
					if !hasSentPayload && s.isStreamRetryable(err) {
						goto nextProvider
					}

					// 已经发送了内容给客户端，不能 fallback，直接返回错误
					errCh <- err
					return
				case <-ctx.Done():
					// Client disconnected during retry. Drain for usage too.
					s.drainStreamForUsage(stream, upstreamErrCh, &finalResponse, &streamUsage, &streamedOutputs, &assistantText)
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

	// 所有 provider 都失败，最后发送错误
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

func (s *Service) isStreamRetryable(err error) bool {
	return isRetryable(err)
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

func (s *Service) recoverStreamResponse(ctx context.Context, identity *repository.AuthIdentity, exec *execution, assistantText string, streamedOutputs []provider.ResponseOutput, finalResponse *provider.Response, hasSentPayload bool) *provider.Response {
	if !hasSentPayload && !hasRenderableStreamPayload(finalResponse) {
		recovered, _, err := s.callWithRetry(context.WithoutCancel(ctx), identity, exec)
		if err == nil && recovered != nil {
			finalResponse = recovered
		}
	}
	if !hasRenderableStreamPayload(finalResponse) && (assistantText != "" || len(streamedOutputs) > 0) {
		finalResponse = buildAccumulatedStreamResponse(exec.responseID, exec.requestedModel, assistantText, streamedOutputs, exec.estimatedPromptTokens)
		return finalResponse
	}
	if finalResponse == nil {
		finalResponse = buildAccumulatedStreamResponse(exec.responseID, exec.requestedModel, assistantText, streamedOutputs, exec.estimatedPromptTokens)
	}
	return finalResponse
}

func (s *Service) emitStreamPayloadFromResponse(out chan<- provider.ResponseEvent, resp *provider.Response) {
	if resp == nil {
		return
	}
	for _, output := range resp.Output {
		switch output.Type {
		case "message":
			for _, content := range output.Content {
				if content.Text == "" {
					continue
				}
				out <- provider.ResponseEvent{
					Type:  provider.EventContentDelta,
					Delta: content.Text,
				}
			}
		case "function_call":
			item := output
			out <- provider.ResponseEvent{
				Type:   provider.EventToolCallDone,
				Output: &item,
			}
		}
	}
}

func hasRenderableStreamPayload(resp *provider.Response) bool {
	if resp == nil {
		return false
	}
	if resp.OutputText() != "" || len(resp.OutputToolCalls()) > 0 {
		return true
	}
	for _, output := range resp.Output {
		if output.Type == "message" {
			for _, content := range output.Content {
				if content.Text != "" {
					return true
				}
			}
		}
	}
	return false
}

func buildAccumulatedStreamResponse(responseID, model, assistantText string, streamedOutputs []provider.ResponseOutput, estimatedPromptTokens int) *provider.Response {
	outputs := make([]provider.ResponseOutput, 0, len(streamedOutputs)+1)
	if assistantText != "" {
		outputs = append(outputs, provider.ResponseOutput{
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
			Content: []provider.ResponseContent{{
				Type: "output_text",
				Text: assistantText,
			}},
		})
	}
	outputs = append(outputs, streamedOutputs...)
	if len(outputs) == 0 {
		return provider.NewTextResponse(responseID, model, "", provider.Usage{
			PromptTokens:     estimatedPromptTokens,
			CompletionTokens: 0,
			TotalTokens:      estimatedPromptTokens,
		})
	}
	return &provider.Response{
		ID:      responseID,
		Object:  "response",
		Created: time.Now().Unix(),
		Model:   model,
		Status:  "completed",
		Output:  outputs,
		Usage: provider.Usage{
			PromptTokens:     estimatedPromptTokens,
			CompletionTokens: provider.RoughTokenCount(assistantText),
			TotalTokens:      estimatedPromptTokens + provider.RoughTokenCount(assistantText),
		},
	}
}

func appendStreamedToolCalls(outputs []provider.ResponseOutput, calls []provider.ToolCall) []provider.ResponseOutput {
	for _, call := range calls {
		outputs = appendStreamOutput(outputs, &provider.ResponseOutput{
			ID:     call.ID,
			Type:   "function_call",
			Status: "completed",
			CallID: call.ID,
			Name:   call.Function.Name,
			Args:   call.Function.Arguments,
		})
	}
	return outputs
}

func appendStreamOutput(outputs []provider.ResponseOutput, output *provider.ResponseOutput) []provider.ResponseOutput {
	if output == nil {
		return outputs
	}
	key := firstNonEmptyLocal(output.ID, output.CallID)
	if output.Type == "function_call" && key != "" {
		for _, existing := range outputs {
			existingKey := firstNonEmptyLocal(existing.ID, existing.CallID)
			if existing.Type == output.Type && existingKey == key {
				return outputs
			}
		}
	}
	cloned := *output
	return append(outputs, cloned)
}

func firstNonEmptyLocal(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func isRenderableStreamEvent(event provider.ResponseEvent) bool {
	if event.Text() != "" {
		return true
	}
	if len(event.ToolCalls) > 0 {
		return true
	}
	if event.Output != nil && event.Output.Type == "function_call" {
		return true
	}
	return false
}

func applyRecoveredStreamUsage(resp *provider.Response, usage *provider.Usage) {
	if resp == nil || usage == nil {
		return
	}
	if usage.PromptTokens > 0 {
		resp.Usage.PromptTokens = usage.PromptTokens
	}
	if usage.CompletionTokens > 0 {
		resp.Usage.CompletionTokens = usage.CompletionTokens
	}
	if usage.TotalTokens > 0 {
		resp.Usage.TotalTokens = usage.TotalTokens
	} else if resp.Usage.PromptTokens > 0 && resp.Usage.CompletionTokens > 0 {
		resp.Usage.TotalTokens = resp.Usage.PromptTokens + resp.Usage.CompletionTokens
	}
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
		Model:             req.Model, // 透传请求中的模型名
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
			// Drain upstream so we capture final usage even after disconnect.
			s.drainStreamForUsage(stream, upstreamErrCh, &finalResponse, &streamUsage, &streamedOutputs, &assistantText)
			s.handleStreamCancellation(ctx, identity, exec.upstreamRequest, exec.responseID, exec.provider, exec.routeTrace, finalResponse, assistantText, streamedOutputs, streamUsage, exec.startedAt)
			return
		}
	}
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

func detachedPersistenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.WithoutCancel(ctx)
	return context.WithTimeout(base, terminalPersistenceTimeout)
}

func (s *Service) persistSuccess(ctx context.Context, identity *repository.AuthIdentity, exec *execution, resp *provider.Response, latencyMs int64) error {
	providerName := exec.provider.Name()
	model := exec.requestedModel
	cost := s.computeCost(exec.provider, exec.requestedModel, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	if err := s.ensureQuotaAvailable(ctx, identity, exec, resp, latencyMs, providerName, cost); err != nil {
		return err
	}

	// RecordUsage stays synchronous: it consumes quota / budget atomically
	// and must signal exceeded states back to the caller before we mark
	// the response completed. If we async this, two parallel callers could
	// both observe ok=true under a near-empty quota and silently overshoot.
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

func (s *Service) normalizeResponse(exec *execution, resp *provider.Response) *provider.Response {
	if resp == nil {
		resp = provider.NewTextResponse(exec.responseID, exec.requestedModel, "", provider.Usage{})
	}
	if resp.ID == "" {
		resp.ID = exec.responseID
	} else {
		resp.ID = exec.responseID
	}
	resp.Object = "response"
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

func validateVisibleOutputBudget(exec *execution, resp *provider.Response) error {
	if exec == nil || exec.upstreamRequest == nil || resp == nil {
		return nil
	}
	requested := exec.upstreamRequest.RequestedMaxTokens()
	if requested <= 0 {
		return nil
	}
	if hasVisibleOutput(resp) {
		return nil
	}
	if !thinkingOnlyResponse(resp) {
		if requested > 128 {
			return nil
		}
		return fmt.Errorf(
			"%w: upstream produced no visible output; requested_tokens=%d completion_tokens=%d; increase max_tokens/max_output_tokens",
			ErrOutputBudgetTooLow,
			requested,
			resp.Usage.CompletionTokens,
		)
	}
	if !nearOutputBudgetLimit(resp.Usage.CompletionTokens, requested) {
		return nil
	}
	return fmt.Errorf(
		"%w: upstream produced only thinking blocks and no visible output; requested_tokens=%d completion_tokens=%d; increase max_tokens/max_output_tokens",
		ErrOutputBudgetTooLow,
		requested,
		resp.Usage.CompletionTokens,
	)
}

func hasVisibleOutput(resp *provider.Response) bool {
	if resp == nil {
		return false
	}
	return resp.OutputText() != "" || len(resp.OutputToolCalls()) > 0
}

func thinkingOnlyResponse(resp *provider.Response) bool {
	if resp == nil {
		return false
	}
	hasThinking := false
	for _, output := range resp.Output {
		if output.Type == "function_call" {
			return false
		}
		for _, content := range output.Content {
			if content.Text != "" || content.Refusal != "" {
				return false
			}
			if content.Thinking != "" {
				hasThinking = true
			}
		}
	}
	return hasThinking
}

func nearOutputBudgetLimit(actual, requested int) bool {
	if actual <= 0 || requested <= 0 {
		return false
	}
	threshold := int(math.Ceil(float64(requested) * 0.9))
	if threshold < 1 {
		threshold = 1
	}
	return actual >= threshold
}

func (s *Service) selectProvider(ctx context.Context, identity *repository.AuthIdentity, sessionID string, req *provider.ResponseRequest) (provider.Provider, error) {
	candidates, _ := s.planCandidates(ctx, identity, sessionID, req)
	if len(candidates) == 0 {
		return nil, ErrNoProvider
	}
	return candidates[0], nil
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
	traceID := "unknown"
	if parentSpan, ok := trace.SpanFromContext(ctx); ok {
		traceID = parentSpan.TraceID
	}
	ctx = trace.StartSpan(ctx, traceID, "provider_call")
	defer trace.FinishSpan(ctx, map[string]string{
		"provider": exec.provider.Name(),
		"model":    exec.provider.Model(),
	})

	ctx, otelSpan := middleware.StartSpan(ctx, "provider_call",
		attribute.String("provider", exec.provider.Name()),
		attribute.String("model", exec.provider.Model()),
	)
	defer otelSpan.End()

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

func (s *Service) getCandidateProviders(ctx context.Context, identity *repository.AuthIdentity, sessionID string, req *provider.ResponseRequest) []provider.Provider {
	candidates, _ := s.planCandidates(ctx, identity, sessionID, req)
	return candidates
}

func modelRequiredButUnavailable(req *provider.ResponseRequest, all []provider.Provider, filtered []provider.Provider) bool {
	if req == nil || req.Model == "" {
		return false
	}
	hadModelMatch := false
	for _, item := range all {
		if item.Model() == req.Model {
			hadModelMatch = true
			break
		}
	}
	if !hadModelMatch {
		return false
	}
	for _, item := range filtered {
		if item.Model() == req.Model {
			return false
		}
	}
	return true
}

func buildRouteContext(req *provider.ResponseRequest, sessionID string) router.RouteContext {
	if req == nil {
		return router.RouteContext{SessionID: sessionID}
	}
	req.Normalize()
	return router.RouteContext{
		Model:               req.Model,
		SessionID:           sessionID,
		InputText:           req.InputText(),
		PromptTokens:        req.EstimatePromptTokens(),
		Stream:              req.Stream,
		HasTools:            req.HasToolsRequested(),
		HasImages:           req.HasImageInput(),
		HasStructuredOutput: req.HasStructuredOutputRequest(),
	}
}

func WrapError(err error) ginError {
	switch {
	case errors.Is(err, auth.ErrModelNotAllowed):
		return ginError{Status: 403, Message: err.Error(), Type: "invalid_request_error"}
	case errors.Is(err, auth.ErrQuotaExceeded):
		return ginError{Status: 429, Message: err.Error(), Type: "rate_limit_error"}
	case errors.Is(err, auth.ErrBudgetExceeded):
		return ginError{Status: 429, Message: err.Error(), Type: "rate_limit_error"}
	case errors.Is(err, ErrOutputBudgetTooLow):
		return ginError{Status: 400, Message: err.Error(), Type: "invalid_request_error"}
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

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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
