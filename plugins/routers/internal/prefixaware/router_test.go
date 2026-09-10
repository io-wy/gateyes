package prefixaware

import (
	"context"
	"strings"
	"sync"
	"testing"

	pluginv1 "github.com/gateyes/gateway/pkg/plugin/v1"
)

func candidates(names ...string) []*pluginv1.Candidate {
	result := make([]*pluginv1.Candidate, len(names))
	for i, name := range names {
		result[i] = &pluginv1.Candidate{Name: name, Healthy: true}
	}
	return result
}

func request(prompt string, names ...string) *pluginv1.OrderCandidatesRequest {
	return &pluginv1.OrderCandidatesRequest{Candidates: candidates(names...), Context: &pluginv1.RouteContext{InputText: prompt}}
}

func TestRouterSeedsTrieBelowThresholdThenPinsSharedPrefix(t *testing.T) {
	router := New(Config{PrefixMinMatchLength: 128, ChunkSize: 128})
	base := strings.Repeat("x", 128)

	first, err := router.OrderCandidates(context.Background(), request(base+strings.Repeat("y", 72), "engine1", "engine2"))
	if err != nil || first.OrderedNames[0] != "engine1" {
		t.Fatalf("first route = (%v,%v), want engine1", first, err)
	}
	second, err := router.OrderCandidates(context.Background(), request(base+strings.Repeat("z", 112), "engine2", "engine1"))
	if err != nil || second.OrderedNames[0] != "engine1" {
		t.Fatalf("shared-prefix route = (%v,%v), want engine1", second, err)
	}
}

func TestRouterFallsBackToLowestObservedQPSBelowThreshold(t *testing.T) {
	router := New(Config{PrefixMinMatchLength: 4096})
	for range 3 {
		if _, err := router.OrderCandidates(context.Background(), request("short", "engine1")); err != nil {
			t.Fatal(err)
		}
	}
	got, err := router.OrderCandidates(context.Background(), request("different", "engine1", "engine2"))
	if err != nil || got.OrderedNames[0] != "engine2" {
		t.Fatalf("QPS fallback = (%v,%v), want engine2", got, err)
	}
}

func TestRouterIgnoresMatchedUnavailableProvider(t *testing.T) {
	router := New(Config{PrefixMinMatchLength: 128})
	prompt := strings.Repeat("a", 128)
	_, _ = router.OrderCandidates(context.Background(), request(prompt, "gone"))
	got, err := router.OrderCandidates(context.Background(), request(prompt+"tail", "live"))
	if err != nil || got.OrderedNames[0] != "live" {
		t.Fatalf("candidate churn route = (%v,%v), want live", got, err)
	}
}

func TestRouterHandlesEmptyCandidatesAndContext(t *testing.T) {
	router := New(Config{})
	got, err := router.OrderCandidates(context.Background(), &pluginv1.OrderCandidatesRequest{})
	if err != nil || len(got.OrderedNames) != 0 {
		t.Fatalf("empty route = (%v,%v), want empty success", got, err)
	}
}

func TestRouterPrefersVLLMCompatiblePrefixText(t *testing.T) {
	router := New(Config{PrefixMinMatchLength: 128})
	prefix := strings.Repeat("v", 128)
	first := request("different legacy input", "p1")
	firstPrefix := prefix + "first"
	first.Context.PrefixText = &firstPrefix
	_, _ = router.OrderCandidates(context.Background(), first)

	second := request("another legacy input", "p2", "p1")
	secondPrefix := prefix + "second"
	second.Context.PrefixText = &secondPrefix
	got, err := router.OrderCandidates(context.Background(), second)
	if err != nil || got.OrderedNames[0] != "p1" {
		t.Fatalf("prefix_text route = (%v,%v), want p1", got, err)
	}
}

func TestRouterTreatsPresentEmptyPrefixTextAsVLLMEmptyPrompt(t *testing.T) {
	router := New(Config{PrefixMinMatchLength: 128})
	legacyPrefix := strings.Repeat("x", 128)
	_, _ = router.OrderCandidates(context.Background(), request(legacyPrefix, "p1"))

	empty := ""
	second := request(legacyPrefix, "p2", "p1")
	second.Context.PrefixText = &empty
	got, err := router.OrderCandidates(context.Background(), second)
	if err != nil || got.OrderedNames[0] != "p2" {
		t.Fatalf("present empty prefix_text route = (%v,%v), want QPS fallback p2", got, err)
	}
}

func TestRouterConcurrentCalls(t *testing.T) {
	router := New(Config{PrefixMinMatchLength: 128})
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := router.OrderCandidates(context.Background(), request(strings.Repeat("a", 128)+string(rune(i)), "p1", "p2"))
			if err != nil {
				t.Errorf("OrderCandidates() error: %v", err)
			}
		}()
	}
	wg.Wait()
}
