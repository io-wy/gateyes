package responses

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// CacheHints lets a single request override the gateway's default cache
// behavior — typically populated from request headers by the HTTP handler.
//
// Headers (case-insensitive in the HTTP layer):
//   - X-Gateyes-Cache-Skip: 1|true       — skip both lookup and write
//   - X-Gateyes-Cache-TTL: <seconds>     — override write TTL
//   - X-Gateyes-Cache-Bucket: <string>   — extra cache-key dimension
//
// Empty / zero values mean "fall back to config defaults", preserving
// behavior for clients that don't send the headers.
type CacheHints struct {
	Skip   bool
	TTL    time.Duration
	Bucket string
}

type cacheHintsKey struct{}

// WithCacheHints attaches hints to ctx. Returns ctx unchanged when hints
// is nil or zero-valued, so it's cheap to call unconditionally.
func WithCacheHints(ctx context.Context, hints CacheHints) context.Context {
	if hints == (CacheHints{}) {
		return ctx
	}
	return context.WithValue(ctx, cacheHintsKey{}, hints)
}

// CacheHintsFrom returns the hints stored on ctx, or a zero value when
// none are set. Always safe.
func CacheHintsFrom(ctx context.Context) CacheHints {
	if ctx == nil {
		return CacheHints{}
	}
	if h, ok := ctx.Value(cacheHintsKey{}).(CacheHints); ok {
		return h
	}
	return CacheHints{}
}

// ParseCacheHintsFromHeaders parses the canonical X-Gateyes-Cache-* headers.
// Unknown / unparseable values are silently ignored (fail-open) so a typo
// in a header doesn't break the request.
//
// header is a function that returns the header value for a given name —
// typically c.GetHeader from gin. Decoupled this way so the package
// doesn't depend on gin.
func ParseCacheHintsFromHeaders(header func(string) string) CacheHints {
	hints := CacheHints{}
	if v := strings.TrimSpace(header("X-Gateyes-Cache-Skip")); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			hints.Skip = true
		}
	}
	if v := strings.TrimSpace(header("X-Gateyes-Cache-TTL")); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			hints.TTL = time.Duration(secs) * time.Second
		}
	}
	if v := strings.TrimSpace(header("X-Gateyes-Cache-Bucket")); v != "" {
		hints.Bucket = v
	}
	return hints
}
