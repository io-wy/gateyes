package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Server    ServerConfig              `json:"server"`
	Auth      AuthConfig                `json:"auth"`
	RateLimit RateLimitConfig           `json:"rate_limit"`
	Quota     QuotaConfig               `json:"quota"`
	Metrics   MetricsConfig             `json:"metrics"`
	Cache     CacheConfig               `json:"cache"`
	Gateway   GatewayConfig             `json:"gateway"`
	Providers map[string]ProviderConfig `json:"providers"`
	Policy    PolicyConfig              `json:"policy"`
}

type ServerConfig struct {
	ListenAddr   string   `json:"listen_addr"`
	ReadTimeout  Duration `json:"read_timeout"`
	WriteTimeout Duration `json:"write_timeout"`
	IdleTimeout  Duration `json:"idle_timeout"`
}

type AuthConfig struct {
	Enabled     bool                        `json:"enabled"`
	Keys        []string                    `json:"keys"`
	Header      string                      `json:"header"`
	QueryParam  string                      `json:"query_param"`
	SkipPaths   []string                    `json:"skip_paths"`
	VirtualKeys map[string]VirtualKeyConfig `json:"virtual_keys"`
}

type VirtualKeyConfig struct {
	Enabled         bool          `json:"enabled"`
	Description     string        `json:"description"`
	Providers       []string      `json:"providers"`
	DefaultProvider string        `json:"default_provider"`
	Routing         RoutingConfig `json:"routing"`
}

type RateLimitConfig struct {
	Enabled           bool                  `json:"enabled"`
	RequestsPerMinute int                   `json:"requests_per_minute"`
	Burst             int                   `json:"burst"`
	By                string                `json:"by"`
	Header            string                `json:"header"`
	SkipPaths         []string              `json:"skip_paths"`
	Backend           string                `json:"backend"`
	RedisAddr         string                `json:"redis_addr"`
	RedisPassword     string                `json:"redis_password"`
	RedisDB           int                   `json:"redis_db"`
	RedisPrefix       string                `json:"redis_prefix"`
	TenantHeader      string                `json:"tenant_header"`
	DefaultCompletion int                   `json:"default_completion_tokens"`
	Rules             []RateLimitRuleConfig `json:"rules"`
}

type QuotaConfig struct {
	Enabled           bool              `json:"enabled"`
	Requests          int               `json:"requests"`
	Window            Duration          `json:"window"`
	By                string            `json:"by"`
	Header            string            `json:"header"`
	SkipPaths         []string          `json:"skip_paths"`
	Backend           string            `json:"backend"`
	RedisAddr         string            `json:"redis_addr"`
	RedisPassword     string            `json:"redis_password"`
	RedisDB           int               `json:"redis_db"`
	RedisPrefix       string            `json:"redis_prefix"`
	TenantHeader      string            `json:"tenant_header"`
	DefaultCompletion int               `json:"default_completion_tokens"`
	TokensPerDay      int64             `json:"tokens_per_day"`
	TokensPerMonth    int64             `json:"tokens_per_month"`
	Rules             []QuotaRuleConfig `json:"rules"`
}

type RateLimitRuleConfig struct {
	Name              string   `json:"name"`
	Enabled           bool     `json:"enabled"`
	Dimensions        []string `json:"dimensions"`
	RequestsPerSecond int      `json:"requests_per_second"`
	RequestsPerMinute int      `json:"requests_per_minute"`
	TokensPerMinute   int64    `json:"tokens_per_minute"`
	Burst             int      `json:"burst"`
	TenantHeader      string   `json:"tenant_header"`
}

type QuotaRuleConfig struct {
	Name             string   `json:"name"`
	Enabled          bool     `json:"enabled"`
	Dimensions       []string `json:"dimensions"`
	RequestsPerDay   int64    `json:"requests_per_day"`
	RequestsPerMonth int64    `json:"requests_per_month"`
	TokensPerDay     int64    `json:"tokens_per_day"`
	TokensPerMonth   int64    `json:"tokens_per_month"`
	TenantHeader     string   `json:"tenant_header"`
}

type MetricsConfig struct {
	Enabled   bool   `json:"enabled"`
	Path      string `json:"path"`
	Namespace string `json:"namespace"`
}

type GatewayConfig struct {
	OpenAIPathPrefix    string        `json:"openai_path_prefix"`
	ProviderHeader      string        `json:"provider_header"`
	ProviderQuery       string        `json:"provider_query"`
	DefaultProvider     string        `json:"default_provider"`
	AgentToProdPrefix   string        `json:"agent_to_prod_prefix"`
	AgentToProdUpstream string        `json:"agent_to_prod_upstream"`
	Routing             RoutingConfig `json:"routing"`
}

type ProviderConfig struct {
	BaseURL    string            `json:"base_url"`
	WSBaseURL  string            `json:"ws_base_url"`
	APIKey     string            `json:"api_key"`
	AuthHeader string            `json:"auth_header"`
	AuthScheme string            `json:"auth_scheme"`
	Headers    map[string]string `json:"headers"`
	Weight     int               `json:"weight"`
	InputCost  float64           `json:"input_cost"`
	OutputCost float64           `json:"output_cost"`
}

type RoutingConfig struct {
	Enabled        bool                 `json:"enabled"`
	Strategy       string               `json:"strategy"`
	Fallback       []string             `json:"fallback"`
	CustomRules    []CustomRule         `json:"custom_rules"`
	HealthCheck    HealthCheckConfig    `json:"health_check"`
	Retry          RetryConfig          `json:"retry"`
	CircuitBreaker CircuitBreakerConfig `json:"circuit_breaker"`
}

type CustomRule struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Priority    int             `json:"priority"`
	Conditions  []RuleCondition `json:"conditions"`
	Action      RuleAction      `json:"action"`
	Enabled     bool            `json:"enabled"`
}

type RuleCondition struct {
	Type     string `json:"type"`
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

type RuleAction struct {
	Type     string                 `json:"type"`
	Provider string                 `json:"provider,omitempty"`
	Status   int                    `json:"status,omitempty"`
	Message  string                 `json:"message,omitempty"`
	Modify   map[string]interface{} `json:"modify,omitempty"`
}

type HealthCheckConfig struct {
	Enabled            bool     `json:"enabled"`
	Interval           Duration `json:"interval"`
	Timeout            Duration `json:"timeout"`
	UnhealthyThreshold int      `json:"unhealthy_threshold"`
	HealthyThreshold   int      `json:"healthy_threshold"`
}

type RetryConfig struct {
	Enabled      bool     `json:"enabled"`
	MaxRetries   int      `json:"max_retries"`
	InitialDelay Duration `json:"initial_delay"`
	MaxDelay     Duration `json:"max_delay"`
	Multiplier   float64  `json:"multiplier"`
}

type CircuitBreakerConfig struct {
	Enabled          bool     `json:"enabled"`
	FailureThreshold int      `json:"failure_threshold"`
	SuccessThreshold int      `json:"success_threshold"`
	Timeout          Duration `json:"timeout"`
	HalfOpenRequests int      `json:"half_open_requests"`
}

type CacheConfig struct {
	Enabled       bool     `json:"enabled"`
	Backend       string   `json:"backend"`
	TTL           Duration `json:"ttl"`
	MaxSize       int64    `json:"max_size"`
	MaxEntries    int      `json:"max_entries"`
	KeyStrategy   string   `json:"key_strategy"`
	RedisAddr     string   `json:"redis_addr"`
	RedisPassword string   `json:"redis_password"`
	RedisDB       int      `json:"redis_db"`
}

type PolicyConfig struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"`
}

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			d.Duration = 0
			return nil
		}
		dur, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		d.Duration = dur
		return nil
	}
	var seconds float64
	if err := json.Unmarshal(b, &seconds); err != nil {
		return err
	}
	d.Duration = time.Duration(seconds) * time.Second
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

func DefaultConfig() Config {
	return Config{
		Server: ServerConfig{
			ListenAddr:   ":8080",
			ReadTimeout:  Duration{Duration: 30 * time.Second},
			WriteTimeout: Duration{Duration: 30 * time.Second},
			IdleTimeout:  Duration{Duration: 120 * time.Second},
		},
		Auth: AuthConfig{
			Enabled:    false,
			Header:     "Authorization",
			QueryParam: "api_key",
			SkipPaths:  []string{"/healthz", "/metrics"},
		},
		RateLimit: RateLimitConfig{
			Enabled:           false,
			RequestsPerMinute: 120,
			Burst:             120,
			By:                "auth",
			SkipPaths:         []string{"/healthz", "/metrics"},
			Backend:           "memory",
			RedisPrefix:       "gateyes",
			TenantHeader:      "X-Tenant-ID",
			DefaultCompletion: 256,
		},
		Quota: QuotaConfig{
			Enabled:           false,
			Requests:          10000,
			Window:            Duration{Duration: 24 * time.Hour},
			By:                "auth",
			SkipPaths:         []string{"/healthz", "/metrics"},
			Backend:           "memory",
			RedisPrefix:       "gateyes",
			TenantHeader:      "X-Tenant-ID",
			DefaultCompletion: 256,
		},
		Metrics: MetricsConfig{
			Enabled:   true,
			Path:      "/metrics",
			Namespace: "gateyes",
		},
		Gateway: GatewayConfig{
			OpenAIPathPrefix:  "/v1",
			ProviderHeader:    "X-Gateyes-Provider",
			ProviderQuery:     "provider",
			DefaultProvider:   "",
			AgentToProdPrefix: "/prod",
		},
		Policy: PolicyConfig{
			Enabled: false,
			Mode:    "audit",
		},
		Providers: map[string]ProviderConfig{},
	}
}

func Load(path string) (Config, error) {
	cfg := DefaultConfig()
	payload, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if c.Server.ListenAddr == "" {
		return errors.New("server.listen_addr is required")
	}
	if c.RateLimit.Enabled {
		backend := strings.ToLower(strings.TrimSpace(c.RateLimit.Backend))
		if backend == "redis" && strings.TrimSpace(c.RateLimit.RedisAddr) == "" {
			return errors.New("rate_limit.redis_addr is required when backend=redis")
		}

		hasLegacyLimit := c.RateLimit.RequestsPerMinute > 0
		hasRuleLimit := false
		for _, rule := range c.RateLimit.Rules {
			if !rule.Enabled {
				continue
			}
			if rule.RequestsPerSecond > 0 || rule.RequestsPerMinute > 0 || rule.TokensPerMinute > 0 {
				hasRuleLimit = true
			}
		}
		if !hasLegacyLimit && !hasRuleLimit {
			return errors.New("rate_limit requires requests_per_minute or at least one enabled rule")
		}
	}
	if c.Quota.Enabled {
		backend := strings.ToLower(strings.TrimSpace(c.Quota.Backend))
		if backend == "redis" && strings.TrimSpace(c.Quota.RedisAddr) == "" {
			return errors.New("quota.redis_addr is required when backend=redis")
		}

		hasLegacyLimit := c.Quota.Requests > 0 && c.Quota.Window.Duration > 0
		hasTokenLimit := c.Quota.TokensPerDay > 0 || c.Quota.TokensPerMonth > 0
		hasRuleLimit := false
		for _, rule := range c.Quota.Rules {
			if !rule.Enabled {
				continue
			}
			if rule.RequestsPerDay > 0 ||
				rule.RequestsPerMonth > 0 ||
				rule.TokensPerDay > 0 ||
				rule.TokensPerMonth > 0 {
				hasRuleLimit = true
			}
		}
		if !hasLegacyLimit && !hasTokenLimit && !hasRuleLimit {
			return errors.New("quota requires requests/window, tokens_per_day/month, or at least one enabled rule")
		}
	}
	for key, virtualConfig := range c.Auth.VirtualKeys {
		if !virtualConfig.Enabled {
			continue
		}
		if len(virtualConfig.Providers) == 0 && strings.TrimSpace(virtualConfig.DefaultProvider) == "" {
			return fmt.Errorf("auth.virtual_keys[%s] requires providers or default_provider", key)
		}
	}
	return nil
}
