package provider

import (
	"sync"
	"time"
)

// Stats 追踪每个 provider 的统计信息
type Stats struct {
	mu             sync.RWMutex
	providerStats  map[string]*ProviderStats
}

type ProviderStats struct {
	Name            string        `json:"name"`
	Model           string        `json:"model"`
	BaseURL         string        `json:"base_url"`
	Status          string        `json:"status"` // healthy, degraded, down
	CurrentLoad     int64         `json:"current_load"`
	TotalRequests   int64         `json:"total_requests"`
	SuccessRequests int64         `json:"success_requests"`
	FailedRequests  int64         `json:"failed_requests"`
	TotalTokens     int64         `json:"total_tokens"`
	AvgLatencyMs    float64       `json:"avg_latency_ms"`
	MinLatencyMs    int64         `json:"min_latency_ms"`
	MaxLatencyMs    int64         `json:"max_latency_ms"`
	LastRequestAt   time.Time     `json:"last_request_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	latencySum      int64
	latencyCount    int64
}

func NewStats() *Stats {
	return &Stats{
		providerStats: make(map[string]*ProviderStats),
	}
}

func (s *Stats) Register(p *Provider) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.providerStats[p.Name] = &ProviderStats{
		Name:      p.Name,
		Model:     p.Model,
		BaseURL:   p.BaseURL,
		Status:    "healthy",
		UpdatedAt: time.Now(),
	}
}

func (s *Stats) RecordRequest(name string, success bool, tokens int, latencyMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if stats, ok := s.providerStats[name]; ok {
		stats.TotalRequests++
		stats.CurrentLoad++
		stats.TotalTokens += int64(tokens)
		stats.LastRequestAt = time.Now()

		if success {
			stats.SuccessRequests++
		} else {
			stats.FailedRequests++
		}

		// 更新延迟统计
		stats.latencySum += latencyMs
		stats.latencyCount++
		stats.AvgLatencyMs = float64(stats.latencySum) / float64(stats.latencyCount)

		if latencyMs < stats.MinLatencyMs || stats.MinLatencyMs == 0 {
			stats.MinLatencyMs = latencyMs
		}
		if latencyMs > stats.MaxLatencyMs {
			stats.MaxLatencyMs = latencyMs
		}

		stats.UpdatedAt = time.Now()
	}
}

func (s *Stats) DecrementLoad(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if stats, ok := s.providerStats[name]; ok {
		if stats.CurrentLoad > 0 {
			stats.CurrentLoad--
		}
	}
}

func (s *Stats) SetStatus(name, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if stats, ok := s.providerStats[name]; ok {
		stats.Status = status
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

func (s *Stats) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.providerStats = make(map[string]*ProviderStats)
}

// GlobalStats 返回全局统计
func (s *Stats) GlobalStats() (totalReq, successReq, failedReq, totalTokens int64, avgLatency float64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var totalLatency int64
	for _, stats := range s.providerStats {
		totalReq += stats.TotalRequests
		successReq += stats.SuccessRequests
		failedReq += stats.FailedRequests
		totalTokens += stats.TotalTokens
		totalLatency += stats.latencySum
	}

	if totalReq > 0 {
		avgLatency = float64(totalLatency) / float64(totalReq)
	}

	return
}
