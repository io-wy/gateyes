package ingress

import (
	"sync"

	"github.com/gateyes/gateway/internal/proxy"
	"github.com/gateyes/gateway/internal/service/limiter"
)

// IngressLimiter provides per-route rate limiting using token buckets and connection limits.
type IngressLimiter struct {
	mu       sync.RWMutex
	buckets  map[string]*limiter.TB // key: "routeID:clientIP" for RPS limiting
	conns    map[string]int         // key: "routeID:clientIP" for connection limiting
}

// NewIngressLimiter creates a new IngressLimiter.
func NewIngressLimiter() *IngressLimiter {
	return &IngressLimiter{
		buckets: make(map[string]*limiter.TB),
		conns:   make(map[string]int),
	}
}

// Acquire checks both RPS and connection limits. Returns true if the request is allowed.
// The caller must call Release after the request completes (typically via defer).
func (l *IngressLimiter) Acquire(routeID, clientIP string, annot *proxy.Annotations) bool {
	if annot == nil {
		return true
	}

	key := routeID + ":" + clientIP

	// Check RPS limit first.
	if annot.RateLimitRPS > 0 {
		rate := int(annot.RateLimitRPS)
		burst := rate * 2
		if burst < 10 {
			burst = 10
		}
		l.mu.Lock()
		b, ok := l.buckets[key]
		if !ok {
			b = limiter.NewTokenBucket(rate, burst)
			l.buckets[key] = b
		}
		l.mu.Unlock()
		if !b.TryConsume(1) {
			return false
		}
	}

	// Check connection limit.
	if annot.RateLimitConnections > 0 {
		l.mu.Lock()
		if l.conns[key] >= annot.RateLimitConnections {
			l.mu.Unlock()
			return false
		}
		l.conns[key]++
		l.mu.Unlock()
	}

	return true
}

// Release decrements the active connection count for the given route and client.
func (l *IngressLimiter) Release(routeID, clientIP string) {
	key := routeID + ":" + clientIP
	l.mu.Lock()
	if l.conns[key] > 0 {
		l.conns[key]--
	}
	l.mu.Unlock()
}
