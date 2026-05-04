// Package router (this file): scrape inference-server-side metrics so
// the routing strategies can include queue depth and KV-cache utilisation
// in their scoring — pattern from Envoy AI Gateway's endpoint-picker.
//
// We currently target vLLM's Prometheus exposition format. The relevant
// gauges:
//
//	vllm:num_requests_running         — in-flight decode steps
//	vllm:num_requests_waiting         — queued requests
//	vllm:gpu_cache_usage_perc         — KV cache fill ratio (0-1)
//
// When a provider exposes none of these, the signal stays zero and the
// router falls back to in-process CurrentLoad — fail-open semantics.

package router

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// InferenceState is the cached per-provider scrape result.
type InferenceState struct {
	NumRequestsRunning float64
	NumRequestsWaiting float64
	GPUCacheUsagePerc  float64
	UpdatedAt          time.Time
	Stale              bool
}

// LoadScore computes a synthetic load metric used by least-load routing.
// Higher = more loaded. Tunable weights match Envoy AI Gateway intuition:
// queue depth dominates (you want to push to under-utilised endpoints),
// running count is secondary, cache utilisation tips the tie at saturation.
func (s InferenceState) LoadScore() float64 {
	if s.Stale {
		return 0
	}
	return s.NumRequestsWaiting*4 + s.NumRequestsRunning*1 + s.GPUCacheUsagePerc*2
}

// InferenceScraper periodically polls each registered endpoint for vLLM-
// style Prometheus metrics. Results are stored under the provider name.
//
// Concurrency model: one goroutine per scrape interval; the cache map is
// protected by a single sync.RWMutex (read-heavy after warmup). Read path
// is the routing strategy, which expects sub-microsecond latency.
type InferenceScraper struct {
	endpoints   map[string]string // provider name -> /metrics URL
	interval    time.Duration
	httpClient  *http.Client
	stateMu     sync.RWMutex
	state       map[string]InferenceState
	closeOnce   sync.Once
	cancel      context.CancelFunc
	scrapeCount atomic.Int64
	errorCount  atomic.Int64
}

// NewInferenceScraper constructs an InferenceScraper. endpoints maps a
// provider name to its /metrics URL; an empty URL skips that provider.
//
// interval <= 0 falls back to 5 seconds.
func NewInferenceScraper(endpoints map[string]string, interval time.Duration) *InferenceScraper {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	clean := make(map[string]string, len(endpoints))
	for name, url := range endpoints {
		if strings.TrimSpace(url) != "" {
			clean[name] = url
		}
	}
	return &InferenceScraper{
		endpoints: clean,
		interval:  interval,
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
		},
		state: make(map[string]InferenceState, len(clean)),
	}
}

// Start launches the scrape loop. ctx cancellation stops the loop. Safe
// to call multiple times — only the first call spawns workers.
func (s *InferenceScraper) Start(ctx context.Context) {
	if s == nil || len(s.endpoints) == 0 {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	go s.run(runCtx)
}

func (s *InferenceScraper) run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.scrapeAll(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scrapeAll(ctx)
		}
	}
}

func (s *InferenceScraper) scrapeAll(ctx context.Context) {
	for name, url := range s.endpoints {
		state, err := s.scrape(ctx, url)
		if err != nil {
			s.errorCount.Add(1)
			s.stateMu.Lock()
			cur := s.state[name]
			cur.Stale = true
			s.state[name] = cur
			s.stateMu.Unlock()
			continue
		}
		state.UpdatedAt = time.Now()
		s.stateMu.Lock()
		s.state[name] = state
		s.stateMu.Unlock()
		s.scrapeCount.Add(1)
	}
}

func (s *InferenceScraper) scrape(ctx context.Context, url string) (InferenceState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return InferenceState{}, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return InferenceState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return InferenceState{}, errors.New("bad status: " + resp.Status)
	}
	return parseVLLMMetrics(resp.Body)
}

// parseVLLMMetrics walks Prometheus text format, picking out the gauges
// we care about. Other lines are ignored. We only support the unlabelled
// or single-instance case — multi-replica vLLM behind a single URL is
// summed (which is usually what you want for routing decisions).
func parseVLLMMetrics(r io.Reader) (InferenceState, error) {
	state := InferenceState{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024), 1024*1024) // some metrics dumps are large
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		// Format: metric{labels} value [timestamp]
		// We accept the form "metric value" (no labels) or strip labels.
		spaceIdx := strings.LastIndex(line, " ")
		if spaceIdx < 0 {
			continue
		}
		name := line[:spaceIdx]
		valStr := line[spaceIdx+1:]
		if brace := strings.Index(name, "{"); brace > 0 {
			name = name[:brace]
		}
		val, err := strconv.ParseFloat(strings.TrimSpace(valStr), 64)
		if err != nil {
			continue
		}
		switch name {
		case "vllm:num_requests_running":
			state.NumRequestsRunning += val
		case "vllm:num_requests_waiting":
			state.NumRequestsWaiting += val
		case "vllm:gpu_cache_usage_perc":
			state.GPUCacheUsagePerc = val
		}
	}
	if err := scanner.Err(); err != nil {
		return state, err
	}
	return state, nil
}

// Get returns the latest cached state for a provider name. The boolean
// is false when there is no state (provider not registered or first
// scrape hasn't completed yet).
func (s *InferenceScraper) Get(name string) (InferenceState, bool) {
	if s == nil {
		return InferenceState{}, false
	}
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	st, ok := s.state[name]
	return st, ok
}

// Stop terminates the scrape loop. Safe to call multiple times.
func (s *InferenceScraper) Stop() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
	})
}

// Counters returns (scrapes-completed, errors). Useful for metrics.
func (s *InferenceScraper) Counters() (int64, int64) {
	if s == nil {
		return 0, 0
	}
	return s.scrapeCount.Load(), s.errorCount.Load()
}
