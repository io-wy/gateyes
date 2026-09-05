// Package inference owns the application-level inference workflow.  It is
// deliberately expressed in ports so transport code and the legacy response
// service do not need to know about provider, cache, or persistence details.
package inference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

var (
	ErrNoProvider                 = errors.New("no provider available")
	ErrProductionExecutorRequired = errors.New("production inference executor is required")
)

type Dependencies struct {
	// Executor is the production inference engine used while its mature
	// circuit-breaker, caching, billing, plugin, and stream semantics are
	// incrementally moved behind the application ports below.
	Executor     ProductionExecutor
	Admission    AdmissionPolicy
	Router       ProviderRouter
	Invoker      ProviderInvoker
	Cache        StreamCache
	Usage        UsageRecorder
	Plugins      PluginHooks
	Repository   ResponseRepository
	Retry        RetryPolicy
	DrainTimeout time.Duration
}

type Orchestrator struct{ deps Dependencies }

// Execute is the migration-safe application entry point. A configured
// production executor retains the complete legacy behavior while the smaller
// workflow ports are adopted incrementally.
func (o *Orchestrator) Execute(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*responseSvc.CreateResult, error) {
	if o.deps.Executor != nil {
		return o.deps.Executor.Create(ctx, identity, req, sessionID)
	}
	return nil, ErrProductionExecutorRequired
}

func (o *Orchestrator) ExecuteStream(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*responseSvc.Stream, error) {
	if o.deps.Executor != nil {
		return o.deps.Executor.CreateStream(ctx, identity, req, sessionID)
	}
	return nil, ErrProductionExecutorRequired
}

func (o *Orchestrator) GetCircuitBreakerStates() map[string]int {
	if o.deps.Executor == nil {
		return nil
	}
	return o.deps.Executor.GetCircuitBreakerStates()
}

func (o *Orchestrator) PersistCircuitBreakerState(ctx context.Context) {
	if o.deps.Executor != nil {
		o.deps.Executor.PersistCircuitBreakerState(ctx)
	}
}

func NewOrchestrator(deps Dependencies) *Orchestrator {
	if deps.Retry.MaxAttempts < 1 {
		deps.Retry.MaxAttempts = 1
	}
	if deps.DrainTimeout <= 0 {
		deps.DrainTimeout = 5 * time.Second
	}
	if deps.Invoker == nil {
		deps.Invoker = providerInvoker{}
	}
	return &Orchestrator{deps: deps}
}

type Result struct {
	Response     *provider.Response
	ProviderName string
	Latency      time.Duration
	Retries      int
	Fallback     int
}

func (o *Orchestrator) Create(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*Result, error) {
	if req == nil {
		return nil, errors.New("nil inference request")
	}
	if o.deps.Admission != nil {
		if err := o.deps.Admission.Admit(ctx, identity, req); err != nil {
			return nil, err
		}
	}
	if o.deps.Plugins != nil {
		var err error
		req, err = o.deps.Plugins.Before(ctx, req)
		if err != nil {
			return nil, err
		}
	}
	started := time.Now()
	if o.deps.Cache != nil {
		if entry, hit, err := o.deps.Cache.Lookup(ctx, identity, req); err != nil {
			return nil, err
		} else if hit {
			var resp provider.Response
			if err := json.Unmarshal(entry.Response, &resp); err != nil {
				return nil, fmt.Errorf("decode cached response: %w", err)
			}
			return &Result{Response: &resp, ProviderName: entry.Provider, Latency: time.Since(started)}, nil
		}
	}
	if o.deps.Router == nil {
		return nil, ErrNoProvider
	}
	candidates, err := o.deps.Router.Plan(ctx, identity, req, sessionID)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, ErrNoProvider
	}
	responseID := fmt.Sprintf("inference-%d", started.UnixNano())
	if o.deps.Repository != nil {
		if err := o.deps.Repository.Create(ctx, repository.ResponseRecord{ID: responseID, TenantID: identity.TenantID, ProjectID: identity.ProjectID, UserID: identity.UserID, APIKeyID: identity.APIKeyID, ProviderName: candidates[0].Name(), Model: req.Model, Status: "in_progress"}); err != nil {
			return nil, err
		}
	}
	var lastErr error
	retries, fallback := 0, 0
	for idx, candidate := range candidates {
		attempts := o.deps.Retry.MaxAttempts
		for attempt := 0; attempt < attempts; attempt++ {
			resp, callErr := o.deps.Invoker.Invoke(ctx, candidate, req)
			if callErr == nil && resp != nil {
				if o.deps.Plugins != nil {
					resp, callErr = o.deps.Plugins.After(ctx, resp)
				}
				if callErr == nil {
					latency := time.Since(started)
					if o.deps.Usage != nil {
						if err := o.deps.Usage.Record(ctx, identity, candidate.Name(), resp, latency, nil); err != nil {
							callErr = err
						}
					}
					if callErr == nil {
						if o.deps.Repository != nil {
							_ = o.deps.Repository.Complete(ctx, repository.ResponseRecord{ID: responseID, TenantID: identity.TenantID, ProviderName: candidate.Name(), Model: req.Model, Status: "completed"})
						}
						if o.deps.Cache != nil {
							body, _ := json.Marshal(resp)
							_ = o.deps.Cache.Store(ctx, identity, req, &cache.Entry{Response: body, Provider: candidate.Name(), Model: req.Model})
						}
						if o.deps.Plugins != nil {
							o.deps.Plugins.Audit(context.WithoutCancel(ctx), resp)
						}
						return &Result{Response: resp, ProviderName: candidate.Name(), Latency: latency, Retries: retries, Fallback: fallback}, nil
					}
				}
			}
			lastErr = callErr
			if attempt+1 < attempts {
				retries++
				if o.deps.Retry.InitialDelay > 0 {
					timer := time.NewTimer(o.deps.Retry.InitialDelay)
					select {
					case <-ctx.Done():
						timer.Stop()
						return nil, ctx.Err()
					case <-timer.C:
					}
				}
			}
		}
		if idx+1 < len(candidates) {
			fallback++
		}
	}
	if o.deps.Repository != nil {
		_ = o.deps.Repository.Fail(ctx, repository.ResponseRecord{ID: responseID, TenantID: identity.TenantID, Model: req.Model, Status: "error"})
	}
	if lastErr == nil {
		lastErr = ErrNoProvider
	}
	return nil, lastErr
}

type StreamResult struct {
	ResponseID   string
	ProviderName string
	Events       <-chan provider.ResponseEvent
	Errors       <-chan error
	Drained      bool
}

func (o *Orchestrator) CreateStream(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*StreamResult, error) {
	if req == nil {
		return nil, errors.New("nil inference request")
	}
	if o.deps.Admission != nil {
		if err := o.deps.Admission.Admit(ctx, identity, req); err != nil {
			return nil, err
		}
	}
	if o.deps.Plugins != nil {
		var err error
		req, err = o.deps.Plugins.Before(ctx, req)
		if err != nil {
			return nil, err
		}
	}
	if o.deps.Cache != nil {
		if entry, hit, err := o.deps.Cache.Lookup(ctx, identity, req); err != nil {
			return nil, err
		} else if hit {
			return o.replayCachedStream(ctx, identity, req, entry)
		}
	}
	if o.deps.Router == nil {
		return nil, ErrNoProvider
	}
	candidates, err := o.deps.Router.Plan(ctx, identity, req, sessionID)
	if err != nil || len(candidates) == 0 {
		if err != nil {
			return nil, err
		}
		return nil, ErrNoProvider
	}
	events := make(chan provider.ResponseEvent)
	errs := make(chan error, 1)
	result := &StreamResult{ResponseID: fmt.Sprintf("stream-%d", time.Now().UnixNano()), ProviderName: candidates[0].Name(), Events: events, Errors: errs}
	go o.runStream(ctx, identity, req, candidates[0], result, events, errs)
	return result, nil
}

func (o *Orchestrator) replayCachedStream(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, entry *cache.Entry) (*StreamResult, error) {
	if entry == nil {
		return nil, errors.New("cache returned nil entry")
	}
	result := &StreamResult{ResponseID: fmt.Sprintf("stream-%d", time.Now().UnixNano()), ProviderName: entry.Provider}
	events := make(chan provider.ResponseEvent, 2)
	errs := make(chan error, 1)
	result.Events, result.Errors = events, errs
	go func() {
		defer close(events)
		defer close(errs)
		started := &provider.Response{ID: result.ResponseID, Object: "response", Model: req.Model, Status: "in_progress"}
		events <- provider.ResponseEvent{Type: provider.EventResponseStarted, Response: started}
		var response provider.Response
		if err := json.Unmarshal(entry.Response, &response); err != nil {
			errs <- fmt.Errorf("decode cached response: %w", err)
			return
		}
		response.ID, response.Model, response.Status = result.ResponseID, req.Model, "completed"
		events <- provider.ResponseEvent{Type: provider.EventResponseCompleted, Response: &response}
		if o.deps.Repository != nil {
			body, _ := json.Marshal(response)
			_ = o.deps.Repository.Complete(context.WithoutCancel(ctx), repository.ResponseRecord{ID: result.ResponseID, TenantID: identity.TenantID, ProviderName: entry.Provider, Model: req.Model, Status: "completed", ResponseBody: body})
		}
	}()
	return result, nil
}

func (o *Orchestrator) runStream(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, candidate provider.Provider, result *StreamResult, out chan<- provider.ResponseEvent, errs chan<- error) {
	defer close(out)
	defer close(errs)
	streamCtx := context.WithoutCancel(ctx)
	stream, upstreamErrs := o.deps.Invoker.Stream(streamCtx, candidate, req)
	if stream == nil {
		err := errors.New("provider returned nil stream")
		o.finishStream(ctx, identity, req, candidate, result, nil, nil, err)
		errs <- err
		return
	}
	var final *provider.Response
	var usage *provider.Usage
	for {
		select {
		case event, ok := <-stream:
			if !ok {
				o.finishStream(ctx, identity, req, candidate, result, final, usage, nil)
				return
			}
			if event.Response != nil {
				final = event.Response
			}
			if event.Usage != nil {
				cp := *event.Usage
				usage = &cp
			}
			select {
			case out <- event:
			case <-ctx.Done():
				o.drain(stream, upstreamErrs, &final, &usage, result)
				o.finishStream(context.WithoutCancel(ctx), identity, req, candidate, result, final, usage, ctx.Err())
				errs <- ctx.Err()
				return
			}
		case err, ok := <-upstreamErrs:
			if ok && err != nil {
				o.finishStream(ctx, identity, req, candidate, result, final, usage, err)
				errs <- err
			}
			return
		case <-ctx.Done():
			o.drain(stream, upstreamErrs, &final, &usage, result)
			o.finishStream(context.WithoutCancel(ctx), identity, req, candidate, result, final, usage, ctx.Err())
			errs <- ctx.Err()
			return
		}
	}
}

func (o *Orchestrator) drain(stream <-chan provider.ResponseEvent, upstreamErrs <-chan error, final **provider.Response, usage **provider.Usage, result *StreamResult) {
	timer := time.NewTimer(o.deps.DrainTimeout)
	defer timer.Stop()
	result.Drained = true
	for {
		select {
		case event, ok := <-stream:
			if !ok {
				return
			}
			if event.Response != nil {
				*final = event.Response
			}
			if event.Usage != nil {
				cp := *event.Usage
				*usage = &cp
			}
		case _, ok := <-upstreamErrs:
			if !ok {
				return
			}
		case <-timer.C:
			return
		}
	}
}

func (o *Orchestrator) finishStream(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, candidate provider.Provider, result *StreamResult, resp *provider.Response, usage *provider.Usage, streamErr error) {
	if resp == nil {
		resp = &provider.Response{Model: req.Model}
		if usage != nil {
			resp.Usage = *usage
		}
	}
	if o.deps.Usage != nil {
		_ = o.deps.Usage.Record(ctx, identity, candidate.Name(), resp, 0, streamErr)
	}
	if o.deps.Repository == nil {
		return
	}
	record := repository.ResponseRecord{ID: result.ResponseID, TenantID: identity.TenantID, ProjectID: identity.ProjectID, ProviderName: candidate.Name(), Model: req.Model, Status: "completed"}
	if streamErr != nil {
		record.Status = "error"
		_ = o.deps.Repository.Fail(ctx, record)
	} else {
		_ = o.deps.Repository.Complete(ctx, record)
	}
}

type providerInvoker struct{}

func (providerInvoker) Invoke(ctx context.Context, p provider.Provider, req *provider.ResponseRequest) (*provider.Response, error) {
	return p.CreateResponse(ctx, req)
}
func (providerInvoker) Stream(ctx context.Context, p provider.Provider, req *provider.ResponseRequest) (<-chan provider.ResponseEvent, <-chan error) {
	return p.StreamResponse(ctx, req)
}
