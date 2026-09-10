package ordering

import (
	"reflect"
	"testing"

	pluginv1 "github.com/gateyes/gateway/pkg/plugin/v1"
)

func TestUniqueCandidatesDropsInvalidAndDuplicateNames(t *testing.T) {
	p1 := &pluginv1.Candidate{Name: "p1", Weight: 10}
	p2 := &pluginv1.Candidate{Name: "p2", Weight: 20}
	got := UniqueCandidates([]*pluginv1.Candidate{
		nil,
		{Name: ""},
		p1,
		{Name: "p1", Weight: 99},
		p2,
	})

	if !reflect.DeepEqual(got, []*pluginv1.Candidate{p1, p2}) {
		t.Fatalf("UniqueCandidates() = %#v, want first valid candidate per name", got)
	}
}

func TestNamesWithFirstPreservesRemainingInputOrder(t *testing.T) {
	candidates := []*pluginv1.Candidate{{Name: "p3"}, {Name: "p1"}, {Name: "p2"}}
	if got, want := NamesWithFirst(candidates, "p1"), []string{"p1", "p3", "p2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("NamesWithFirst() = %v, want %v", got, want)
	}
}

func TestNamesWithFirstHandlesMissingSelectionAndEmptyInput(t *testing.T) {
	candidates := []*pluginv1.Candidate{{Name: "p1"}, {Name: "p2"}}
	if got, want := NamesWithFirst(candidates, "missing"), []string{"p1", "p2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("NamesWithFirst(missing) = %v, want %v", got, want)
	}
	if got := NamesWithFirst(nil, "p1"); len(got) != 0 {
		t.Fatalf("NamesWithFirst(nil) = %v, want empty", got)
	}
}

func TestEffectiveWeightDefaultsNonPositiveValues(t *testing.T) {
	for _, candidate := range []*pluginv1.Candidate{{Weight: -1}, {Weight: 0}, {Weight: 7}} {
		got := EffectiveWeight(candidate)
		want := float64(candidate.Weight)
		if want <= 0 {
			want = 1
		}
		if got != want {
			t.Fatalf("EffectiveWeight(%d) = %v, want %v", candidate.Weight, got, want)
		}
	}
}
