package provider

import (
	"sync"
	"sync/atomic"
	"time"
)

// Stats tracks per-provider runtime counters.
//
// The hot path (RecordRequest, IncrementLoad, DecrementLoad) takes only an
// RLock on the provider map plus atomic ops or a short per-provider mutex on
// the slow-path fields (buckets, status). Multiple goroutines updating
// different providers do not contend.
type Stats struct {
	mu            sync.RWMutex
	providerStats map[string]*providerStatsAtomic
}

type tokenBucket struct {
	timestamp int64
	tokens    int64
}

// providerStatsAtomic is the internal storage form. Hot-path counters are
// updated via sync/atomic; slow-path mutable state (buckets, status, times)
// is guarded by `inner`. The struct is laid out so 64-bit fields are
// 8-byte aligned (required for atomic access on 32-bit platforms).
type providerStatsAtomic struct {
	// 64-bit atomic fields first (alignment).
	currentLoad     atomic.Int64
	totalRequests   atomic.Int64
	successRequests atomic.Int64
	failedRequests  atomic.Int64
	totalTokens     atomic.Int64
	latencySum      atomic.Int64
	latencyCount    atomic.Int64
	minLatencyMs    atomic.Int64
	maxLatencyMs    atomic.Int64
	lastRequestUnix atomic.Int64
	updatedAtUnix   atomic.Int64

	// Static, set at Register and never mutated.
	name    string
	pType   string
	model   string
	baseURL string

	// Slow-path mutable state.
	inner   sync.Mutex
	status  string
	buckets [60]tokenBucket
}

// ProviderStats is the snapshot returned to callers. Fields are plain Go
// values; readers may consume them freely. Each call to Get/List/snapshot
// returns a fresh copy; the source values are read via atomic loads.
type ProviderStats struct {
	Name            string    `json:"name"`
	Type            string    `json:"type"`
	Model           string    `json:"model"`
	BaseURL         string    `json:"base_url"`
	Status          string    `json:"status"`
	CurrentLoad     int64     `json:"current_load"`
	TotalRequests   int64     `json:"total_requests"`
	SuccessRequests int64     `json:"success_requests"`
	FailedRequests  int64     `json:"failed_requests"`
	TotalTokens     int64     `json:"total_tokens"`
	AvgLatencyMs    float64   `json:"avg_latency_ms"`
	MinLatencyMs    int64     `json:"min_latency_ms"`
	MaxLatencyMs    int64     `json:"max_latency_ms"`
	LastRequestAt   time.Time `json:"last_request_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func NewStats() *Stats {
	return &Stats{providerStats: make(map[string]*providerStatsAtomic)}
}

func (s *Stats) Register(p Provider) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	a := &providerStatsAtomic{
		name:    p.Name(),
		pType:   p.Type(),
		model:   p.Model(),
		baseURL: p.BaseURL(),
		status:  "healthy",
	}
	a.updatedAtUnix.Store(now.UnixNano())
	s.providerStats[p.Name()] = a
}

func (s *Stats) Unregister(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.providerStats, name)
}

func (s *Stats) RecordRequest(name string, success bool, tokens int, latencyMs int64) {
	s.mu.RLock()
	a, ok := s.providerStats[name]
	s.mu.RUnlock()
	if !ok {
		return
	}

	a.totalRequests.Add(1)
	a.totalTokens.Add(int64(tokens))
	if success {
		a.successRequests.Add(1)
	} else {
		a.failedRequests.Add(1)
	}

	a.latencySum.Add(latencyMs)
	a.latencyCount.Add(1)

	// Min via CAS loop. Init to first value if zero.
	for {
		cur := a.minLatencyMs.Load()
		if cur != 0 && cur <= latencyMs {
			break
		}
		if a.minLatencyMs.CompareAndSwap(cur, latencyMs) {
			break
		}
	}
	// Max via CAS loop.
	for {
		cur := a.maxLatencyMs.Load()
		if latencyMs <= cur {
			break
		}
		if a.maxLatencyMs.CompareAndSwap(cur, latencyMs) {
			break
		}
	}

	now := time.Now()
	a.lastRequestUnix.Store(now.UnixNano())
	a.updatedAtUnix.Store(now.UnixNano())

	// Bucket update needs the per-provider mutex (array slot rotation by
	// timestamp would otherwise race). Held only for a few instructions.
	a.inner.Lock()
	idx := now.Unix() % 60
	if a.buckets[idx].timestamp != now.Unix() {
		a.buckets[idx] = tokenBucket{timestamp: now.Unix(), tokens: 0}
	}
	a.buckets[idx].tokens += int64(tokens)
	a.inner.Unlock()
}

func (s *Stats) IncrementLoad(name string) {
	s.mu.RLock()
	a, ok := s.providerStats[name]
	s.mu.RUnlock()
	if !ok {
		return
	}
	a.currentLoad.Add(1)
	a.updatedAtUnix.Store(time.Now().UnixNano())
}

func (s *Stats) DecrementLoad(name string) {
	s.mu.RLock()
	a, ok := s.providerStats[name]
	s.mu.RUnlock()
	if !ok {
		return
	}
	// Decrement only if > 0; use CAS loop.
	for {
		cur := a.currentLoad.Load()
		if cur <= 0 {
			return
		}
		if a.currentLoad.CompareAndSwap(cur, cur-1) {
			a.updatedAtUnix.Store(time.Now().UnixNano())
			return
		}
	}
}

func (s *Stats) SetStatus(name string, status string) {
	s.mu.RLock()
	a, ok := s.providerStats[name]
	s.mu.RUnlock()
	if !ok {
		return
	}
	a.inner.Lock()
	a.status = status
	a.inner.Unlock()
	a.updatedAtUnix.Store(time.Now().UnixNano())
}

// snapshot reads the atomic state of a provider into a stable value.
func (a *providerStatsAtomic) snapshot() *ProviderStats {
	count := a.latencyCount.Load()
	avg := 0.0
	if count > 0 {
		avg = float64(a.latencySum.Load()) / float64(count)
	}
	a.inner.Lock()
	status := a.status
	a.inner.Unlock()
	return &ProviderStats{
		Name:            a.name,
		Type:            a.pType,
		Model:           a.model,
		BaseURL:         a.baseURL,
		Status:          status,
		CurrentLoad:     a.currentLoad.Load(),
		TotalRequests:   a.totalRequests.Load(),
		SuccessRequests: a.successRequests.Load(),
		FailedRequests:  a.failedRequests.Load(),
		TotalTokens:     a.totalTokens.Load(),
		AvgLatencyMs:    avg,
		MinLatencyMs:    a.minLatencyMs.Load(),
		MaxLatencyMs:    a.maxLatencyMs.Load(),
		LastRequestAt:   unixNanoToTime(a.lastRequestUnix.Load()),
		UpdatedAt:       unixNanoToTime(a.updatedAtUnix.Load()),
	}
}

func unixNanoToTime(ns int64) time.Time {
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func (s *Stats) Get(name string) (*ProviderStats, bool) {
	s.mu.RLock()
	a, ok := s.providerStats[name]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return a.snapshot(), true
}

func (s *Stats) List() []*ProviderStats {
	s.mu.RLock()
	atoms := make([]*providerStatsAtomic, 0, len(s.providerStats))
	for _, a := range s.providerStats {
		atoms = append(atoms, a)
	}
	s.mu.RUnlock()

	result := make([]*ProviderStats, 0, len(atoms))
	for _, a := range atoms {
		result = append(result, a.snapshot())
	}
	return result
}

func (s *Stats) TPM(name string) int64 {
	s.mu.RLock()
	a, ok := s.providerStats[name]
	s.mu.RUnlock()
	if !ok {
		return 0
	}

	cutoff := time.Now().Unix() - 60
	a.inner.Lock()
	defer a.inner.Unlock()
	var total int64
	for _, b := range a.buckets {
		if b.timestamp >= cutoff {
			total += b.tokens
		}
	}
	return total
}

func (s *Stats) CurrentLoad(name string) int64 {
	s.mu.RLock()
	a, ok := s.providerStats[name]
	s.mu.RUnlock()
	if !ok {
		return 0
	}
	return a.currentLoad.Load()
}

func (s *Stats) GlobalStats() (int64, int64, int64, int64, float64) {
	s.mu.RLock()
	atoms := make([]*providerStatsAtomic, 0, len(s.providerStats))
	for _, a := range s.providerStats {
		atoms = append(atoms, a)
	}
	s.mu.RUnlock()

	var totalReq, successReq, failedReq, totalTokens, totalLatency int64
	for _, a := range atoms {
		totalReq += a.totalRequests.Load()
		successReq += a.successRequests.Load()
		failedReq += a.failedRequests.Load()
		totalTokens += a.totalTokens.Load()
		totalLatency += a.latencySum.Load()
	}
	var avgLatency float64
	if totalReq > 0 {
		avgLatency = float64(totalLatency) / float64(totalReq)
	}
	return totalReq, successReq, failedReq, totalTokens, avgLatency
}
