package sessionaffinity

import (
	"context"
	"reflect"
	"testing"

	pluginv1 "github.com/gateyes/gateway/pkg/plugin/v1"
)

func affinityRequest(model, session string, candidates ...*pluginv1.Candidate) *pluginv1.OrderCandidatesRequest {
	return &pluginv1.OrderCandidatesRequest{Candidates: candidates, Context: &pluginv1.RouteContext{Model: model, SessionId: session}}
}

func TestRouterReturnsStableSelectionIndependentOfInputOrder(t *testing.T) {
	router := New()
	a := []*pluginv1.Candidate{{Name: "p1", Weight: 1}, {Name: "p2", Weight: 1}, {Name: "p3", Weight: 1}}
	b := []*pluginv1.Candidate{{Name: "p3", Weight: 1}, {Name: "p1", Weight: 1}, {Name: "p2", Weight: 1}}
	first, _ := router.OrderCandidates(context.Background(), affinityRequest("m", "session-1", a...))
	second, _ := router.OrderCandidates(context.Background(), affinityRequest("m", "session-1", b...))
	if first.OrderedNames[0] != second.OrderedNames[0] {
		t.Fatalf("selection changed with input order: %v vs %v", first.OrderedNames, second.OrderedNames)
	}
}

func TestRouterWithoutSessionPreservesGatewayOrder(t *testing.T) {
	router := New()
	req := affinityRequest("m", "", &pluginv1.Candidate{Name: "p3"}, &pluginv1.Candidate{Name: "p1"}, &pluginv1.Candidate{Name: "p2"})
	got, err := router.OrderCandidates(context.Background(), req)
	want := []string{"p3", "p1", "p2"}
	if err != nil || !reflect.DeepEqual(got.OrderedNames, want) {
		t.Fatalf("missing-session order = (%v,%v), want %v", got, err, want)
	}
}

func TestRouterOnlyRemapsSessionsAssignedToRemovedCandidate(t *testing.T) {
	router := New()
	all := []*pluginv1.Candidate{{Name: "p1", Weight: 1}, {Name: "p2", Weight: 1}, {Name: "p3", Weight: 1}}
	remaining := []*pluginv1.Candidate{{Name: "p1", Weight: 1}, {Name: "p2", Weight: 1}}
	for i := range 200 {
		session := "session-" + string(rune(i))
		before, _ := router.OrderCandidates(context.Background(), affinityRequest("m", session, all...))
		after, _ := router.OrderCandidates(context.Background(), affinityRequest("m", session, remaining...))
		if before.OrderedNames[0] != "p3" && before.OrderedNames[0] != after.OrderedNames[0] {
			t.Fatalf("session %q moved from %q to %q after unrelated removal", session, before.OrderedNames[0], after.OrderedNames[0])
		}
	}
}

func TestRouterWeightBiasesSelection(t *testing.T) {
	router := New()
	counts := map[string]int{}
	for i := range 2000 {
		session := "session-" + string(rune(i))
		got, _ := router.OrderCandidates(context.Background(), affinityRequest("m", session,
			&pluginv1.Candidate{Name: "small", Weight: 1},
			&pluginv1.Candidate{Name: "large", Weight: 9},
		))
		counts[got.OrderedNames[0]]++
	}
	if counts["large"] < 1500 {
		t.Fatalf("weighted selections = %v, want large provider to dominate", counts)
	}
}

func TestRouterHandlesEmptyRequest(t *testing.T) {
	got, err := New().OrderCandidates(context.Background(), nil)
	if err != nil || len(got.OrderedNames) != 0 {
		t.Fatalf("empty request = (%v,%v), want empty success", got, err)
	}
}
