package responses

import "context"

const (
	CacheResultHit   = "hit"
	CacheResultMiss  = "miss"
	CacheResultSkip  = "skip"
	CacheResultError = "error"
)

type CacheTrace struct {
	Result         string   `json:"result,omitempty"`
	Layer          string   `json:"layer,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	Key            string   `json:"key,omitempty"`
	Rewrites       []string `json:"rewrites,omitempty"`
	PromptCacheKey string   `json:"prompt_cache_key,omitempty"`
}

type cacheTraceKey struct{}

func WithCacheTrace(ctx context.Context, trace *CacheTrace) context.Context {
	if trace == nil {
		return ctx
	}
	return context.WithValue(ctx, cacheTraceKey{}, trace)
}

func CacheTraceFrom(ctx context.Context) *CacheTrace {
	if ctx == nil {
		return nil
	}
	if trace, ok := ctx.Value(cacheTraceKey{}).(*CacheTrace); ok {
		return trace
	}
	return nil
}

func setCacheTrace(ctx context.Context, result, layer, reason, key string) {
	trace := CacheTraceFrom(ctx)
	if trace == nil {
		return
	}
	trace.Result = result
	trace.Layer = layer
	trace.Reason = reason
	trace.Key = key
}

func appendCacheRewrite(ctx context.Context, rewrite string) {
	trace := CacheTraceFrom(ctx)
	if trace == nil || rewrite == "" {
		return
	}
	for _, existing := range trace.Rewrites {
		if existing == rewrite {
			return
		}
	}
	trace.Rewrites = append(trace.Rewrites, rewrite)
}

func setPromptCacheKeyTrace(ctx context.Context, key string) {
	trace := CacheTraceFrom(ctx)
	if trace == nil || key == "" {
		return
	}
	trace.PromptCacheKey = key
}
