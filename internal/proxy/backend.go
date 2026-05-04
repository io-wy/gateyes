package proxy

import (
	"context"
	"net/http"
	"sync"
)

// Backend is a generic upstream service instance.
// It mirrors the metadata surface of provider.Provider for router reuse.
type Backend interface {
	Name() string
	Address() string
	Weight() int
	Healthy() bool
	Protocol() string // "http", "https", "grpc"
	SetHealthy(bool)
}

// SimpleBackend is a concrete Backend implementation.
type SimpleBackend struct {
	name     string
	address  string
	weight   int
	healthy  bool
	protocol string
	mu       sync.RWMutex
}

func NewBackend(name, address, protocol string, weight int) *SimpleBackend {
	if protocol == "" {
		protocol = "http"
	}
	if weight <= 0 {
		weight = 1
	}
	return &SimpleBackend{
		name:     name,
		address:  address,
		weight:   weight,
		healthy:  true,
		protocol: protocol,
	}
}

func (b *SimpleBackend) Name() string     { return b.name }
func (b *SimpleBackend) Address() string  { return b.address }
func (b *SimpleBackend) Weight() int      { return b.weight }
func (b *SimpleBackend) Protocol() string { return b.protocol }

func (b *SimpleBackend) Healthy() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.healthy
}

func (b *SimpleBackend) SetHealthy(v bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.healthy = v
}

// BackendPool is a mutable collection of backends for a route.
type BackendPool struct {
	backends []Backend
	mu       sync.RWMutex
}

func NewBackendPool(backends []Backend) *BackendPool {
	return &BackendPool{backends: backends}
}

func (p *BackendPool) Set(backends []Backend) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.backends = backends
}

func (p *BackendPool) List() []Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Backend, len(p.backends))
	copy(out, p.backends)
	return out
}

func (p *BackendPool) Healthy() []Backend {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var out []Backend
	for _, b := range p.backends {
		if b.Healthy() {
			out = append(out, b)
		}
	}
	return out
}

// RoundTripper wraps a backend with protocol-aware transport.
type RoundTripper struct {
	backend Backend
	rt      http.RoundTripper
}

func NewRoundTripper(b Backend, rt http.RoundTripper) *RoundTripper {
	if rt == nil {
		rt = http.DefaultTransport
	}
	return &RoundTripper{backend: b, rt: rt}
}

func (t *RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.rt.RoundTrip(req)
}
