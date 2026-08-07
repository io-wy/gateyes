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
  - name: secondary
    type: openai
    baseURL: https://example2.com
    apiKey: sk-second
    model: gpt-4o-mini
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
	if cfg.Cache.Semantic.Enabled {
		t.Fatal("DefaultConfig().Cache.Semantic.Enabled = true, want false")
	}
}

func TestLoadSemanticCacheConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := strings.TrimSpace(`
server:
  listenAddr: :8080
cache:
  enabled: true
  backend: auto
  semantic:
    enabled: true
    backend: pgvector
    embeddingProvider: openai-embeddings
    embeddingModel: text-embedding-3-small
    threshold: 0.93
    maxCandidates: 7
    ttlSeconds: 7200
    writeAsync: true
    allowStream: true
    allowedSurfaces:
      - responses
      - service_responses
    requireServiceOptIn: true
`)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	semantic := cfg.Cache.Semantic
	if !semantic.Enabled || semantic.Backend != "pgvector" || semantic.EmbeddingProvider != "openai-embeddings" {
		t.Fatalf("semantic config = %+v, want enabled pgvector provider", semantic)
	}
	if semantic.Threshold != 0.93 || semantic.MaxCandidates != 7 || semantic.TTLSeconds != 7200 {
		t.Fatalf("semantic numeric config = %+v", semantic)
	}
	if !semantic.WriteAsync || !semantic.AllowStream || !semantic.RequireServiceOptIn {
		t.Fatalf("semantic bool config = %+v", semantic)
	}
	if len(semantic.AllowedSurfaces) != 2 || semantic.AllowedSurfaces[0] != "responses" || semantic.AllowedSurfaces[1] != "service_responses" {
		t.Fatalf("AllowedSurfaces = %#v", semantic.AllowedSurfaces)
	}
}

func TestValidateRejectsUnsupportedValues(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Database.Driver = "oracle"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported database.driver") {
		t.Fatalf("Validate(database driver) error = %v, want unsupported database.driver", err)
	}

	cfg = DefaultConfig()
	cfg.Database.Driver = "postgres"
	cfg.Router.Strategy = "magic"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported router.strategy") {
		t.Fatalf("Validate(router strategy) error = %v, want unsupported router.strategy", err)
	}

	cfg = DefaultConfig()
	cfg.Database.Driver = "postgres"
	cfg.Providers = []ProviderConfig{{Name: "p1", Type: "unknown"}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported provider type") {
		t.Fatalf("Validate(provider type) error = %v, want unsupported provider type", err)
	}

	cfg = DefaultConfig()
	cfg.Database.Driver = "postgres"
	cfg.APIKeys = []APIKeyConfig{{Key: "dup"}, {Key: "dup"}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate api key") {
		t.Fatalf("Validate(api key duplicate) error = %v, want duplicate api key", err)
	}
}

func TestValidateAcceptsInferenceRoutingStrategies(t *testing.T) {
	for _, strategy := range []string{
		"least_latency",
		"power_of_two",
		"least_kv_cache",
		"least_gpu_cache",
	} {
		t.Run(strategy, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Database.Driver = "postgres"
			cfg.Router.Strategy = strategy
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate(%s) error = %v", strategy, err)
			}
		})
	}
}

func TestRedisConfig_Enabled(t *testing.T) {
	if (RedisConfig{}).Enabled() {
		t.Error("empty RedisConfig should not be enabled")
	}
	if !(RedisConfig{Addr: "localhost:6379"}).Enabled() {
		t.Error("RedisConfig with Addr should be enabled")
	}
}

func TestLoadRedisConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := strings.TrimSpace(`
server:
  listenAddr: :8080
redis:
  addr: localhost:6379
  password: secret
  db: 2
  poolSize: 20
`)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if got, want := cfg.Redis.Addr, "localhost:6379"; got != want {
		t.Errorf("Redis.Addr = %q, want %q", got, want)
	}
	if got, want := cfg.Redis.Password, "secret"; got != want {
		t.Errorf("Redis.Password = %q, want %q", got, want)
	}
	if got, want := cfg.Redis.DB, 2; got != want {
		t.Errorf("Redis.DB = %d, want %d", got, want)
	}
	if got, want := cfg.Redis.PoolSize, 20; got != want {
		t.Errorf("Redis.PoolSize = %d, want %d", got, want)
	}
	if !cfg.Redis.Enabled() {
		t.Error("Redis should be enabled with addr set")
	}
}
