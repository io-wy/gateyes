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

	summary := m.CacheSummary()
	if summary.Totals.Lookups.Hit != 2 || summary.Totals.Lookups.Miss != 1 {
		t.Fatalf("cache summary lookups = %+v, want 2 hits and 1 miss", summary.Totals.Lookups)
	}
	if summary.Totals.HitRate != 2.0/3.0 {
		t.Fatalf("cache summary hit rate = %v, want %v", summary.Totals.HitRate, 2.0/3.0)
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

	summary := m.CacheSummary()
	if summary.Totals.LookupAvgMs != 27.5 {
		t.Fatalf("cache summary lookup avg ms = %v, want 27.5", summary.Totals.LookupAvgMs)
	}
}

func TestCacheSummaryByLayer(t *testing.T) {
	m := NewMetrics("cache_summary_test")
	m.RecordCacheLookup("l1", "hit")
	m.RecordCacheLookup("l1", "miss")
	m.RecordCacheLookup("l1_stream", "hit")
	m.RecordCacheLookup("l1_stream", "error")
	m.RecordCacheWrite("l1", "success")
	m.RecordCacheWrite("l1", "error")
	m.ObserveCacheValueSize("l1", 100)
	m.ObserveCacheValueSize("l1", 300)
	m.ObserveCacheGetDuration("l1", 2*time.Millisecond)
	m.ObserveCacheGetDuration("l1", 6*time.Millisecond)

	summary := m.CacheSummary()
	if !summary.Enabled {
		t.Fatal("cache summary should report metrics enabled")
	}
	if len(summary.Layers) != 2 {
		t.Fatalf("cache summary layers = %d, want 2", len(summary.Layers))
	}
	l1 := summary.Layers[0]
	if l1.Layer != "l1" {
		t.Fatalf("first cache layer = %q, want l1", l1.Layer)
	}
	if l1.Lookups.Total != 2 || l1.Lookups.Hit != 1 || l1.Lookups.Miss != 1 {
		t.Fatalf("l1 lookups = %+v, want total 2 hit 1 miss 1", l1.Lookups)
	}
	if l1.Writes.Success != 1 || l1.Writes.Error != 1 {
		t.Fatalf("l1 writes = %+v, want success 1 error 1", l1.Writes)
	}
	if l1.ValueAvgBytes != 200 {
		t.Fatalf("l1 value avg bytes = %v, want 200", l1.ValueAvgBytes)
	}
	if l1.LookupAvgMs != 4 {
		t.Fatalf("l1 lookup avg ms = %v, want 4", l1.LookupAvgMs)
	}
	if summary.Totals.Lookups.Total != 4 {
		t.Fatalf("total lookups = %d, want 4", summary.Totals.Lookups.Total)
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

func TestEventBusMetrics(t *testing.T) {
	m := NewMetrics("eventbus_test")

	m.IncEventBusDropped()
	m.IncEventBusDropped()
	m.IncEventBusProcessed()
	m.IncEventBusPanics()
	m.SetEventBusQueueSize(7)

	if got := testutil.ToFloat64(m.eventBusDropped); got != 2 {
		t.Errorf("eventbus_dropped_total = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.eventBusProcessed); got != 1 {
		t.Errorf("eventbus_processed_total = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.eventBusPanics); got != 1 {
		t.Errorf("eventbus_panics_total = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.eventBusQueueSize); got != 7 {
		t.Errorf("eventbus_queue_size = %v, want 7", got)
	}
}
