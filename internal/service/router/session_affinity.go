package router

import (
	"hash/fnv"
	"sync"
	"time"

	"github.com/gateyes/gateway/internal/service/provider"
)

// SessionAffinity pins a session (identified by SessionID) to a single
// provider. It uses consistent hashing when the session has not been seen
// before, and remembers the choice so subsequent requests with the same
// session ID hit the same provider.
//
// This is the implementation of the legacy "sticky" strategy, migrated
// from the strategy layer into the Affinity pipeline.
type SessionAffinity struct {
	mu       sync.RWMutex
	sessions map[string]string // sessionID → providerName
	ttl      time.Duration
}

// NewSessionAffinity creates a session affinity with the given TTL.
// ttl <= 0 means entries never expire.
func NewSessionAffinity(ttl time.Duration) *SessionAffinity {
	return &SessionAffinity{
		sessions: make(map[string]string),
		ttl:      ttl,
	}
}

// Pin reorders candidates so that the session's provider is first.
// If the session is new, it picks a provider using weighted consistent
// hashing of SessionID.
func (s *SessionAffinity) Pin(candidates []provider.Provider, ctx RouteContext) []provider.Provider {
	if len(candidates) <= 1 || ctx.SessionID == "" {
		return candidates
	}

	s.mu.RLock()
	name, ok := s.sessions[ctx.SessionID]
	s.mu.RUnlock()

	if !ok {
		name = pickByHash(candidates, ctx.SessionID)
		s.mu.Lock()
		s.sessions[ctx.SessionID] = name
		s.mu.Unlock()
	}

	for i, p := range candidates {
		if p.Name() == name {
			result := make([]provider.Provider, 0, len(candidates))
			result = append(result, p)
			result = append(result, candidates[:i]...)
			result = append(result, candidates[i+1:]...)
			return result
		}
	}
	return candidates
}

// Promote updates the session mapping. This is a no-op for SessionAffinity
// because the mapping is already established in Pin.
func (s *SessionAffinity) Promote(ctx RouteContext, providerName string) {
	if ctx.SessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[ctx.SessionID] = providerName
}

// pickByHash selects a provider using weighted consistent hashing.
func pickByHash(candidates []provider.Provider, key string) string {
	totalWeight := 0
	for _, p := range candidates {
		w := p.Weight()
		if w <= 0 {
			w = 1
		}
		totalWeight += w
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	hash := int(h.Sum32())

	if totalWeight > 0 {
		pick := hash % totalWeight
		if pick < 0 {
			pick = -pick
		}
		cum := 0
		for _, p := range candidates {
			w := p.Weight()
			if w <= 0 {
				w = 1
			}
			cum += w
			if pick < cum {
				return p.Name()
			}
		}
	}
	pick := hash % len(candidates)
	if pick < 0 {
		pick = -pick
	}
	return candidates[pick].Name()
}
