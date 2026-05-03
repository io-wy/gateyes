package handler

import (
	"testing"
	"time"
)

func TestRecordCacheLookup(t *testing.T) {
	m := NewMetrics("cache_lookup_test")
	m.RecordCacheLookup("l1", "hit")
	m.RecordCacheLookup("l1", "miss")
}

func TestRecordCacheWrite(t *testing.T) {
	m := NewMetrics("cache_write_test")
	m.RecordCacheWrite("l1", "success")
	m.RecordCacheWrite("l1", "failure")
}

func TestObserveCacheValueSize(t *testing.T) {
	m := NewMetrics("cache_size_test")
	m.ObserveCacheValueSize("l1", 1024)
	m.ObserveCacheValueSize("l2", 4096)
}

func TestObserveCacheGetDuration(t *testing.T) {
	m := NewMetrics("cache_duration_test")
	m.ObserveCacheGetDuration("l1", 5*time.Millisecond)
	m.ObserveCacheGetDuration("l2", 50*time.Millisecond)
}

func TestSetCircuitBreakerState(t *testing.T) {
	m := NewMetrics("cb_state_test")
	m.SetCircuitBreakerState("tenant-a", "test-openai", 1)
	m.SetCircuitBreakerState("tenant-a", "test-openai", 0)
}
