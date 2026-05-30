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
type sessionEntry struct {
	providerName string
	createdAt    time.Time
}

type SessionAffinity struct {
	mu       sync.RWMutex
	sessions map[string]sessionEntry // sessionID → entry
	ttl      time.Duration
}

// NewSessionAffinity creates a session affinity with the given TTL.
// ttl <= 0 means entries never expire.
func NewSessionAffinity(ttl time.Duration) *SessionAffinity {
	return &SessionAffinity{
		sessions: make(map[string]sessionEntry),
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

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.sessions) > 10000 {
		s.cleanupLocked()
	}

	entry, ok := s.sessions[ctx.SessionID]
	if ok && s.ttl > 0 && time.Since(entry.createdAt) > s.ttl {
		delete(s.sessions, ctx.SessionID)
		ok = false
	}

	var name string
	if !ok {
		name = pickByHash(candidates, ctx.SessionID)
		entry = sessionEntry{providerName: name, createdAt: time.Now()}
		s.sessions[ctx.SessionID] = entry
	} else {
		name = entry.providerName
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

// Promote updates the session mapping.
func (s *SessionAffinity) Promote(ctx RouteContext, providerName string) {
	if ctx.SessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[ctx.SessionID] = sessionEntry{providerName: providerName, createdAt: time.Now()}
}

func (s *SessionAffinity) cleanupLocked() {
	if s.ttl <= 0 {
		return
	}
	now := time.Now()
	for k, v := range s.sessions {
		if now.Sub(v.createdAt) > s.ttl {
			delete(s.sessions, k)
		}
	}
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
