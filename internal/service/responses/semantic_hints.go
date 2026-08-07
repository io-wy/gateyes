package responses

import (
	"context"
	"strconv"
	"strings"
)

type SemanticCacheHints struct {
	Enable bool
	Skip   bool
}

type semanticCacheHintsKey struct{}

func WithSemanticCacheHints(ctx context.Context, hints SemanticCacheHints) context.Context {
	if hints == (SemanticCacheHints{}) {
		return ctx
	}
	return context.WithValue(ctx, semanticCacheHintsKey{}, hints)
}

func SemanticCacheHintsFrom(ctx context.Context) SemanticCacheHints {
	if ctx == nil {
		return SemanticCacheHints{}
	}
	if hints, ok := ctx.Value(semanticCacheHintsKey{}).(SemanticCacheHints); ok {
		return hints
	}
	return SemanticCacheHints{}
}

func ParseSemanticCacheHintsFromHeaders(header func(string) string) SemanticCacheHints {
	hints := SemanticCacheHints{}
	if parseTruthy(header("X-Gateyes-Semantic-Cache")) {
		hints.Enable = true
	}
	if parseTruthy(header("X-Gateyes-Semantic-Cache-Skip")) {
		hints.Skip = true
	}
	return hints
}

func parseTruthy(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return false
	}
	switch value {
	case "1", "true", "yes", "on", "enable", "enabled":
		return true
	}
	if n, err := strconv.Atoi(value); err == nil {
		return n > 0
	}
	return false
}
