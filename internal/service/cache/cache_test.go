package cache

import (
	"strings"
	"testing"
)

func TestBuildKey_DifferentTenants(t *testing.T) {
	a := BuildKey(KeyInput{TenantID: "tenant-a", Model: "gpt-4o-mini", PromptCanon: `{"x":1}`, Surface: "responses"})
	b := BuildKey(KeyInput{TenantID: "tenant-b", Model: "gpt-4o-mini", PromptCanon: `{"x":1}`, Surface: "responses"})
	if a == b {
		t.Fatalf("expected distinct keys for distinct tenants, got %s == %s", a, b)
	}
}

func TestBuildKey_DifferentModels(t *testing.T) {
	a := BuildKey(KeyInput{TenantID: "t", Model: "gpt-4o-mini", PromptCanon: `{"x":1}`, Surface: "responses"})
	b := BuildKey(KeyInput{TenantID: "t", Model: "gpt-4o", PromptCanon: `{"x":1}`, Surface: "responses"})
	if a == b {
		t.Fatalf("expected distinct keys for distinct models")
	}
}

func TestBuildKey_DifferentStreamFlag(t *testing.T) {
	base := KeyInput{TenantID: "t", Model: "gpt-4o", PromptCanon: `{"x":1}`, Surface: "responses"}
	stream := base
	stream.Stream = true
	if BuildKey(base) == BuildKey(stream) {
		t.Fatalf("expected stream flag to change key")
	}
}

func TestBuildKey_DifferentSurfaces(t *testing.T) {
	a := BuildKey(KeyInput{TenantID: "t", Model: "m", PromptCanon: "p", Surface: "responses"})
	b := BuildKey(KeyInput{TenantID: "t", Model: "m", PromptCanon: "p", Surface: "chat_completions"})
	if a == b {
		t.Fatalf("expected surface to change key")
	}
}

func TestBuildKey_PrefixIsStable(t *testing.T) {
	k := BuildKey(KeyInput{TenantID: "t", Model: "m", PromptCanon: "p"})
	if !strings.HasPrefix(k, "gw:l1:") {
		t.Fatalf("expected gw:l1: prefix, got %q", k)
	}
}

func TestBuildKey_FieldSeparator_PreventsCollisions(t *testing.T) {
	// Without a delimiter "ab"+"cd" would equal "abc"+"d". Confirm we don't
	// regress to that.
	a := BuildKey(KeyInput{TenantID: "ab", Model: "cd"})
	b := BuildKey(KeyInput{TenantID: "abc", Model: "d"})
	if a == b {
		t.Fatalf("delimiter must prevent ambiguous concatenation")
	}
}

func TestCanonicalizeJSON_SortsObjectKeys(t *testing.T) {
	a, err := CanonicalizeJSON(map[string]any{"b": 1, "a": 2})
	if err != nil {
		t.Fatalf("canon a: %v", err)
	}
	b, err := CanonicalizeJSON(map[string]any{"a": 2, "b": 1})
	if err != nil {
		t.Fatalf("canon b: %v", err)
	}
	if string(a) != string(b) {
		t.Fatalf("expected key order to be ignored, got %s != %s", a, b)
	}
}

func TestCanonicalizeJSON_PreservesArrayOrder(t *testing.T) {
	a, err := CanonicalizeJSON([]any{1, 2, 3})
	if err != nil {
		t.Fatalf("canon a: %v", err)
	}
	b, err := CanonicalizeJSON([]any{3, 2, 1})
	if err != nil {
		t.Fatalf("canon b: %v", err)
	}
	if string(a) == string(b) {
		t.Fatalf("array order must be preserved")
	}
}

func TestCanonicalizeJSON_NestedObjects(t *testing.T) {
	a, err := CanonicalizeJSON(map[string]any{"outer": map[string]any{"z": 1, "a": 2}})
	if err != nil {
		t.Fatalf("canon: %v", err)
	}
	expected := `{"outer":{"a":2,"z":1}}`
	if string(a) != expected {
		t.Fatalf("expected %s, got %s", expected, a)
	}
}

func TestCanonicalizeJSON_HandlesPrimitives(t *testing.T) {
	cases := []any{42, 3.14, "hello", true, nil}
	for _, c := range cases {
		if _, err := CanonicalizeJSON(c); err != nil {
			t.Fatalf("primitive %v: %v", c, err)
		}
	}
}

func TestBuildKey_StableAcrossCalls(t *testing.T) {
	in := KeyInput{TenantID: "t", Model: "m", PromptCanon: "p", Surface: "responses"}
	first := BuildKey(in)
	for i := 0; i < 10; i++ {
		if BuildKey(in) != first {
			t.Fatalf("BuildKey not deterministic at iter %d", i)
		}
	}
}
