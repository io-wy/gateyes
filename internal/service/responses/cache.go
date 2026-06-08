package responses

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/provider"
)

// CacheMetrics is the subset of metrics methods used by the responses service.
// It is defined here to avoid an import cycle with internal/transport/http/handler.
type CacheMetrics interface {
	RecordCacheLookup(layer, result string)
	RecordCacheWrite(layer, result string)
	ObserveCacheValueSize(layer string, size int)
	ObserveCacheGetDuration(layer string, d time.Duration)
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
	if hint := CacheHintsFrom(ctx).TTL; hint > 0 {
		ttl = hint
	}
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
