package config

import (
	"encoding/json"
	"errors"
	"os"
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
	Enabled    bool     `json:"enabled"`
	Keys       []string `json:"keys"`
	Header     string   `json:"header"`
	QueryParam string   `json:"query_param"`
	SkipPaths  []string `json:"skip_paths"`
}

type RateLimitConfig struct {
	Enabled           bool     `json:"enabled"`
	RequestsPerMinute int      `json:"requests_per_minute"`
	Burst             int      `json:"burst"`
	By                string   `json:"by"`
	Header            string   `json:"header"`
	SkipPaths         []string `json:"skip_paths"`
}

type QuotaConfig struct {
	Enabled   bool     `json:"enabled"`
	Requests  int      `json:"requests"`
	Window    Duration `json:"window"`
	By        string   `json:"by"`
	Header    string   `json:"header"`
	SkipPaths []string `json:"skip_paths"`
}

type MetricsConfig struct {
	Enabled   bool   `json:"enabled"`
	Path      string `json:"path"`
	Namespace string `json:"namespace"`
}

type GatewayConfig struct {
	OpenAIPathPrefix    string        `json:"openai_path_prefix"`
	AnthropicPathPrefix string        `json:"anthropic_path_prefix"`
	AnthropicProvider   string        `json:"anthropic_provider"`
	ProviderHeader      string        `json:"provider_header"`
	ProviderQuery       string        `json:"provider_query"`
	DefaultProvider     string        `json:"default_provider"`
	AgentToProdPrefix   string        `json:"agent_to_prod_prefix"`
	AgentToMcpPrefix    string        `json:"agent_to_mcp_prefix"`
	AgentToProdUpstream string        `json:"agent_to_prod_upstream"`
	AgentToMcpUpstream  string        `json:"agent_to_mcp_upstream"`
	Routing             RoutingConfig `json:"routing"`
	MCPGuard            MCPGuardConfig `json:"mcp_guard"`
}

type ProviderConfig struct {
	BaseURL    string            `json:"base_url"`
	WSBaseURL  string            `json:"ws_base_url"`
	APIKey     string            `json:"api_key"`
	AuthHeader string            `json:"auth_header"`
	AuthScheme string            `json:"auth_scheme"`
	Headers    map[string]string `json:"headers"`
	Weight     int               `json:"weight"`      // For weighted routing
	InputCost  float64           `json:"input_cost"`  // USD per 1M input tokens
	OutputCost float64           `json:"output_cost"` // USD per 1M output tokens
}

type RoutingConfig struct {
	Enabled        bool                 `json:"enabled"`
	Strategy       string               `json:"strategy"` // round-robin, least-latency, weighted, cost-optimized, priority
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

type MCPGuardConfig struct {
	Enabled            bool                      `json:"enabled"`
	HealthCheck        MCPHealthCheckConfig      `json:"health_check"`
	CircuitBreaker     CircuitBreakerConfig      `json:"circuit_breaker"`
	Timeout            MCPTimeoutConfig          `json:"timeout"`
	ConnectionPool     MCPConnectionPoolConfig   `json:"connection_pool"`
	AnomalyDetection   MCPAnomalyDetectionConfig `json:"anomaly_detection"`
	FallbackBehavior   MCPFallbackBehavior       `json:"fallback_behavior"`
}

type MCPHealthCheckConfig struct {
	Enabled            bool     `json:"enabled"`
	Interval           Duration `json:"interval"`
	Timeout            Duration `json:"timeout"`
	HealthyThreshold   int      `json:"healthy_threshold"`
	UnhealthyThreshold int      `json:"unhealthy_threshold"`
	Endpoint           string   `json:"endpoint"`
}

type MCPTimeoutConfig struct {
	Connect Duration `json:"connect"`
	Read    Duration `json:"read"`
	Write   Duration `json:"write"`
	Idle    Duration `json:"idle"`
}

type MCPConnectionPoolConfig struct {
	Enabled         bool     `json:"enabled"`
	MaxConnections  int      `json:"max_connections"`
	MaxIdleTime     Duration `json:"max_idle_time"`
	MaxLifetime     Duration `json:"max_lifetime"`
	HealthCheckFreq Duration `json:"health_check_freq"`
}

type MCPAnomalyDetectionConfig struct {
	Enabled              bool     `json:"enabled"`
	ErrorRateThreshold   float64  `json:"error_rate_threshold"`
	LatencyThreshold     Duration `json:"latency_threshold"`
	ConsecutiveErrors    int      `json:"consecutive_errors"`
	AlertWebhook         string   `json:"alert_webhook"`
}

type MCPFallbackBehavior struct {
	Strategy       string   `json:"strategy"`
	CacheTTL       Duration `json:"cache_ttl"`
	AlternativeMCP string   `json:"alternative_mcp"`
}

type GuardrailsConfig struct {
	Enabled           bool                      `json:"enabled"`
	PIIDetection      PIIDetectionConfig        `json:"pii_detection"`
	ContentFilter     ContentFilterConfig       `json:"content_filter"`
	ResponseValidator ResponseValidatorConfig   `json:"response_validator"`
	AnomalyDetection  GuardrailAnomalyConfig    `json:"anomaly_detection"`
	CustomRules       []GuardrailCustomRule     `json:"custom_rules"`
}

type PIIDetectionConfig struct {
	Enabled     bool     `json:"enabled"`
	Redact      bool     `json:"redact"`
	Patterns    []string `json:"patterns"`
	EntityTypes []string `json:"entity_types"`
}

type ContentFilterConfig struct {
	Enabled              bool     `json:"enabled"`
	BlockProfanity       bool     `json:"block_profanity"`
	BlockToxicity        bool     `json:"block_toxicity"`
	BlockPromptInjection bool     `json:"block_prompt_injection"`
	CustomBlocklist      []string `json:"custom_blocklist"`
	ToxicityThreshold    float64  `json:"toxicity_threshold"`
}

type ResponseValidatorConfig struct {
	Enabled           bool     `json:"enabled"`
	MaxTokens         int      `json:"max_tokens"`
	MaxResponseSize   int64    `json:"max_response_size"`
	ValidateJSON      bool     `json:"validate_json"`
	ValidateStructure bool     `json:"validate_structure"`
	RequiredFields    []string `json:"required_fields"`
}

type GuardrailAnomalyConfig struct {
	Enabled               bool     `json:"enabled"`
	MaxRequestsPerMinute  int      `json:"max_requests_per_minute"`
	MaxTokensPerRequest   int      `json:"max_tokens_per_request"`
	SuspiciousPatterns    []string `json:"suspicious_patterns"`
	BlockRepeatedRequests bool     `json:"block_repeated_requests"`
	BlockRapidRequests    bool     `json:"block_rapid_requests"`
}

type GuardrailCustomRule struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Pattern     string `json:"pattern"`
	Action      string `json:"action"`
	Message     string `json:"message"`
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
	Enabled    bool             `json:"enabled"`
	Mode       string           `json:"mode"`
	Guardrails GuardrailsConfig `json:"guardrails"`
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
		},
		Quota: QuotaConfig{
			Enabled:   false,
			Requests:  10000,
			Window:    Duration{Duration: 24 * time.Hour},
			By:        "auth",
			SkipPaths: []string{"/healthz", "/metrics"},
		},
		Metrics: MetricsConfig{
			Enabled:   true,
			Path:      "/metrics",
			Namespace: "gateyes",
		},
		Gateway: GatewayConfig{
			OpenAIPathPrefix:    "/v1",
			AnthropicPathPrefix: "/anthropic",
			AnthropicProvider:   "anthropic",
			ProviderHeader:      "X-gateyes-Provider",
			ProviderQuery:       "provider",
			DefaultProvider:     "",
			AgentToProdPrefix:   "/prod",
			AgentToMcpPrefix:    "/mcp",
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
		if c.RateLimit.RequestsPerMinute <= 0 {
			return errors.New("rate_limit.requests_per_minute must be positive")
		}
	}
	if c.Quota.Enabled {
		if c.Quota.Requests <= 0 {
			return errors.New("quota.requests must be positive")
		}
		if c.Quota.Window.Duration <= 0 {
			return errors.New("quota.window must be positive")
		}
	}
	return nil
}
