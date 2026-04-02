package config

import (
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server        ServerConfig           `yaml:"server"`
	Database      DatabaseConfig         `yaml:"database"`
	Metrics       MetricsConfig          `yaml:"metrics"`
	Router        RouterConfig           `yaml:"router"`
	Limiter       LimiterConfig          `yaml:"limiter"`
	Alert         AlertConfig            `yaml:"alert"`
	Retry         RetryConfig            `yaml:"retry"`
	CircuitBreaker CircuitBreakerConfig  `yaml:"circuitBreaker"`
	Providers     []ProviderConfig       `yaml:"providers"`
	APIKeys       []APIKeyConfig         `yaml:"apiKeys"`
	Admin         AdminConfig            `yaml:"admin"`
}

type ServerConfig struct {
	ListenAddr   string `yaml:"listenAddr"`
	ReadTimeout  int    `yaml:"readTimeout"`
	WriteTimeout int    `yaml:"writeTimeout"`
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
}

type RouterConfig struct {
	Strategy string `yaml:"strategy"`
}

type LimiterConfig struct {
	GlobalQPS          int `yaml:"globalQPS"`          // 全局默认 QPS
	GlobalTPM          int `yaml:"globalTPM"`          // 全局每分钟 token 上限
	GlobalTokenBurst   int `yaml:"globalTokenBurst"`  // 全局 token 桶突发容量
	PerUserRequestBurst int `yaml:"perUserRequestBurst"` // 每用户请求突发容量
	QueueSize          int `yaml:"queueSize"`         // 队列大小
}

type ProviderConfig struct {
	Name        string  `yaml:"name"`
	Type        string  `yaml:"type"`
	BaseURL     string  `yaml:"baseURL"`
	Endpoint    string  `yaml:"endpoint"` // "chat" or "responses", default "chat"
	APIKey      string  `yaml:"apiKey"`
	Model       string  `yaml:"model"`
	PriceInput  float64 `yaml:"priceInput"`
	PriceOutput float64 `yaml:"priceOutput"`
	MaxTokens   int     `yaml:"maxTokens"`
	Timeout     int     `yaml:"timeout"`
	Enabled     bool    `yaml:"enabled"`
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
	Enabled        bool     `yaml:"enabled"`
	QuotaThreshold float64  `yaml:"quotaThreshold"` // 0.8 = 80%
	WebhookURL     string   `yaml:"webhookURL"`
	WebhookSecret  string   `yaml:"webhookSecret"`
}

type RetryConfig struct {
	MaxRetries     int     `yaml:"maxRetries"`      // 最大重试次数
	InitialDelayMs int     `yaml:"initialDelayMs"`  // 初始退避延迟 ms
	MaxDelayMs     int     `yaml:"maxDelayMs"`      // 最大退避延迟 ms
	BackoffFactor  float64 `yaml:"backoffFactor"`   // 退避系数
}

type CircuitBreakerConfig struct {
	FailureThreshold   int `yaml:"failureThreshold"`   // 连续失败 N 次后熔断
	RecoveryTimeout    int `yaml:"recoveryTimeout"`    // 熔断后恢复尝试间隔秒
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

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	data = replaceEnvVars(data)

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddr:   ":8080",
			ReadTimeout:  30,
			WriteTimeout: 300,
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
		},
		Router: RouterConfig{
			Strategy: "round_robin",
		},
		Limiter: LimiterConfig{
			GlobalQPS:          1000,
			GlobalTPM:          1000000,
			GlobalTokenBurst:   100000,
			PerUserRequestBurst: 100,
			QueueSize:          1000,
		},
		Retry: RetryConfig{
			MaxRetries:     2,
			InitialDelayMs: 100,
			MaxDelayMs:     5000,
			BackoffFactor:  2.0,
		},
		CircuitBreaker: CircuitBreakerConfig{
			FailureThreshold:   5,
			RecoveryTimeout:    60,
			HalfOpenMaxRequests: 1,
		},
		Admin: AdminConfig{
			DefaultTenant:   "default",
			BootstrapKey:    "admin-key-001",
			BootstrapSecret: "admin-secret-001",
		},
	}
}
