package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	pluginSvc "github.com/gateyes/gateway/internal/domain/plugin"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
	"github.com/gateyes/gateway/internal/pkg/trace"
	"go.opentelemetry.io/otel/attribute"
)

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
	respCopy := *r.resp
	return &respCopy, r.retries, nil
}

func isRetryable(err error) bool {
	if err == nil {
		return true
	}

	if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrForbidden) {
		return false
	}

	var upstreamErr *provider.UpstreamError
	if errors.As(err, &upstreamErr) {
		return upstreamErr.IsRetryable()
	}

	errMsg := err.Error()

	if strings.Contains(errMsg, "401") || strings.Contains(errMsg, "403") || strings.Contains(errMsg, "400") ||
		strings.Contains(errMsg, "422") || strings.Contains(errMsg, "404") {
		return false
	}

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

	ctx, otelSpan := trace.StartOTelSpan(ctx, "provider_call",
		attribute.String("provider", exec.provider.Name()),
		attribute.String("model", exec.provider.Model()),
	)
	defer otelSpan.End()

	retryCfg := s.cfg.Retry
	var lastErr error
	retryCount := 0

	for i := 0; i <= retryCfg.MaxRetries; i++ {
		// pre_upstream: plugins can modify the request or return cache_hit.
		prePayload := map[string]any{"request": exec.upstreamRequest}
		if cmd := s.invokePlugins(ctx, pluginSvc.PreUpstream, prePayload, traceID, exec.tenantID, "", exec.provider.Model(), exec.upstreamRequest.Stream); cmd != nil {
			switch cmd.Action {
			case "BLOCK":
				return nil, retryCount, fmt.Errorf("plugin blocked: %s", cmd.Reason)
			case "TRANSFORM":
				// Parse transformed request from payload.
				var transformed provider.ResponseRequest
				if err := json.Unmarshal(cmd.Payload, &transformed); err == nil {
					exec.upstreamRequest = &transformed
				}
			case "CACHE_HIT":
				var cached provider.Response
				if err := json.Unmarshal(cmd.Payload, &cached); err == nil {
					return &cached, retryCount, nil
				}
			}
		}

		resp, err := exec.provider.CreateResponse(ctx, exec.upstreamRequest)

		if err == nil {
			// post_upstream: plugins can modify the response.
			postPayload := map[string]any{"response": resp}
			if cmd := s.invokePlugins(ctx, pluginSvc.PostUpstream, postPayload, traceID, exec.tenantID, "", exec.provider.Model(), exec.upstreamRequest.Stream); cmd != nil {
				switch cmd.Action {
				case "BLOCK":
					return nil, retryCount, fmt.Errorf("plugin blocked: %s", cmd.Reason)
				case "TRANSFORM":
					var transformed provider.Response
					if err := json.Unmarshal(cmd.Payload, &transformed); err == nil {
						resp = &transformed
					}
				}
			}
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
