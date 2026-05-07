package ingress

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gateyes/gateway/internal/proxy"
)

var (
	ErrIPNotWhitelisted = errors.New("client IP not in whitelist")
	ErrNoHealthyBackend = errors.New("no healthy backends available")
)

// SelectionContext holds request-scoped data for backend selection.
type SelectionContext struct {
	Request   *http.Request
	Rule      *proxy.RouteRule
	CookieVal string // value of AffinityCookieName cookie, if any
}

// SelectResult holds the outcome of backend selection.
type SelectResult struct {
	Backend    proxy.Backend
	CookieName string
	CookieVal  string
}

// BackendSelector selects a backend from healthy candidates based on annotations.
type BackendSelector struct {
	mu        sync.Mutex
	rrCounter map[string]int64 // per-routeID round-robin counter
	rrWeights map[string]int   // per-backend weighted round-robin accumulator
	rng       *rand.Rand
}

// NewBackendSelector creates a new BackendSelector.
func NewBackendSelector() *BackendSelector {
	return &BackendSelector{
		rrCounter: make(map[string]int64),
		rrWeights: make(map[string]int),
		rng:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Select picks a backend according to annotation rules.
func (s *BackendSelector) Select(ctx SelectionContext) (SelectResult, error) {
	req := ctx.Request
	rule := ctx.Rule
	annot := rule.Annotations
	if annot == nil {
		annot = &proxy.Annotations{}
	}

	// 1. IP Whitelist check.
	if len(annot.WhitelistSourceRange) > 0 {
		if !checkIPWhitelist(req.RemoteAddr, annot.WhitelistSourceRange) {
			return SelectResult{}, ErrIPNotWhitelisted
		}
	}

	// Determine stable and canary backends.
	stableBackends := rule.BackendPool.Healthy()
	var canaryBackends []proxy.Backend
	if rule.CanaryBackendPool != nil {
		canaryBackends = rule.CanaryBackendPool.Healthy()
	}

	// 2. Canary by header (highest priority canary rule).
	if annot.CanaryByHeader != "" {
		hdrVal := req.Header.Get(annot.CanaryByHeader)
		if strings.EqualFold(hdrVal, "always") || strings.EqualFold(hdrVal, "true") {
			if len(canaryBackends) > 0 {
				return SelectResult{Backend: s.pickRoundRobin(rule.ID, canaryBackends)}, nil
			}
			// Fall through to stable if no healthy canary backends.
		}
	}

	// 3. Canary by weight.
	if annot.Canary && annot.CanaryWeight > 0 && len(canaryBackends) > 0 {
		roll := s.rng.Intn(100)
		if roll < annot.CanaryWeight {
			return SelectResult{Backend: s.pickRoundRobin(rule.ID, canaryBackends)}, nil
		}
	}

	// At this point we use stable backends.
	if len(stableBackends) == 0 {
		return SelectResult{}, ErrNoHealthyBackend
	}

	// 4. Session affinity.
	if annot.Affinity == "cookie" && annot.AffinityCookieName != "" {
		result := s.selectByAffinity(rule.ID, stableBackends, ctx.CookieVal, annot.AffinityCookieName, req)
		return result, nil
	}

	// 5. Round-robin (default).
	return SelectResult{Backend: s.pickRoundRobin(rule.ID, stableBackends)}, nil
}

// selectByAffinity picks a backend using session affinity.
func (s *BackendSelector) selectByAffinity(routeID string, backends []proxy.Backend, cookieVal, cookieName string, req *http.Request) SelectResult {
	// If cookie exists and backend is healthy, pin to it.
	if cookieVal != "" {
		for _, b := range backends {
			if b.Name() == cookieVal {
				return SelectResult{Backend: b}
			}
		}
	}

	// Use consistent hashing to pick a backend.
	hashInput := cookieVal
	if hashInput == "" {
		// Fallback to request IP if no cookie.
		hashInput = clientIPFromRequest(req)
	}
	idx := consistentHash(hashInput, len(backends))
	selected := backends[idx]

	// Return cookie to set.
	return SelectResult{
		Backend:    selected,
		CookieName: cookieName,
		CookieVal:  selected.Name(),
	}
}

// pickRoundRobin selects a backend using weighted round-robin.
func (s *BackendSelector) pickRoundRobin(routeID string, backends []proxy.Backend) proxy.Backend {
	if len(backends) == 1 {
		return backends[0]
	}

	// Check if all weights are equal.
	allEqual := true
	firstWeight := backends[0].Weight()
	for i := 1; i < len(backends); i++ {
		if backends[i].Weight() != firstWeight {
			allEqual = false
			break
		}
	}

	if allEqual {
		// Simple round-robin.
		s.mu.Lock()
		counter := s.rrCounter[routeID]
		idx := int(counter % int64(len(backends)))
		s.rrCounter[routeID] = counter + 1
		s.mu.Unlock()
		return backends[idx]
	}

	// Weighted round-robin: increment weight, pick max, subtract total.
	s.mu.Lock()
	defer s.mu.Unlock()

	totalWeight := 0
	for _, b := range backends {
		w := b.Weight()
		if w <= 0 {
			w = 1
		}
		totalWeight += w
	}

	maxIdx := 0
	maxVal := -1
	for i, b := range backends {
		w := b.Weight()
		if w <= 0 {
			w = 1
		}
		s.rrWeights[b.Name()] += w
		if s.rrWeights[b.Name()] > maxVal {
			maxVal = s.rrWeights[b.Name()]
			maxIdx = i
		}
	}

	selected := backends[maxIdx]
	s.rrWeights[selected.Name()] -= totalWeight
	return selected
}

// checkIPWhitelist verifies whether remoteAddr is within any of the allowed CIDR ranges.
func checkIPWhitelist(remoteAddr string, allowed []string) bool {
	clientIP := clientIPFromString(remoteAddr)
	if clientIP == nil {
		return false
	}

	for _, cidr := range allowed {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ipNet.Contains(clientIP) {
			return true
		}
	}
	return false
}

// clientIPFromString extracts the IP from a remote address string (host:port or IP).
func clientIPFromString(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	return net.ParseIP(host)
}

// clientIPFromRequest extracts client IP from an HTTP request.
func clientIPFromRequest(req *http.Request) string {
	if req == nil {
		return ""
	}
	// Check X-Forwarded-For first.
	xff := req.Header.Get("X-Forwarded-For")
	if xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	// Check X-Real-Ip.
	if xri := req.Header.Get("X-Real-Ip"); xri != "" {
		return xri
	}
	// Fallback to RemoteAddr.
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}

// consistentHash computes a deterministic index from a string key and bucket count.
func consistentHash(key string, buckets int) int {
	if buckets <= 0 {
		return 0
	}
	h := sha256.Sum256([]byte(key))
	n := binary.BigEndian.Uint64(h[:8])
	return int(n % uint64(buckets))
}
