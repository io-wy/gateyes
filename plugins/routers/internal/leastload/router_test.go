package leastload

import (
	"context"
	"reflect"
	"testing"
	"time"

	pluginv1 "github.com/gateyes/gateway/pkg/plugin/v1"
)

func TestRouterPrefersLowerFreshVLLMLoadScore(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	router := New(Config{SignalMaxAge: 10 * time.Second})
	router.now = func() time.Time { return now }
	req := &pluginv1.OrderCandidatesRequest{Candidates: []*pluginv1.Candidate{
		{Name: "busy", Healthy: true, Weight: 100, CurrentLoad: 0, QueueWaiting: 2, QueueRunning: 4, GpuKvCacheUsagePerc: .9, SignalsUpdatedAtUnixMs: now.UnixMilli()},
		{Name: "idle", Healthy: true, Weight: 100, CurrentLoad: 20, QueueWaiting: 0, QueueRunning: 1, GpuKvCacheUsagePerc: .2, SignalsUpdatedAtUnixMs: now.UnixMilli()},
	}}
	got, err := router.OrderCandidates(context.Background(), req)
	if err != nil || got.OrderedNames[0] != "idle" {
		t.Fatalf("OrderCandidates() = (%v,%v), want idle first", got, err)
	}
}

func TestRouterFallsBackToCurrentLoadForStaleSignals(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	router := New(Config{SignalMaxAge: 5 * time.Second})
	router.now = func() time.Time { return now }
	req := &pluginv1.OrderCandidatesRequest{Candidates: []*pluginv1.Candidate{
		{Name: "p1", Healthy: true, Weight: 1, CurrentLoad: 1, QueueWaiting: 99, SignalsUpdatedAtUnixMs: now.Add(-time.Minute).UnixMilli()},
		{Name: "p2", Healthy: true, Weight: 1, CurrentLoad: 2, SignalsUpdatedAtUnixMs: now.Add(-time.Minute).UnixMilli()},
	}}
	got, err := router.OrderCandidates(context.Background(), req)
	if err != nil || got.OrderedNames[0] != "p1" {
		t.Fatalf("stale fallback = (%v,%v), want p1 first", got, err)
	}
}

func TestRouterNormalizesLoadByWeight(t *testing.T) {
	router := New(Config{})
	req := &pluginv1.OrderCandidatesRequest{Candidates: []*pluginv1.Candidate{
		{Name: "small", Healthy: true, Weight: 1, CurrentLoad: 2},
		{Name: "large", Healthy: true, Weight: 10, CurrentLoad: 4},
	}}
	got, err := router.OrderCandidates(context.Background(), req)
	if err != nil || got.OrderedNames[0] != "large" {
		t.Fatalf("weighted load = (%v,%v), want large first", got, err)
	}
}

func TestRouterOrdersHealthyBeforeUnhealthyAndBreaksTiesStably(t *testing.T) {
	router := New(Config{})
	req := &pluginv1.OrderCandidatesRequest{Candidates: []*pluginv1.Candidate{
		{Name: "z", Healthy: true, CurrentLoad: 1, AvgTtftMs: 10},
		{Name: "a", Healthy: false, CurrentLoad: 0},
		{Name: "b", Healthy: true, CurrentLoad: 1, AvgTtftMs: 5},
	}}
	got, err := router.OrderCandidates(context.Background(), req)
	want := []string{"b", "z", "a"}
	if err != nil || !reflect.DeepEqual(got.OrderedNames, want) {
		t.Fatalf("stable ordering = (%v,%v), want %v", got, err, want)
	}
}

func TestRouterHandlesEmptyRequest(t *testing.T) {
	got, err := New(Config{}).OrderCandidates(context.Background(), nil)
	if err != nil || len(got.OrderedNames) != 0 {
		t.Fatalf("empty request = (%v,%v), want empty success", got, err)
	}
}
