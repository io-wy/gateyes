package cache

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeCache struct {
	mu      sync.Mutex
	data    map[string]*Entry
	errs    map[string]error
	stats   Stats
	closed  bool
	getHook func(key string) (*Entry, bool, error)
}

func newFakeCache() *fakeCache {
	return &fakeCache{data: make(map[string]*Entry), errs: make(map[string]error)}
}

func (f *fakeCache) Get(_ context.Context, key string) (*Entry, bool, error) {
	if f.getHook != nil {
		return f.getHook(key)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errs[key]; err != nil {
		return nil, false, err
	}
	if v, ok := f.data[key]; ok {
		f.stats.Hits++
		return v, true, nil
	}
	f.stats.Misses++
	return nil, false, nil
}

func (f *fakeCache) Set(_ context.Context, key string, e *Entry, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errs["set:"+key]; err != nil {
		return err
	}
	f.data[key] = e
	f.stats.Writes++
	return nil
}

func (f *fakeCache) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, key)
	return nil
}

func (f *fakeCache) Stats() Stats {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stats
}

func (f *fakeCache) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func TestFallbackCache_PrimaryHit(t *testing.T) {
	pri := newFakeCache()
	fb := newFakeCache()
	c := NewFallbackCache(pri, fb)
	pri.data["k"] = newTestEntry("m")

	got, hit, err := c.Get(context.Background(), "k")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !hit || got.Model != "m" {
		t.Fatalf("expected primary hit")
	}
}

func TestFallbackCache_FallbackOnMiss(t *testing.T) {
	pri := newFakeCache()
	fb := newFakeCache()
	c := NewFallbackCache(pri, fb)
	fb.data["k"] = newTestEntry("fb")

	got, hit, err := c.Get(context.Background(), "k")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !hit || got.Model != "fb" {
		t.Fatalf("expected fallback hit")
	}
}

func TestFallbackCache_FallbackOnError(t *testing.T) {
	pri := newFakeCache()
	fb := newFakeCache()
	c := NewFallbackCache(pri, fb)
	pri.errs["k"] = errors.New("boom")
	fb.data["k"] = newTestEntry("fb")

	got, hit, err := c.Get(context.Background(), "k")
	if err != nil {
		t.Fatalf("fallback should swallow primary error, got %v", err)
	}
	if !hit || got.Model != "fb" {
		t.Fatalf("expected fallback hit after primary error")
	}
}

func TestFallbackCache_SetBoth(t *testing.T) {
	pri := newFakeCache()
	fb := newFakeCache()
	c := NewFallbackCache(pri, fb)

	if err := c.Set(context.Background(), "k", newTestEntry("v"), 0); err != nil {
		t.Fatalf("set: %v", err)
	}
	if pri.data["k"] == nil || pri.data["k"].Model != "v" {
		t.Fatalf("primary not set")
	}
	if fb.data["k"] == nil || fb.data["k"].Model != "v" {
		t.Fatalf("fallback not set")
	}
}

func TestFallbackCache_SetPrimaryErrorStillSetsFallback(t *testing.T) {
	pri := newFakeCache()
	fb := newFakeCache()
	c := NewFallbackCache(pri, fb)
	pri.errs["set:k"] = errors.New("boom")

	err := c.Set(context.Background(), "k", newTestEntry("v"), 0)
	if err == nil {
		t.Fatalf("expected primary error to propagate")
	}
	if fb.data["k"] == nil {
		t.Fatalf("fallback should still be set despite primary error")
	}
}

func TestFallbackCache_DeleteBoth(t *testing.T) {
	pri := newFakeCache()
	fb := newFakeCache()
	c := NewFallbackCache(pri, fb)
	_ = c.Set(context.Background(), "k", newTestEntry("v"), 0)
	if err := c.Delete(context.Background(), "k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if pri.data["k"] != nil || fb.data["k"] != nil {
		t.Fatalf("expected both deleted")
	}
}

func TestFallbackCache_StatsFromPrimary(t *testing.T) {
	pri := newFakeCache()
	fb := newFakeCache()
	c := NewFallbackCache(pri, fb)
	pri.stats.Hits = 7
	if s := c.Stats(); s.Hits != 7 {
		t.Fatalf("expected primary stats, got %+v", s)
	}
}

func TestFallbackCache_CloseBoth(t *testing.T) {
	pri := newFakeCache()
	fb := newFakeCache()
	c := NewFallbackCache(pri, fb)
	_ = c.Close()
	if !pri.closed || !fb.closed {
		t.Fatalf("expected both closed")
	}
}

func TestFallbackCache_NilPrimaryUsesFallback(t *testing.T) {
	fb := newFakeCache()
	c := NewFallbackCache(nil, fb)
	fb.data["k"] = newTestEntry("fb")
	got, hit, err := c.Get(context.Background(), "k")
	if err != nil || !hit || got.Model != "fb" {
		t.Fatalf("expected fallback-only hit")
	}
}

func TestFallbackCache_NilFallback(t *testing.T) {
	pri := newFakeCache()
	c := NewFallbackCache(pri, nil)
	pri.data["k"] = newTestEntry("m")
	got, hit, err := c.Get(context.Background(), "k")
	if err != nil || !hit || got.Model != "m" {
		t.Fatalf("expected primary-only hit")
	}
}
