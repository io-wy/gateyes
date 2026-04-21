package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceEnvVarsReplacesKnownAndKeepsUnknown(t *testing.T) {
	t.Setenv("GATEYES_TEST_TOKEN", "resolved")

	got := string(replaceEnvVars([]byte("a=${GATEYES_TEST_TOKEN},b=${GATEYES_MISSING}")))
	if got != "a=resolved,b=${GATEYES_MISSING}" {
		t.Fatalf("replaceEnvVars() = %q, want %q", got, "a=resolved,b=${GATEYES_MISSING}")
	}
}

func TestLoadReplacesEnvVarsAndParsesYAML(t *testing.T) {
	t.Setenv("GATEYES_LISTEN", ":9090")
	t.Setenv("GATEYES_PROVIDER_KEY", "provider-secret")
	t.Setenv("GATEYES_GRPC_TARGET", "127.0.0.1:50051")

	path := filepath.Join(t.TempDir(), "config.yaml")
	data := strings.TrimSpace(`
server:
  listenAddr: ${GATEYES_LISTEN}
providers:
  - name: openai-main
    type: openai
    baseURL: https://example.com
    apiKey: ${GATEYES_PROVIDER_KEY}
    model: gpt-test
    enabled: true
  - name: grpc-vllm
    type: grpc
    vendor: vllm
    grpcTarget: ${GATEYES_GRPC_TARGET}
    grpcUseTLS: true
    grpcAuthority: vllm.internal
    model: Qwen/Qwen3-8B
    enabled: true
`)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error: %v", path, err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q) error: %v", path, err)
	}

	if got, want := cfg.Server.ListenAddr, ":9090"; got != want {
		t.Fatalf("Load(%q).Server.ListenAddr = %q, want %q", path, got, want)
	}
	if len(cfg.Providers) != 2 {
		t.Fatalf("Load(%q).Providers length = %d, want %d", path, len(cfg.Providers), 2)
	}
	if got, want := cfg.Providers[0].APIKey, "provider-secret"; got != want {
		t.Fatalf("Load(%q).Providers[0].APIKey = %q, want %q", path, got, want)
	}
	if got, want := cfg.Providers[1].GRPCTarget, "127.0.0.1:50051"; got != want {
		t.Fatalf("Load(%q).Providers[1].GRPCTarget = %q, want %q", path, got, want)
	}
	if !cfg.Providers[1].GRPCUseTLS {
		t.Fatalf("Load(%q).Providers[1].GRPCUseTLS = false, want true", path)
	}
	if got, want := cfg.Providers[1].GRPCAuthority, "vllm.internal"; got != want {
		t.Fatalf("Load(%q).Providers[1].GRPCAuthority = %q, want %q", path, got, want)
	}
}

func TestLoadUsesViperEnvOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := strings.TrimSpace(`
server:
  listenAddr: :8080
metrics:
  namespace: gateway
`)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error: %v", path, err)
	}

	t.Setenv("GATEYES_SERVER_LISTENADDR", ":9191")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q) error: %v", path, err)
	}
	if got, want := cfg.Server.ListenAddr, ":9191"; got != want {
		t.Fatalf("Load(%q).Server.ListenAddr = %q, want %q", path, got, want)
	}
}

func TestDefaultConfigHasExpectedDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if got, want := cfg.Server.ListenAddr, ":8080"; got != want {
		t.Fatalf("DefaultConfig().Server.ListenAddr = %q, want %q", got, want)
	}
	if got, want := cfg.Router.Strategy, "round_robin"; got != want {
		t.Fatalf("DefaultConfig().Router.Strategy = %q, want %q", got, want)
	}
	if got, want := cfg.Admin.DefaultTenant, "default"; got != want {
		t.Fatalf("DefaultConfig().Admin.DefaultTenant = %q, want %q", got, want)
	}
}

func TestValidateRejectsUnsupportedValues(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Database.Driver = "oracle"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported database.driver") {
		t.Fatalf("Validate(database driver) error = %v, want unsupported database.driver", err)
	}

	cfg = DefaultConfig()
	cfg.Router.Strategy = "magic"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported router.strategy") {
		t.Fatalf("Validate(router strategy) error = %v, want unsupported router.strategy", err)
	}

	cfg = DefaultConfig()
	cfg.Providers = []ProviderConfig{{Name: "p1", Type: "unknown"}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported provider type") {
		t.Fatalf("Validate(provider type) error = %v, want unsupported provider type", err)
	}

	cfg = DefaultConfig()
	cfg.APIKeys = []APIKeyConfig{{Key: "dup"}, {Key: "dup"}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate api key") {
		t.Fatalf("Validate(api key duplicate) error = %v, want duplicate api key", err)
	}
}
