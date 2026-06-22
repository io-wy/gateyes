package config

import "testing"

// TestLoadRealProviders loads the repository's own configs/config.example.yaml and
// asserts it stays parseable with structurally complete providers.
//
// Requirement: config.example.yaml is the reference config shipped to users. If a
// ProviderConfig field is renamed or removed without updating the example, anyone
// copying it can no longer boot the gateway. This pins the implicit "example config
// must not drift" contract as a regression gate (see CLAUDE.md config-sync rule),
// replacing the previous version that only printed the fields without asserting.
func TestLoadRealProviders(t *testing.T) {
	cfg, err := Load("../../../configs/config.example.yaml")
	if err != nil {
		t.Fatalf("Load(config.example.yaml) error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load(config.example.yaml) = nil config")
	}
	if len(cfg.Providers) == 0 {
		t.Fatal("config.example.yaml defines no providers, want at least one reference provider")
	}

	for i, p := range cfg.Providers {
		if p.Name == "" {
			t.Errorf("providers[%d].Name is empty", i)
		}
		if p.Type == "" {
			t.Errorf("providers[%d] (%q).Type is empty", i, p.Name)
		}
		if p.Model == "" {
			t.Errorf("providers[%d] (%q).Model is empty", i, p.Name)
		}
	}
}
