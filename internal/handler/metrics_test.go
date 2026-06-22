package handler

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// These tests assert the cache/circuit-breaker metric helpers actually record
// against the right label set with the right value — not merely that the call
// does not panic. Each test uses a distinct namespace because NewMetrics
// registers into the global prometheus registry (duplicate namespace+name panics).

func TestRecordCacheLookup(t *testing.T) {
	m := NewMetrics("cache_lookup_test")
	m.RecordCacheLookup("l1", "hit")
	m.RecordCacheLookup("l1", "hit")
	m.RecordCacheLookup("l1", "miss")

	if got := testutil.ToFloat64(m.cacheLookups.WithLabelValues("l1", "hit")); got != 2 {
		t.Errorf("cache_lookups_total{layer=l1,result=hit} = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.cacheLookups.WithLabelValues("l1", "miss")); got != 1 {
		t.Errorf("cache_lookups_total{layer=l1,result=miss} = %v, want 1", got)
	}
}

func TestRecordCacheWrite(t *testing.T) {
	m := NewMetrics("cache_write_test")
	m.RecordCacheWrite("l1", "success")
	m.RecordCacheWrite("l1", "failure")

	if got := testutil.ToFloat64(m.cacheWrites.WithLabelValues("l1", "success")); got != 1 {
		t.Errorf("cache_writes_total{layer=l1,result=success} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.cacheWrites.WithLabelValues("l1", "failure")); got != 1 {
		t.Errorf("cache_writes_total{layer=l1,result=failure} = %v, want 1", got)
	}
}

func TestObserveCacheValueSize(t *testing.T) {
	m := NewMetrics("cache_size_test")
	m.ObserveCacheValueSize("l1", 1024)
	m.ObserveCacheValueSize("l2", 4096)

	// Two distinct layers must produce two histogram series.
	if got := testutil.CollectAndCount(m.cacheValueSize); got != 2 {
		t.Errorf("cache_value_size_bytes series count = %d, want 2 (l1,l2)", got)
	}
}

func TestObserveCacheGetDuration(t *testing.T) {
	m := NewMetrics("cache_duration_test")
	m.ObserveCacheGetDuration("l1", 5*time.Millisecond)
	m.ObserveCacheGetDuration("l2", 50*time.Millisecond)

	if got := testutil.CollectAndCount(m.cacheGetDuration); got != 2 {
		t.Errorf("cache_get_duration_seconds series count = %d, want 2 (l1,l2)", got)
	}
}

func TestSetCircuitBreakerState(t *testing.T) {
	m := NewMetrics("cb_state_test")

	m.SetCircuitBreakerState("tenant-a", "test-openai", 1)
	if got := testutil.ToFloat64(m.providerCircuitState.WithLabelValues("tenant-a", "test-openai")); got != 1 {
		t.Errorf("provider_circuit_state{tenant_id=tenant-a,provider=test-openai} = %v, want 1 (open)", got)
	}

	// Gauge must reflect the latest value, not accumulate.
	m.SetCircuitBreakerState("tenant-a", "test-openai", 0)
	if got := testutil.ToFloat64(m.providerCircuitState.WithLabelValues("tenant-a", "test-openai")); got != 0 {
		t.Errorf("provider_circuit_state after reset = %v, want 0 (closed)", got)
	}
}
