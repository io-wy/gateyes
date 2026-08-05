package provider

import (
	"testing"

	"github.com/gateyes/gateway/internal/repository"
)

func TestMergeRegistryPatch(t *testing.T) {
	current := repository.ProviderRegistryRecord{Name: "p1", Type: "openai", Model: "m1", Enabled: true, RuntimeConfig: &repository.ProviderRuntimeConfig{Timeout: 5}}
	enabled := false
	model := "m2"
	weight := 7
	patch := RegistryPatch{
		Enabled:       &enabled,
		Model:         &model,
		RoutingWeight: &weight,
		Labels:        map[string]string{"runtime": "vllm"},
	}
	updated := mergeRegistryPatch(current, patch)
	if updated.Model != "m2" || updated.Enabled != false || updated.RoutingWeight != 7 {
		t.Fatalf("mergeRegistryPatch mismatch: model=%s enabled=%v weight=%d", updated.Model, updated.Enabled, updated.RoutingWeight)
	}
	if updated.RuntimeConfig.Labels["runtime"] != "vllm" {
		t.Fatalf("mergeRegistryPatch labels = %#v, want runtime label", updated.RuntimeConfig.Labels)
	}
	if updated.RuntimeConfig.Enabled {
		t.Fatal("mergeRegistryPatch runtime enabled = true, want false")
	}
}
