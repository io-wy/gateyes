package responses

import (
	"context"
	"time"

	"github.com/gateyes/gateway/internal/config"
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
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

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
	EventBus    *eventbus.Bus
	Guardrails  *guardrail.Manager
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
	sfg            singleflight.Group
	drainSem       chan struct{}
	persistSem     chan struct{}
}

type CreateResult struct {
	Response         *provider.Response
	ProviderName     string
	LatencyMs        int64
	PromptTokens     int
	CompletionTokens int
	Retries          int
	Fallback         int
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
		drainSem:       make(chan struct{}, 100),
		persistSem:     make(chan struct{}, 50),
	}
}

// SetRedis enables Redis-backed features (circuit breaker persistence).
func (s *Service) SetRedis(rdb *redis.Client) {
	if s.circuitBreaker != nil {
		s.circuitBreaker.SetRedis(rdb)
	}
}

// RestoreCircuitBreakerState loads circuit breaker states from Redis after restart.
func (s *Service) RestoreCircuitBreakerState(ctx context.Context) {
	if s.circuitBreaker != nil {
		s.circuitBreaker.RestoreState(ctx)
	}
}

// PersistCircuitBreakerState saves circuit breaker states to Redis.
func (s *Service) PersistCircuitBreakerState(ctx context.Context) {
	if s.circuitBreaker != nil {
		s.circuitBreaker.PersistState(ctx)
	}
}

// drainWithSemaphore limits the number of concurrent stream drain goroutines
// to prevent memory exhaustion during mass client disconnections.
func (s *Service) drainWithSemaphore(
	stream <-chan provider.ResponseEvent,
	upstreamErrCh <-chan error,
	finalResponse **provider.Response,
	streamUsage **provider.Usage,
	streamedOutputs *[]provider.ResponseOutput,
	assistantText *string,
) {
	select {
	case s.drainSem <- struct{}{}:
		s.drainStreamForUsage(stream, upstreamErrCh, finalResponse, streamUsage, streamedOutputs, assistantText)
		<-s.drainSem
	default:
		// semaphore full: skip drain to avoid goroutine explosion
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

func (s *Service) selectProvider(ctx context.Context, identity *repository.AuthIdentity, sessionID string, req *provider.ResponseRequest) (provider.Provider, error) {
	candidates, _ := s.planCandidates(ctx, identity, sessionID, req)
	if len(candidates) == 0 {
		return nil, ErrNoProvider
	}
	return candidates[0], nil
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

// GetCircuitBreakerStates returns all circuit breaker states for metrics collection
func (s *Service) GetCircuitBreakerStates() map[string]int {
	if s.circuitBreaker == nil {
		return nil
	}
	return s.circuitBreaker.GetAllStates()
}
