package config

import (
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server     ServerConfig      `yaml:"server"`
	Metrics    MetricsConfig     `yaml:"metrics"`
	Router     RouterConfig      `yaml:"router"`
	Limiter    LimiterConfig     `yaml:"limiter"`
	Cache      CacheConfig       `yaml:"cache"`
	Providers  []ProviderConfig  `yaml:"providers"`
	APIKeys    []APIKeyConfig   `yaml:"apiKeys"`
	Admin      AdminConfig       `yaml:"admin"`
}

type ServerConfig struct {
	ListenAddr    string `yaml:"listenAddr"`
	ReadTimeout   int    `yaml:"readTimeout"`
	WriteTimeout  int    `yaml:"writeTimeout"`
}

type MetricsConfig struct {
	Namespace string `yaml:"namespace"`
	Enabled   bool   `yaml:"enabled"`
}

type RouterConfig struct {
	Strategy      string `yaml:"strategy"` // round_robin, random, least_load, cost_based, sticky
	StickySession bool   `yaml:"stickySession"`
}

type LimiterConfig struct {
	GlobalQPS   int `yaml:"globalQPS"`
	GlobalTPM   int `yaml:"globalTPM"` // tokens per minute
	Burst       int `yaml:"burst"`
	QueueSize   int `yaml:"queueSize"`
}

type CacheConfig struct {
	Enabled bool `yaml:"enabled"`
	MaxSize int  `yaml:"maxSize"`
	TTL     int  `yaml:"ttl"` // seconds
}

type ProviderConfig struct {
	Name       string  `yaml:"name"`
	Type       string  `yaml:"type"` // openai, azure, anthropic
	BaseURL    string  `yaml:"baseURL"`
	APIKey     string  `yaml:"apiKey"`
	Model      string  `yaml:"model"`
	Weight     int     `yaml:"weight"`
	PriceInput float64 `yaml:"priceInput"`
	PriceOutput float64 `yaml:"priceOutput"`
	MaxTokens  int     `yaml:"maxTokens"`
	Timeout    int     `yaml:"timeout"`
}

type APIKeyConfig struct {
	Key     string   `yaml:"key"`
	Secret  string   `yaml:"secret"`
	Quota   int      `yaml:"quota"` // total tokens allowed
	QPS     int      `yaml:"qps"`
	Models  []string `yaml:"models"`
}

type AdminConfig struct {
	AdminKey string `yaml:"adminKey"`
}

// 替换环境变量 ${VAR} -> actual value
func replaceEnvVars(data []byte) []byte {
	re := regexp.MustCompile(`\$\{([^}]+)\}`)
	return re.ReplaceAllFunc(data, func(match []byte) []byte {
		varName := string(match[2 : len(match)-1])
		if value := os.Getenv(varName); value != "" {
			return []byte(value)
		}
		return match // keep original if env not set
	})
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// 替换环境变量
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
		Metrics: MetricsConfig{
			Namespace: "gateway",
			Enabled:   true,
		},
		Router: RouterConfig{
			Strategy:      "round_robin",
			StickySession: false,
		},
		Limiter: LimiterConfig{
			GlobalQPS: 1000,
			GlobalTPM: 1000000,
			Burst:     100,
			QueueSize: 1000,
		},
		Cache: CacheConfig{
			Enabled: true,
			MaxSize: 10000,
			TTL:     3600,
		},
		Admin: AdminConfig{
			AdminKey: "admin-secret-key",
		},
	}
}
