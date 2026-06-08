package config

import (
	"testing"
)

func TestValidate_NilConfig(t *testing.T) {
	var cfg *Config
	if err := cfg.Validate(); err == nil {
		t.Error("nil config should return error")
	}
}

func TestValidate_EmptyListenAddr(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.ListenAddr = ""
	if err := cfg.Validate(); err == nil {
		t.Error("empty listen addr should be rejected")
	}
}

func TestValidate_NegativeTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = []ProviderConfig{{Name: "p1", Type: "openai", Timeout: -1}}
	if err := cfg.Validate(); err == nil {
		t.Error("negative provider timeout should be rejected")
	}
}

func TestValidate_DuplicateProviderName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = []ProviderConfig{
		{Name: "dup", Type: "openai"},
		{Name: "dup", Type: "openai"},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("duplicate provider names should be rejected")
	}
}

func TestValidate_UnsupportedCacheBackend(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Cache.Backend = "redis"
	if err := cfg.Validate(); err == nil {
		t.Error("unsupported cache backend should be rejected")
	}
}

func TestValidate_UnsupportedEndpoint(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Providers = []ProviderConfig{{Name: "p1", Type: "openai", Endpoint: "unknown"}}
	if err := cfg.Validate(); err == nil {
		t.Error("unsupported endpoint should be rejected")
	}
}

func TestValidate_UnsupportedRankerMethod(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Router.Ranker.Method = "magic"
	if err := cfg.Validate(); err == nil {
		t.Error("unsupported ranker method should be rejected")
	}
}

func TestValidate_NegativeLimiterValues(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Limiter.GlobalQPS = -1
	if err := cfg.Validate(); err == nil {
		t.Error("negative limiter value should be rejected")
	}
}

func TestValidate_NegativeRetryValues(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Retry.MaxRetries = -1
	if err := cfg.Validate(); err == nil {
		t.Error("negative retry value should be rejected")
	}
}

func TestValidate_NegativeCircuitBreakerValues(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CircuitBreaker.FailureThreshold = -1
	if err := cfg.Validate(); err == nil {
		t.Error("negative circuit breaker value should be rejected")
	}
}

func TestValidate_NegativeHealthCheckValues(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HealthCheck.IntervalSeconds = -1
	if err := cfg.Validate(); err == nil {
		t.Error("negative health check value should be rejected")
	}
}
