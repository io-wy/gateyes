// Package pricing fetches and serves model→price data from an external
// JSON feed (default: LiteLLM's model_prices_and_context_window.json).
//
// The gateway's per-provider PriceInput / PriceOutput config takes
// precedence; this feed only fills gaps when a model is not explicitly
// priced in yaml. That keeps yaml-driven configurations authoritative
// while letting operators avoid hand-maintaining a price table for
// every new OpenAI / Anthropic / vendor model.
//
// Reference shape (LiteLLM JSON, partial):
//
//	{
//	  "gpt-4o-mini": {
//	    "input_cost_per_token": 0.00000015,
//	    "output_cost_per_token": 0.0000006,
//	    ...
//	  },
//	  ...
//	}
package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultFeedURL is the LiteLLM-published price table. Stable URL, MIT-licensed.
const DefaultFeedURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// ModelPrice is the per-token price for a model. Both fields are USD per
// single token (NOT per 1000 tokens — careful when copying numbers).
type ModelPrice struct {
	InputPerToken  float64
	OutputPerToken float64
}

// Feed is a thread-safe price lookup table refreshed periodically from
// an external JSON source, with optional disk-cache fallback so a cold
// start without network still has something to work with.
type Feed struct {
	url       string
	cacheFile string
	interval  time.Duration

	httpClient *http.Client

	mu     sync.RWMutex
	prices map[string]ModelPrice

	loadedFromDisk atomic.Bool
	refreshCount   atomic.Int64
	errorCount     atomic.Int64

	startOnce sync.Once
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// Options configures a Feed.
type Options struct {
	URL       string        // empty -> DefaultFeedURL
	CacheFile string        // empty -> no disk cache
	Interval  time.Duration // 0 -> 24h
}

// New constructs a Feed. Prices map is empty until Start runs the first
// refresh (or until Bootstrap loads from CacheFile synchronously).
func New(opts Options) *Feed {
	if opts.URL == "" {
		opts.URL = DefaultFeedURL
	}
	if opts.Interval <= 0 {
		opts.Interval = 24 * time.Hour
	}
	return &Feed{
		url:       opts.URL,
		cacheFile: opts.CacheFile,
		interval:  opts.Interval,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		prices: make(map[string]ModelPrice),
	}
}

// Get returns the price for a model and whether it was found.
//
// Lookup is case-insensitive on the model key — LiteLLM uses lowercase
// keys, but provider model strings vary in case. Callers should pass the
// canonical model name unchanged; this method does the normalisation.
func (f *Feed) Get(model string) (ModelPrice, bool) {
	if f == nil || model == "" {
		return ModelPrice{}, false
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	if p, ok := f.prices[model]; ok {
		return p, true
	}
	// Try a couple of normalisations.
	lower := lowerASCII(model)
	if p, ok := f.prices[lower]; ok {
		return p, true
	}
	return ModelPrice{}, false
}

func lowerASCII(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b[i] = c
	}
	return string(b)
}

// Bootstrap populates the price table from the on-disk cache file (if
// configured), so a cold start without network connectivity still has
// pricing data. Safe to call before Start; ignored when no CacheFile.
func (f *Feed) Bootstrap() error {
	if f == nil || f.cacheFile == "" {
		return nil
	}
	data, err := os.ReadFile(f.cacheFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	prices, err := parseFeed(data)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.prices = prices
	f.mu.Unlock()
	f.loadedFromDisk.Store(true)
	return nil
}

// Start launches the background refresh loop. Cancel ctx to stop. Safe
// to call multiple times — only the first wins. Returns immediately.
func (f *Feed) Start(ctx context.Context) {
	if f == nil {
		return
	}
	f.startOnce.Do(func() {
		runCtx, cancel := context.WithCancel(ctx)
		f.cancel = cancel
		f.wg.Add(1)
		go f.run(runCtx)
	})
}

// Stop terminates the refresh loop. Safe to call multiple times.
func (f *Feed) Stop() {
	if f == nil {
		return
	}
	if f.cancel != nil {
		f.cancel()
	}
	f.wg.Wait()
}

func (f *Feed) run(ctx context.Context) {
	defer f.wg.Done()
	// First refresh runs immediately so callers get fresh data ASAP.
	_ = f.Refresh(ctx)
	ticker := time.NewTicker(f.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = f.Refresh(ctx)
		}
	}
}

// Refresh performs a one-shot fetch + parse + replace cycle. Exported
// so callers can trigger an out-of-cycle refresh from an admin endpoint.
func (f *Feed) Refresh(ctx context.Context) error {
	if f == nil {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		f.errorCount.Add(1)
		return err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		f.errorCount.Add(1)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		f.errorCount.Add(1)
		return errors.New("pricing: bad status: " + resp.Status)
	}
	data, err := readAll(resp.Body, 16*1024*1024) // cap at 16MB to avoid runaway
	if err != nil {
		f.errorCount.Add(1)
		return err
	}
	prices, err := parseFeed(data)
	if err != nil {
		f.errorCount.Add(1)
		return err
	}
	f.mu.Lock()
	f.prices = prices
	f.mu.Unlock()
	f.refreshCount.Add(1)

	if f.cacheFile != "" {
		_ = writeAtomic(f.cacheFile, data)
	}
	return nil
}

// Counters returns (successful refreshes, errors). For metrics.
func (f *Feed) Counters() (int64, int64) {
	if f == nil {
		return 0, 0
	}
	return f.refreshCount.Load(), f.errorCount.Load()
}

// Size returns the number of priced models.
func (f *Feed) Size() int {
	if f == nil {
		return 0
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.prices)
}

// parseFeed accepts the LiteLLM-style JSON shape and extracts only the
// fields we care about. Unknown fields are silently ignored, which keeps
// us forward-compatible with new attributes the upstream JSON adds.
func parseFeed(data []byte) (map[string]ModelPrice, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	prices := make(map[string]ModelPrice, len(raw))
	for name, blob := range raw {
		var entry struct {
			InputCostPerToken  *float64 `json:"input_cost_per_token"`
			OutputCostPerToken *float64 `json:"output_cost_per_token"`
		}
		if err := json.Unmarshal(blob, &entry); err != nil {
			continue
		}
		if entry.InputCostPerToken == nil && entry.OutputCostPerToken == nil {
			continue
		}
		mp := ModelPrice{}
		if entry.InputCostPerToken != nil {
			mp.InputPerToken = *entry.InputCostPerToken
		}
		if entry.OutputCostPerToken != nil {
			mp.OutputPerToken = *entry.OutputCostPerToken
		}
		prices[name] = mp
	}
	return prices, nil
}

func readAll(r interface{ Read([]byte) (int, error) }, max int64) ([]byte, error) {
	buf := make([]byte, 0, 64*1024)
	tmp := make([]byte, 32*1024)
	var total int64
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			total += int64(n)
			if total > max {
				return nil, errors.New("pricing: feed exceeds max size")
			}
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				return buf, nil
			}
			return buf, err
		}
	}
}

// writeAtomic writes data to path via a temp + rename so a crashed write
// never leaves a half-written file as the cached source of truth.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".pricing-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op if rename succeeds
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
