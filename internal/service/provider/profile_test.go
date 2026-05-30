package provider

import (
	"net/http"
	"testing"

	"github.com/gateyes/gateway/internal/config"
)

func TestProfileHelpersCoverNestedBranches(t *testing.T) {
	payload := map[string]any{}
	applyOpenAIVendorDefaults("ollama", payload, nil)
	if payload["stream_options"] == nil {
		t.Fatalf("applyOpenAIVendorDefaults(ollama) = %+v, want stream_options", payload)
	}

	setDefaultPayloadValue(nil, "x", 1)
	setDefaultPayloadValue(payload, "", 1)
	payload["existing"] = 1
	setDefaultPayloadValue(payload, "existing", 2)
	if payload["existing"] != 1 {
		t.Fatalf("setDefaultPayloadValue(existing) = %#v, want unchanged", payload["existing"])
	}

	src := map[string]any{
		"nested": map[string]any{"add": "new"},
		"list":   []any{map[string]any{"k": "v"}},
	}
	dst := map[string]any{
		"nested": map[string]any{"keep": "old"},
	}
	mergeAnyMap(dst, src)
	if dst["nested"].(map[string]any)["keep"] != "old" || dst["nested"].(map[string]any)["add"] != "new" {
		t.Fatalf("mergeAnyMap() nested = %+v, want merged nested map", dst["nested"])
	}
	if _, ok := dst["list"].([]any); !ok {
		t.Fatalf("mergeAnyMap() list = %#v, want cloned list", dst["list"])
	}
	src["nested"].(map[string]any)["add"] = "mutated"
	if dst["nested"].(map[string]any)["add"] != "new" {
		t.Fatalf("mergeAnyMap() destination mutated by source change = %+v", dst["nested"])
	}

	clonedMap := cloneAnyValue(map[string]any{"nested": map[string]any{"k": "v"}}).(map[string]any)
	clonedSlice := cloneAnyValue([]any{map[string]any{"k": "v"}}).([]any)
	clonedMap["nested"].(map[string]any)["k"] = "changed"
	clonedSlice[0].(map[string]any)["k"] = "changed"
	if clonedMap["nested"].(map[string]any)["k"] != "changed" || clonedSlice[0].(map[string]any)["k"] != "changed" {
		t.Fatalf("cloneAnyValue() mutated clone unexpectedly = map:%+v slice:%+v", clonedMap, clonedSlice)
	}

	headers := http.Header{}
	applyProviderProfile(config.ProviderConfig{
		Type:   "openai",
		Vendor: "ollama",
		Headers: map[string]string{
			"X-Test": "yes",
		},
	}, map[string]any{}, headers)
	if headers.Get("X-Test") != "yes" || headers.Get("X-Gateyes-Vendor") != "ollama" {
		t.Fatalf("applyProviderProfile() headers = %#v, want configured + vendor header", headers)
	}
}
