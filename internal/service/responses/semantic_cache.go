package responses

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/provider"
)

type semanticEmbeddingProvider interface {
	CreateEmbedding(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error)
}

type semanticCacheMaterial struct {
	scope           ServiceCacheScope
	surface         string
	model           string
	embeddingModel  string
	promptHash      string
	promptCanonical []byte
	promptText      string
	embedding       []float64
}

func (s *Service) semanticCacheLayer(stream bool) string {
	if stream {
		return cache.LayerL2SemanticStream
	}
	return cache.LayerL2Semantic
}

func (s *Service) lookupSemanticCache(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest) (*cache.Entry, bool, *semanticCacheMaterial) {
	layer := s.semanticCacheLayer(req != nil && req.Stream)
	start := time.Now()
	material, reason, err := s.buildSemanticCacheMaterial(ctx, identity, req)
	if reason == "semantic_disabled" {
		return nil, false, nil
	}
	if reason != "" {
		setCacheTrace(ctx, CacheResultSkip, layer, reason, "")
		if s.metrics != nil {
			s.metrics.RecordCacheLookup(layer, CacheResultSkip)
		}
		return nil, false, nil
	}
	if err != nil {
		setCacheTrace(ctx, CacheResultError, layer, err.Error(), "")
		if s.metrics != nil {
			s.metrics.RecordCacheLookup(layer, CacheResultError)
			s.metrics.ObserveCacheGetDuration(layer, time.Since(start))
		}
		return nil, false, nil
	}

	filter := s.semanticCacheFilter(identity, material)
	candidates, err := s.semanticCache.FindSemanticCacheCandidates(ctx, filter, material.embedding)
	if err != nil {
		setCacheTrace(ctx, CacheResultError, layer, err.Error(), "")
		if s.metrics != nil {
			s.metrics.RecordCacheLookup(layer, CacheResultError)
			s.metrics.ObserveCacheGetDuration(layer, time.Since(start))
		}
		return nil, false, material
	}
	threshold := semanticThreshold(s.cfg.Cache.Semantic)
	for _, candidate := range candidates {
		if candidate.Similarity < threshold {
			continue
		}
		entry := semanticRecordToCacheEntry(candidate.SemanticCacheRecord, req.Stream)
		if entry == nil || len(entry.Response) == 0 {
			continue
		}
		key := "semantic:" + candidate.ID
		setCacheTrace(ctx, CacheResultHit, layer, "", key)
		setSemanticCacheTrace(ctx, candidate.ID, candidate.Similarity, threshold, material.embeddingModel)
		if s.metrics != nil {
			s.metrics.RecordCacheLookup(layer, CacheResultHit)
			s.metrics.ObserveCacheGetDuration(layer, time.Since(start))
			s.metrics.ObserveCacheValueSize(layer, cacheEntryValueSize(entry))
		}
		s.recordSemanticCacheHit(ctx, candidate.SemanticCacheRecord)
		s.writeCache(ctx, identity, req, entry)
		return entry, true, material
	}

	setCacheTrace(ctx, CacheResultMiss, layer, "", material.promptHash)
	if s.metrics != nil {
		s.metrics.RecordCacheLookup(layer, CacheResultMiss)
		s.metrics.ObserveCacheGetDuration(layer, time.Since(start))
	}
	return nil, false, material
}

func setSemanticCacheTrace(ctx context.Context, entryID string, similarity, threshold float64, embeddingModel string) {
	trace := CacheTraceFrom(ctx)
	if trace == nil {
		return
	}
	trace.EntryID = entryID
	trace.Similarity = similarity
	trace.Threshold = threshold
	trace.EmbeddingModel = embeddingModel
}

func (s *Service) writeSemanticCache(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, providerName string, resp *provider.Response, material *semanticCacheMaterial, transcript []cache.StreamEvent) {
	if resp == nil {
		return
	}
	layer := s.semanticCacheLayer(req != nil && req.Stream)
	if material == nil {
		var reason string
		var err error
		material, reason, err = s.buildSemanticCacheMaterial(ctx, identity, req)
		if reason != "" || err != nil {
			if err != nil && s.metrics != nil {
				s.metrics.RecordCacheWrite(layer, CacheResultError)
			}
			return
		}
	}
	body, err := json.Marshal(resp)
	if err != nil {
		if s.metrics != nil {
			s.metrics.RecordCacheWrite(layer, CacheResultError)
		}
		return
	}
	usageBody, _ := json.Marshal(resp.Usage)
	params := repository.CreateSemanticCacheParams{
		TenantID:            identity.TenantID,
		ProjectID:           identity.ProjectID,
		ServiceID:           material.scope.ServiceID,
		APIKeyID:            identity.APIKeyID,
		Surface:             material.surface,
		Model:               material.model,
		EmbeddingModel:      material.embeddingModel,
		PromptHash:          material.promptHash,
		PromptCanonical:     append([]byte(nil), material.promptCanonical...),
		PromptText:          material.promptText,
		Embedding:           append([]float64(nil), material.embedding...),
		ResponseBody:        body,
		StreamBody:          semanticStreamBody(transcript),
		ProviderName:        providerName,
		UsageBody:           usageBody,
		SimilarityThreshold: semanticThreshold(s.cfg.Cache.Semantic),
		ExpiresAt:           semanticCacheExpiresAt(s.cfg.Cache),
	}
	write := func(workCtx context.Context) {
		if _, err := s.semanticCache.CreateSemanticCacheEntry(workCtx, params); err != nil {
			if s.metrics != nil {
				s.metrics.RecordCacheWrite(layer, CacheResultError)
			}
			return
		}
		if s.metrics != nil {
			s.metrics.RecordCacheWrite(layer, "success")
			s.metrics.ObserveCacheValueSize(layer, len(body)+len(params.StreamBody))
		}
	}
	if s.cfg.Cache.Semantic.WriteAsync {
		go func() {
			workCtx, cancel := detachedPersistenceContext(ctx)
			defer cancel()
			write(workCtx)
		}()
		return
	}
	workCtx, cancel := detachedPersistenceContext(ctx)
	defer cancel()
	write(workCtx)
}

func (s *Service) buildSemanticCacheMaterial(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest) (*semanticCacheMaterial, string, error) {
	if s == nil || s.cfg == nil || !s.cfg.Cache.Enabled || !s.cfg.Cache.Semantic.Enabled {
		return nil, "semantic_disabled", nil
	}
	if s.semanticCache == nil {
		return nil, "semantic_store_unavailable", nil
	}
	if s.embedding == nil {
		return nil, "embedding_provider_unavailable", nil
	}
	if identity == nil || req == nil {
		return nil, "invalid_request", nil
	}
	if req.Stream && !s.cfg.Cache.Semantic.AllowStream {
		return nil, "stream_disabled", nil
	}
	if skip, reason := s.cacheSkipReason(ctx, req); skip {
		return nil, reason, nil
	}
	if !semanticSurfaceAllowed(s.cfg.Cache.Semantic, req.Surface) {
		return nil, "surface_not_allowed", nil
	}
	if hint := SemanticCacheHintsFrom(ctx); hint.Skip {
		return nil, "request_header", nil
	}
	scope, _ := ServiceCacheScopeFrom(ctx)
	hints := SemanticCacheHintsFrom(ctx)
	if s.cfg.Cache.Semantic.RequireServiceOptIn && !hints.Enable && !serviceSemanticCacheOptedIn(scope.Metadata) {
		return nil, "service_opt_in_required", nil
	}
	embeddingModel := strings.TrimSpace(s.cfg.Cache.Semantic.EmbeddingModel)
	if embeddingModel == "" {
		return nil, "embedding_model_required", nil
	}
	canonical := s.buildCachePromptCanonical(ctx, identity, req)
	if len(canonical) == 0 {
		return nil, "empty_prompt", nil
	}
	input, _ := json.Marshal(string(canonical))
	embed, err := s.embedding.CreateEmbedding(ctx, &provider.EmbeddingRequest{
		Model: embeddingModel,
		Input: input,
	})
	if err != nil {
		return nil, "", err
	}
	if embed == nil || len(embed.Data) == 0 || len(embed.Data[0].Embedding) == 0 {
		return nil, "empty_embedding", nil
	}
	hashBytes := sha256.Sum256(canonical)
	surface := strings.TrimSpace(req.Surface)
	if surface == "" {
		surface = "responses"
	}
	return &semanticCacheMaterial{
		scope:           scope,
		surface:         surface,
		model:           req.Model,
		embeddingModel:  embeddingModel,
		promptHash:      hex.EncodeToString(hashBytes[:]),
		promptCanonical: canonical,
		promptText:      strings.TrimSpace(req.InputText()),
		embedding:       append([]float64(nil), embed.Data[0].Embedding...),
	}, "", nil
}

func (s *Service) semanticCacheFilter(identity *repository.AuthIdentity, material *semanticCacheMaterial) repository.SemanticCacheFilter {
	filter := repository.SemanticCacheFilter{
		TenantID:       identity.TenantID,
		ProjectID:      identity.ProjectID,
		ServiceID:      material.scope.ServiceID,
		Surface:        material.surface,
		Model:          material.model,
		EmbeddingModel: material.embeddingModel,
		Limit:          semanticMaxCandidates(s.cfg.Cache.Semantic),
	}
	disabled := false
	filter.Disabled = &disabled
	return filter
}

func semanticRecordToCacheEntry(record repository.SemanticCacheRecord, stream bool) *cache.Entry {
	if len(record.ResponseBody) == 0 {
		return nil
	}
	var usage cache.Usage
	if len(record.UsageBody) > 0 {
		_ = json.Unmarshal(record.UsageBody, &usage)
	}
	return &cache.Entry{
		Response:         append([]byte(nil), record.ResponseBody...),
		StreamRaw:        append([]byte(nil), record.StreamBody...),
		StreamTranscript: decodeStreamTranscript(record.StreamBody),
		Stream:           stream,
		Model:            record.Model,
		Provider:         record.ProviderName,
		Usage:            usage,
		CreatedAt:        record.CreatedAt.Unix(),
	}
}

func semanticStreamBody(transcript []cache.StreamEvent) []byte {
	if len(transcript) == 0 {
		return nil
	}
	body, err := json.Marshal(transcript)
	if err != nil {
		return nil
	}
	return body
}

func decodeStreamTranscript(raw []byte) []cache.StreamEvent {
	if len(raw) == 0 {
		return nil
	}
	var transcript []cache.StreamEvent
	if err := json.Unmarshal(raw, &transcript); err == nil && len(transcript) > 0 {
		return transcript
	}
	return nil
}

func (s *Service) recordSemanticCacheHit(ctx context.Context, record repository.SemanticCacheRecord) {
	if s == nil || s.semanticCache == nil || record.ID == "" || record.TenantID == "" {
		return
	}
	hitAt := time.Now().UTC()
	hitCount := record.HitCount + 1
	go func() {
		workCtx, cancel := detachedPersistenceContext(ctx)
		defer cancel()
		_, _ = s.semanticCache.UpdateSemanticCacheEntry(workCtx, record.TenantID, record.ID, repository.UpdateSemanticCacheParams{
			LastHitAt: &hitAt,
			HitCount:  &hitCount,
		})
	}()
}

func semanticThreshold(cfg config.SemanticCacheConfig) float64 {
	if cfg.Threshold <= 0 {
		return 0.92
	}
	return cfg.Threshold
}

func semanticMaxCandidates(cfg config.SemanticCacheConfig) int {
	if cfg.MaxCandidates <= 0 {
		return 5
	}
	return cfg.MaxCandidates
}

func semanticCacheExpiresAt(cfg config.CacheConfig) time.Time {
	ttl := cfg.Semantic.TTLSeconds
	if ttl <= 0 {
		ttl = cfg.DefaultTTL
	}
	if ttl <= 0 {
		ttl = 3600
	}
	return time.Now().UTC().Add(time.Duration(ttl) * time.Second)
}

func semanticSurfaceAllowed(cfg config.SemanticCacheConfig, surface string) bool {
	if len(cfg.AllowedSurfaces) == 0 {
		return true
	}
	surface = strings.ToLower(strings.TrimSpace(surface))
	for _, allowed := range cfg.AllowedSurfaces {
		if strings.ToLower(strings.TrimSpace(allowed)) == surface {
			return true
		}
	}
	return false
}

func serviceSemanticCacheOptedIn(metadata map[string]any) bool {
	if len(metadata) == 0 {
		return false
	}
	for _, key := range []string{"semantic_cache", "semanticCache", "semantic_cache_enabled", "semanticCacheEnabled"} {
		if value, ok := metadata[key]; ok {
			return truthyMetadataValue(value)
		}
	}
	if cacheValue, ok := metadata["cache"].(map[string]any); ok {
		if semantic, ok := cacheValue["semantic"].(map[string]any); ok {
			return truthyMetadataValue(semantic["enabled"])
		}
		if semantic, ok := cacheValue["semantic"]; ok {
			return truthyMetadataValue(semantic)
		}
	}
	return false
}

func truthyMetadataValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return parseTruthy(typed)
	case float64:
		return typed > 0
	case int:
		return typed > 0
	case map[string]any:
		return truthyMetadataValue(typed["enabled"])
	default:
		return false
	}
}
