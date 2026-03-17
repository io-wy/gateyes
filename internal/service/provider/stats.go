package provider

import (
	"sync"
	"time"
)

type Stats struct {
	mu            sync.RWMutex
	providerStats map[string]*ProviderStats
}

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
	latencySum      int64
	latencyCount    int64
}

func NewStats() *Stats {
	return &Stats{providerStats: make(map[string]*ProviderStats)}
}

func (s *Stats) Register(p Provider) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.providerStats[p.Name()] = &ProviderStats{
		Name:      p.Name(),
		Type:      p.Type(),
		Model:     p.Model(),
		BaseURL:   p.BaseURL(),
		Status:    "healthy",
		UpdatedAt: time.Now(),
	}
}

func (s *Stats) RecordRequest(name string, success bool, tokens int, latencyMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stats, ok := s.providerStats[name]
	if !ok {
		return
	}

	stats.TotalRequests++
	stats.TotalTokens += int64(tokens)
	stats.LastRequestAt = time.Now()
	stats.UpdatedAt = stats.LastRequestAt

	if success {
		stats.SuccessRequests++
	} else {
		stats.FailedRequests++
	}

	stats.latencySum += latencyMs
	stats.latencyCount++
	stats.AvgLatencyMs = float64(stats.latencySum) / float64(stats.latencyCount)

	if stats.MinLatencyMs == 0 || latencyMs < stats.MinLatencyMs {
		stats.MinLatencyMs = latencyMs
	}
	if latencyMs > stats.MaxLatencyMs {
		stats.MaxLatencyMs = latencyMs
	}
}

func (s *Stats) IncrementLoad(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stats, ok := s.providerStats[name]; ok {
		stats.CurrentLoad++
		stats.UpdatedAt = time.Now()
	}
}

func (s *Stats) DecrementLoad(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stats, ok := s.providerStats[name]; ok && stats.CurrentLoad > 0 {
		stats.CurrentLoad--
		stats.UpdatedAt = time.Now()
	}
}

func (s *Stats) Get(name string) (*ProviderStats, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stats, ok := s.providerStats[name]
	return stats, ok
}

func (s *Stats) List() []*ProviderStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*ProviderStats, 0, len(s.providerStats))
	for _, stats := range s.providerStats {
		result = append(result, stats)
	}
	return result
}

func (s *Stats) GlobalStats() (int64, int64, int64, int64, float64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var totalReq int64
	var successReq int64
	var failedReq int64
	var totalTokens int64
	var totalLatency int64

	for _, stats := range s.providerStats {
		totalReq += stats.TotalRequests
		successReq += stats.SuccessRequests
		failedReq += stats.FailedRequests
		totalTokens += stats.TotalTokens
		totalLatency += stats.latencySum
	}

	var avgLatency float64
	if totalReq > 0 {
		avgLatency = float64(totalLatency) / float64(totalReq)
	}

	return totalReq, successReq, failedReq, totalTokens, avgLatency
}
