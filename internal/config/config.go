package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config contains the minimum runtime configuration for the gateway baseline.
type Config struct {
	Server      ServerConfig
	Auth        AuthConfig
	Scheduler   SchedulerConfig
	Concurrency ConcurrencyConfig
}

type ServerConfig struct {
	Address           string
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

type AuthConfig struct {
	EnableGatewayAuth bool
}

type SchedulerConfig struct {
	DefaultProvider      string
	DefaultChannelID     string
	DefaultUpstreamModel string
}

type ConcurrencyConfig struct {
	GlobalLimit         int
	DefaultChannelLimit int
	DefaultTokenLimit   int
}

func LoadFromEnv() (Config, error) {
	cfg := Config{
		Server: ServerConfig{
			Address:           getEnv("GATEYES_ADDR", ":8080"),
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
			ShutdownTimeout:   5 * time.Second,
		},
		Auth: AuthConfig{
			EnableGatewayAuth: getEnvBool("GATEYES_ENABLE_GATEWAY_AUTH", true),
		},
		Scheduler: SchedulerConfig{
			DefaultProvider:      getEnv("GATEYES_DEFAULT_PROVIDER", "openai"),
			DefaultChannelID:     getEnv("GATEYES_DEFAULT_CHANNEL_ID", "default"),
			DefaultUpstreamModel: getEnv("GATEYES_DEFAULT_UPSTREAM_MODEL", "gpt-4o-mini"),
		},
		Concurrency: ConcurrencyConfig{
			GlobalLimit:         200,
			DefaultChannelLimit: 50,
			DefaultTokenLimit:   20,
		},
	}

	var err error
	cfg.Server.ReadHeaderTimeout, err = getEnvDuration("GATEYES_READ_HEADER_TIMEOUT", cfg.Server.ReadHeaderTimeout)
	if err != nil {
		return Config{}, err
	}
	cfg.Server.WriteTimeout, err = getEnvDuration("GATEYES_WRITE_TIMEOUT", cfg.Server.WriteTimeout)
	if err != nil {
		return Config{}, err
	}
	cfg.Server.IdleTimeout, err = getEnvDuration("GATEYES_IDLE_TIMEOUT", cfg.Server.IdleTimeout)
	if err != nil {
		return Config{}, err
	}
	cfg.Server.ShutdownTimeout, err = getEnvDuration("GATEYES_SHUTDOWN_TIMEOUT", cfg.Server.ShutdownTimeout)
	if err != nil {
		return Config{}, err
	}

	cfg.Concurrency.GlobalLimit, err = getEnvInt("GATEYES_GLOBAL_CONCURRENCY", cfg.Concurrency.GlobalLimit)
	if err != nil {
		return Config{}, err
	}
	cfg.Concurrency.DefaultChannelLimit, err = getEnvInt("GATEYES_CHANNEL_CONCURRENCY", cfg.Concurrency.DefaultChannelLimit)
	if err != nil {
		return Config{}, err
	}
	cfg.Concurrency.DefaultTokenLimit, err = getEnvInt("GATEYES_TOKEN_CONCURRENCY", cfg.Concurrency.DefaultTokenLimit)
	if err != nil {
		return Config{}, err
	}

	// TODO(io): add config file loading and schema validation after baseline is stable.
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		parsed, err := strconv.ParseBool(v)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func getEnvInt(key string, fallback int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be integer: %w", key, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be > 0", key)
	}
	return parsed, nil
}

func getEnvDuration(key string, fallback time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be valid duration: %w", key, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be > 0", key)
	}
	return parsed, nil
}
