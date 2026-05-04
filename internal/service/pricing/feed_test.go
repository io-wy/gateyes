package pricing

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const sampleFeed = `{
  "gpt-4o-mini": {
    "input_cost_per_token": 0.00000015,
    "output_cost_per_token": 0.0000006,
    "max_tokens": 16384
  },
  "claude-3-5-sonnet-20241022": {
    "input_cost_per_token": 0.000003,
    "output_cost_per_token": 0.000015
  },
  "no-price-model": {
    "max_tokens": 8192
  }
}`

func TestParseFeedExtractsTokenPrices(t *testing.T) {
	prices, err := parseFeed([]byte(sampleFeed))
	if err != nil {
		t.Fatalf("parseFeed() error: %v", err)
	}
	gpt, ok := prices["gpt-4o-mini"]
	if !ok {
		t.Fatal("gpt-4o-mini missing")
	}
	if gpt.InputPerToken != 0.00000015 || gpt.OutputPerToken != 0.0000006 {
		t.Fatalf("gpt-4o-mini = %+v, want input=1.5e-7 output=6e-7", gpt)
	}
	if _, ok := prices["no-price-model"]; ok {
		t.Fatal("no-price-model should not be present (no token costs)")
	}
}

func TestFeedGetCaseInsensitive(t *testing.T) {
	f := &Feed{prices: map[string]ModelPrice{
		"gpt-4o-mini": {InputPerToken: 0.1},
	}}
	if got, ok := f.Get("GPT-4O-MINI"); !ok || got.InputPerToken != 0.1 {
		t.Fatalf("case-insensitive Get failed: %+v ok=%v", got, ok)
	}
}

func TestFeedRefreshFromHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, sampleFeed)
	}))
	defer srv.Close()

	f := New(Options{URL: srv.URL, Interval: 0})
	if err := f.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	if got := f.Size(); got != 2 {
		t.Fatalf("Size() = %d, want 2", got)
	}
	if _, ok := f.Get("claude-3-5-sonnet-20241022"); !ok {
		t.Fatal("claude not found after refresh")
	}
	refreshes, errs := f.Counters()
	if refreshes != 1 || errs != 0 {
		t.Fatalf("counters = (%d, %d), want (1, 0)", refreshes, errs)
	}
}

func TestFeedRefreshErrorIncrementsCounter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := New(Options{URL: srv.URL, Interval: 0})
	if err := f.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh() returned nil error on 500")
	}
	_, errs := f.Counters()
	if errs != 1 {
		t.Fatalf("error counter = %d, want 1", errs)
	}
}

func TestFeedBootstrapFromCacheFile(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "prices.json")
	if err := os.WriteFile(cache, []byte(sampleFeed), 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	f := New(Options{URL: "http://invalid.localhost", CacheFile: cache, Interval: 0})
	if err := f.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap() error: %v", err)
	}
	if !f.loadedFromDisk.Load() {
		t.Fatal("loadedFromDisk = false, want true")
	}
	if got, ok := f.Get("gpt-4o-mini"); !ok || got.InputPerToken == 0 {
		t.Fatalf("Get from cache failed: %+v ok=%v", got, ok)
	}
}

func TestFeedRefreshWritesCacheFile(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "prices.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, sampleFeed)
	}))
	defer srv.Close()

	f := New(Options{URL: srv.URL, CacheFile: cache, Interval: 0})
	if err := f.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	data, err := os.ReadFile(cache)
	if err != nil {
		t.Fatalf("cache read: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("cache file empty")
	}
}

func TestFeedStartStopLifecycle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, sampleFeed)
	}))
	defer srv.Close()

	f := New(Options{URL: srv.URL, Interval: 50 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Start(ctx)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if f.Size() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if f.Size() == 0 {
		t.Fatal("feed never refreshed")
	}
	f.Stop()
}
