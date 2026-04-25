package config

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server         ServerConfig         `yaml:"server"`
	Database       DatabaseConfig       `yaml:"database"`
	Metrics        MetricsConfig        `yaml:"metrics"`
	Tracing        TracingConfig        `yaml:"tracing"`
	Router         RouterConfig         `yaml:"router"`
	Limiter        LimiterConfig        `yaml:"limiter"`
	Alert          AlertConfig          `yaml:"alert"`
	HealthCheck    HealthCheckConfig    `yaml:"healthCheck"`
	Retry          RetryConfig          `yaml:"retry"`
	CircuitBreaker CircuitBreakerConfig `yaml:"circuitBreaker"`
	Providers      []ProviderConfig     `yaml:"providers"`
	APIKeys        []APIKeyConfig       `yaml:"apiKeys"`
	Admin          AdminConfig          `yaml:"admin"`
}

type ServerConfig struct {
	ListenAddr      string `yaml:"listenAddr"`
	ReadTimeout     int    `yaml:"readTimeout"`
	WriteTimeout    int    `yaml:"writeTimeout"`
	IdleTimeout     int    `yaml:"idleTimeout"`
	ShutdownTimeout int    `yaml:"shutdownTimeout"`
}

type DatabaseConfig struct {
	Driver                 string `yaml:"driver"`
	DSN                    string `yaml:"dsn"`
	AutoMigrate            bool   `yaml:"autoMigrate"`
	MaxOpenConns           int    `yaml:"maxOpenConns"`
	MaxIdleConns           int    `yaml:"maxIdleConns"`
	ConnMaxLifetimeSeconds int    `yaml:"connMaxLifetimeSeconds"`
}

type MetricsConfig struct {
	Namespace string `yaml:"namespace"`
	Enabled   bool   `yaml:"enabled"`
}

type TracingConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Exporter string `yaml:"exporter"` // stdout, otlp
	Endpoint string `yaml:"endpoint"` // OTLP endpoint, e.g. http://localhost:4318
}

type RouterConfig struct {
	Strategy   string           `yaml:"strategy"`
	Ranker     RankerConfig     `yaml:"ranker"`
	RuleEngine RuleEngineConfig `yaml:"ruleEngine"`
}

type RankerConfig struct {
	Enabled bool   `yaml:"enabled"`
	Method  string `yaml:"method"`
}

type RuleEngineConfig struct {
	Enabled bool              `yaml:"enabled"`
	Rules   []RouteRuleConfig `yaml:"rules"`
}

type RouteRuleConfig struct {
	Name   string            `yaml:"name"`
	Match  RouteMatchConfig  `yaml:"match"`
	Action RouteActionConfig `yaml:"action"`
}

type RouteMatchConfig struct {
	Models              []string `yaml:"models"`
	MinPromptTokens     int      `yaml:"minPromptTokens"`
	MaxPromptTokens     int      `yaml:"maxPromptTokens"`
	HasTools            *bool    `yaml:"hasTools"`
	HasImages           *bool    `yaml:"hasImages"`
	HasStructuredOutput *bool    `yaml:"hasStructuredOutput"`
	Stream              *bool    `yaml:"stream"`
	AnyRegex            []string `yaml:"anyRegex"`
}

type RouteActionConfig struct {
	Providers []string `yaml:"providers"`
}

type LimiterConfig struct {
	GlobalQPS           int `yaml:"globalQPS"`           // 全局默认 QPS
	GlobalTPM           int `yaml:"globalTPM"`           // 全局每分钟 token 上限
	GlobalTokenBurst    int `yaml:"globalTokenBurst"`    // 全局 token 桶突发容量
	GlobalRPM           int `yaml:"globalRPM"`           // 全局每分钟请求上限
	GlobalRPMBurst      int `yaml:"globalRPMBurst"`      // 全局 RPM 桶突发容量
	PerUserRequestBurst int `yaml:"perUserRequestBurst"` // 每用户请求突发容量
	TenantTPM           int `yaml:"tenantTPM"`           // 每租户每分钟 token 上限，0=禁用
	TenantTPMBurst      int `yaml:"tenantTPMBurst"`      // 租户 token 桶突发容量
	TenantRPM           int `yaml:"tenantRPM"`           // 每租户每分钟请求上限，0=禁用
	TenantRPMBurst      int `yaml:"tenantRPMBurst"`      // 租户 RPM 桶突发容量
	ProviderTPM         int `yaml:"providerTPM"`         // 每 provider 每分钟 token 上限，0=禁用
	ProviderTPMBurst    int `yaml:"providerTPMBurst"`    // provider token 桶突发容量
	ProviderRPM         int `yaml:"providerRPM"`         // 每 provider 每分钟请求上限，0=禁用
	ProviderRPMBurst    int `yaml:"providerRPMBurst"`    // provider RPM 桶突发容量
	ModelTPM            int `yaml:"modelTPM"`            // 每 model 每分钟 token 上限，0=禁用
	ModelTPMBurst       int `yaml:"modelTPMBurst"`       // model token 桶突发容量
	ModelRPM            int `yaml:"modelRPM"`            // 每 model 每分钟请求上限，0=禁用
	ModelRPMBurst       int `yaml:"modelRPMBurst"`       // model RPM 桶突发容量
	QueueSize           int `yaml:"queueSize"`           // 队列大小
}

type ProviderConfig struct {
	Name          string            `yaml:"name"`
	Type          string            `yaml:"type"`
	Vendor        string            `yaml:"vendor"`
	BaseURL       string            `yaml:"baseURL"`
	GRPCTarget    string            `yaml:"grpcTarget"`
	GRPCUseTLS    bool              `yaml:"grpcUseTLS"`
	GRPCAuthority string            `yaml:"grpcAuthority"`
	Endpoint      string            `yaml:"endpoint"` // "chat" or "responses", default "chat"
	APIKey        string            `yaml:"apiKey"`
	Model         string            `yaml:"model"`
	Weight        int               `yaml:"weight"`
	PriceInput    float64           `yaml:"priceInput"`
	PriceOutput   float64           `yaml:"priceOutput"`
	MaxTokens     int               `yaml:"maxTokens"`
	Timeout       int               `yaml:"timeout"`
	Enabled       bool              `yaml:"enabled"`
	Headers       map[string]string `yaml:"headers"`
	ExtraBody     map[string]any    `yaml:"extraBody"`
	EnvFile       string            `yaml:"envFile"` // 敏感字段外置 .env 文件路径，空则自动尝试 .env1, .env2...
}

type APIKeyConfig struct {
	Key    string   `yaml:"key"`
	Secret string   `yaml:"secret"`
	Quota  int      `yaml:"quota"`
	QPS    int      `yaml:"qps"`
	Models []string `yaml:"models"`
}

type AdminConfig struct {
	DefaultTenant   string `yaml:"defaultTenant"`
	BootstrapKey    string `yaml:"bootstrapKey"`
	BootstrapSecret string `yaml:"bootstrapSecret"`
}

type AlertConfig struct {
	Enabled            bool                `yaml:"enabled"`
	QuotaThreshold     float64             `yaml:"quotaThreshold"` // 0.8 = 80%
	WebhookURL         string              `yaml:"webhookURL"`
	WebhookSecret      string              `yaml:"webhookSecret"`
	ProviderStateURL   string              `yaml:"providerStateURL"`
	BudgetExhaustedURL string              `yaml:"budgetExhaustedURL"`
	RequestEventURL    string              `yaml:"requestEventURL"`
	ErrorEventURL      string              `yaml:"errorEventURL"`
	Channels           []AlertChannelConfig `yaml:"channels"`
	DedupWindowSeconds int                 `yaml:"dedupWindowSeconds"` // 聚合降噪窗口，默认 300
}

type AlertChannelConfig struct {
	Name   string            `yaml:"name"`
	Type   string            `yaml:"type"`   // webhook, slack
	Target string            `yaml:"target"` // URL
	Secret string            `yaml:"secret"`
	Labels map[string]string `yaml:"labels"` // 用于路由过滤，如 severity=critical
}

type HealthCheckConfig struct {
	Enabled          bool `yaml:"enabled"`
	IntervalSeconds  int  `yaml:"intervalSeconds"`
	TimeoutSeconds   int  `yaml:"timeoutSeconds"`
	FailureThreshold int  `yaml:"failureThreshold"`
}

type RetryConfig struct {
	MaxRetries     int     `yaml:"maxRetries"`     // 最大重试次数
	InitialDelayMs int     `yaml:"initialDelayMs"` // 初始退避延迟 ms
	MaxDelayMs     int     `yaml:"maxDelayMs"`     // 最大退避延迟 ms
	BackoffFactor  float64 `yaml:"backoffFactor"`  // 退避系数
}

type CircuitBreakerConfig struct {
	FailureThreshold    int `yaml:"failureThreshold"`    // 连续失败 N 次后熔断
	RecoveryTimeout     int `yaml:"recoveryTimeout"`     // 熔断后恢复尝试间隔秒
	HalfOpenMaxRequests int `yaml:"halfOpenMaxRequests"` // half-open 状态下最大并发探测请求数
}

func replaceEnvVars(data []byte) []byte {
	re := regexp.MustCompile(`\$\{([^}]+)\}`)
	return re.ReplaceAllFunc(data, func(match []byte) []byte {
		varName := string(match[2 : len(match)-1])
		if value := os.Getenv(varName); value != "" {
			return []byte(value)
		}
		return match
	})
}

func (c *Config) hydrateProviderEnvFiles(configPath string) error {
	baseDir := filepath.Dir(configPath)
	for i := range c.Providers {
		p := &c.Providers[i]
		envFile := p.EnvFile
		if envFile == "" {
			envFile = fmt.Sprintf(".env%d", i+1)
		}
		if !filepath.IsAbs(envFile) {
			envFile = filepath.Join(baseDir, envFile)
		}
		envVars, err := loadEnvFile(envFile)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("load env file for provider %s: %w", p.Name, err)
		}
		applyEnvToProvider(envVars, p)
	}
	return nil
}

func loadEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	vars := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		vars[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return vars, nil
}

func applyEnvToProvider(env map[string]string, p *ProviderConfig) {
	for k, v := range env {
		if v == "" {
			continue
		}
		switch strings.ToUpper(k) {
		case "API_KEY":
			p.APIKey = v
		case "BASE_URL":
			p.BaseURL = v
		case "GRPC_TARGET":
			p.GRPCTarget = v
		case "GRPC_AUTHORITY":
			p.GRPCAuthority = v
		case "ENDPOINT":
			p.Endpoint = v
		case "MODEL":
			p.Model = v
		}
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	data = replaceEnvVars(data)

	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix("GATEYES")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, err
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	if err := cfg.hydrateProviderEnvFiles(path); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	if strings.TrimSpace(c.Server.ListenAddr) == "" {
		return fmt.Errorf("server.listenAddr is required")
	}
	if !containsString([]string{"sqlite", "postgres", "mysql", ""}, c.Database.Driver) {
		return fmt.Errorf("unsupported database.driver: %s", c.Database.Driver)
	}
	if !containsString([]string{"round_robin", "random", "least_load", "least_tpm", "cost_based", "sticky", "ml_rank", ""}, c.Router.Strategy) {
		return fmt.Errorf("unsupported router.strategy: %s", c.Router.Strategy)
	}
	if !containsString([]string{"", "none", "ml_rank"}, c.Router.Ranker.Method) {
		return fmt.Errorf("unsupported router.ranker.method: %s", c.Router.Ranker.Method)
	}
	if c.Metrics.Namespace != "" {
		pattern := regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
		if !pattern.MatchString(c.Metrics.Namespace) {
			return fmt.Errorf("invalid metrics.namespace: %s", c.Metrics.Namespace)
		}
	}
	seenProviders := make(map[string]struct{}, len(c.Providers))
	for _, provider := range c.Providers {
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			return fmt.Errorf("provider name is required")
		}
		if _, ok := seenProviders[name]; ok {
			return fmt.Errorf("duplicate provider name: %s", name)
		}
		seenProviders[name] = struct{}{}
		if !containsString([]string{"openai", "anthropic", "grpc", "azure", ""}, strings.ToLower(strings.TrimSpace(provider.Type))) {
			return fmt.Errorf("unsupported provider type for %s: %s", name, provider.Type)
		}
		if endpoint := strings.ToLower(strings.TrimSpace(provider.Endpoint)); endpoint != "" && !containsString([]string{"chat", "responses"}, endpoint) {
			return fmt.Errorf("unsupported provider endpoint for %s: %s", name, provider.Endpoint)
		}
		if provider.Timeout < 0 {
			return fmt.Errorf("provider timeout must be >= 0 for %s", name)
		}
		if provider.MaxTokens < 0 {
			return fmt.Errorf("provider maxTokens must be >= 0 for %s", name)
		}
	}
	seenKeys := make(map[string]struct{}, len(c.APIKeys))
	for _, apiKey := range c.APIKeys {
		key := strings.TrimSpace(apiKey.Key)
		if key == "" {
			return fmt.Errorf("apiKeys.key is required")
		}
		if _, ok := seenKeys[key]; ok {
			return fmt.Errorf("duplicate api key: %s", key)
		}
		seenKeys[key] = struct{}{}
		if apiKey.QPS < 0 || apiKey.Quota < -1 {
			return fmt.Errorf("invalid api key quota/qps for %s", key)
		}
	}
	if c.Limiter.GlobalQPS < 0 || c.Limiter.GlobalTPM < 0 || c.Limiter.QueueSize < 0 {
		return fmt.Errorf("limiter values must be >= 0")
	}
	if c.Retry.MaxRetries < 0 || c.Retry.InitialDelayMs < 0 || c.Retry.MaxDelayMs < 0 {
		return fmt.Errorf("retry values must be >= 0")
	}
	if c.CircuitBreaker.FailureThreshold < 0 || c.CircuitBreaker.RecoveryTimeout < 0 || c.CircuitBreaker.HalfOpenMaxRequests < 0 {
		return fmt.Errorf("circuitBreaker values must be >= 0")
	}
	if c.HealthCheck.IntervalSeconds < 0 || c.HealthCheck.TimeoutSeconds < 0 || c.HealthCheck.FailureThreshold < 0 {
		return fmt.Errorf("healthCheck values must be >= 0")
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddr:      ":8080",
			ReadTimeout:     30,
			WriteTimeout:    300,
			IdleTimeout:     120,
			ShutdownTimeout: 10,
		},
		Database: DatabaseConfig{
			Driver:                 "sqlite",
			DSN:                    "gateyes.db",
			AutoMigrate:            true,
			MaxOpenConns:           10,
			MaxIdleConns:           5,
			ConnMaxLifetimeSeconds: 300,
		},
		Metrics: MetricsConfig{
			Namespace: "gateway",
			Enabled:   true,
		},
		Router: RouterConfig{
			Strategy: "round_robin",
			Ranker: RankerConfig{
				Enabled: false,
				Method:  "",
			},
			RuleEngine: RuleEngineConfig{
				Enabled: false,
			},
		},
		Limiter: LimiterConfig{
			GlobalQPS:           1000,
			GlobalTPM:           1000000,
			GlobalTokenBurst:    100000,
			PerUserRequestBurst: 100,
			QueueSize:           1000,
		},
		Alert: AlertConfig{
			Enabled:        true,
			QuotaThreshold: 0.8,
		},
		HealthCheck: HealthCheckConfig{
			Enabled:          true,
			IntervalSeconds:  60,
			TimeoutSeconds:   15,
			FailureThreshold: 2,
		},
		Retry: RetryConfig{
			MaxRetries:     2,
			InitialDelayMs: 100,
			MaxDelayMs:     5000,
			BackoffFactor:  2.0,
		},
		CircuitBreaker: CircuitBreakerConfig{
			FailureThreshold:    5,
			RecoveryTimeout:     60,
			HalfOpenMaxRequests: 1,
		},
		Admin: AdminConfig{
			DefaultTenant:   "default",
			BootstrapKey:    "admin-key-001",
			BootstrapSecret: "admin-secret-001",
		},
	}
}
