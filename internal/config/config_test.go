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

func TestLoadIngressAndDiscoveryConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := strings.TrimSpace(`
server:
  listenAddr: :8080
ingress:
  enabled: true
  class: gateyes
  watchNamespace: default
  tlsEnabled: true
discovery:
  type: kubernetes
  kubernetes:
    namespace: prod
proxy:
  connectTimeout: 10
  readTimeout: 120
  maxBodySize: 52428800
`)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !cfg.Ingress.Enabled {
		t.Error("Ingress.Enabled should be true")
	}
	if got, want := cfg.Ingress.Class, "gateyes"; got != want {
		t.Errorf("Ingress.Class = %q, want %q", got, want)
	}
	if got, want := cfg.Ingress.WatchNamespace, "default"; got != want {
		t.Errorf("Ingress.WatchNamespace = %q, want %q", got, want)
	}
	if !cfg.Ingress.TLSEnabled {
		t.Error("Ingress.TLSEnabled should be true")
	}
	if got, want := cfg.Discovery.Type, "kubernetes"; got != want {
		t.Errorf("Discovery.Type = %q, want %q", got, want)
	}
	if got, want := cfg.Discovery.K8s.Namespace, "prod"; got != want {
		t.Errorf("Discovery.K8s.Namespace = %q, want %q", got, want)
	}
	if got, want := cfg.Proxy.ConnectTimeout, 10; got != want {
		t.Errorf("Proxy.ConnectTimeout = %d, want %d", got, want)
	}
	if got, want := cfg.Proxy.ReadTimeout, 120; got != want {
		t.Errorf("Proxy.ReadTimeout = %d, want %d", got, want)
	}
	if got, want := cfg.Proxy.MaxBodySize, int64(52428800); got != want {
		t.Errorf("Proxy.MaxBodySize = %d, want %d", got, want)
	}
}

func TestValidateIngressDiscoveryProxy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Discovery.Type = "unknown"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported discovery.type") {
		t.Fatalf("Validate(discovery type) error = %v, want unsupported discovery.type", err)
	}

	cfg = DefaultConfig()
	cfg.Proxy.ConnectTimeout = -1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "proxy timeout values must be >= 0") {
		t.Fatalf("Validate(proxy timeout) error = %v", err)
	}

	cfg = DefaultConfig()
	cfg.Proxy.MaxBodySize = -1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "proxy.maxBodySize must be >= 0") {
		t.Fatalf("Validate(proxy body size) error = %v", err)
	}
}

func TestDefaultConfig_IngressDiscoveryProxyDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Ingress.Enabled {
		t.Error("Default Ingress.Enabled should be false")
	}
	if got, want := cfg.Ingress.Class, "gateyes"; got != want {
		t.Errorf("Default Ingress.Class = %q, want %q", got, want)
	}
	if got, want := cfg.Discovery.Type, "kubernetes"; got != want {
		t.Errorf("Default Discovery.Type = %q, want %q", got, want)
	}
	if got, want := cfg.Proxy.ConnectTimeout, 5; got != want {
		t.Errorf("Default Proxy.ConnectTimeout = %d, want %d", got, want)
	}
	if got, want := cfg.Proxy.MaxIdleConns, 100; got != want {
		t.Errorf("Default Proxy.MaxIdleConns = %d, want %d", got, want)
	}
}
