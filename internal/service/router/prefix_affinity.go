package router

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/gateyes/gateway/internal/service/provider"
)

// PrefixAffinity pins requests with the same prompt prefix to the same
// provider. This improves prefix-cache hit rates on backends (e.g. vLLM)
// that support prefix-based KV caching.
//
// It maintains an in-memory LRU of prefix-hash → provider-name. The
// prefix is derived from the first PrefixDepth runes of the canonical
// prompt text. When PrefixDepth is 0 the full prompt is used.
type PrefixAffinity struct {
	mu          sync.RWMutex
	items       map[string]*prefixEntry
	ttl         time.Duration
	prefixDepth int // 0 = full prompt
}

type prefixEntry struct {
	providerName string
	expiresAt    time.Time
}

// NewPrefixAffinity creates a prefix affinity. ttl <= 0 means no expiry.
func NewPrefixAffinity(prefixDepth int, ttl time.Duration) *PrefixAffinity {
	return &PrefixAffinity{
		items:       make(map[string]*prefixEntry),
		ttl:         ttl,
		prefixDepth: prefixDepth,
	}
}

// Pin reorders candidates so that the prefix-mapped provider is first.
func (pa *PrefixAffinity) Pin(candidates []provider.Provider, ctx RouteContext) []provider.Provider {
	if len(candidates) <= 1 {
		return candidates
	}
	key := pa.hashPrefix(ctx.InputText)
	if key == "" {
		return candidates
	}

	pa.mu.RLock()
	en, ok := pa.items[key]
	pa.mu.RUnlock()

	if !ok || (pa.ttl > 0 && time.Now().After(en.expiresAt)) {
		return candidates
	}

	for i, p := range candidates {
		if p.Name() == en.providerName {
			result := make([]provider.Provider, 0, len(candidates))
			result = append(result, p)
			result = append(result, candidates[:i]...)
			result = append(result, candidates[i+1:]...)
			return result
		}
	}
	return candidates
}

// Promote records the provider used for this request's prefix.
func (pa *PrefixAffinity) Promote(ctx RouteContext, providerName string) {
	key := pa.hashPrefix(ctx.InputText)
	if key == "" {
		return
	}
	pa.mu.Lock()
	defer pa.mu.Unlock()
	var expires time.Time
	if pa.ttl > 0 {
		expires = time.Now().Add(pa.ttl)
	}
	pa.items[key] = &prefixEntry{providerName: providerName, expiresAt: expires}
}

func (pa *PrefixAffinity) hashPrefix(text string) string {
	if text == "" {
		return ""
	}
	prefix := text
	if pa.prefixDepth > 0 {
		runes := []rune(text)
		if len(runes) > pa.prefixDepth {
			prefix = string(runes[:pa.prefixDepth])
		}
	}
	h := sha256.New()
	_, _ = h.Write([]byte(prefix))
	return hex.EncodeToString(h.Sum(nil))
}
