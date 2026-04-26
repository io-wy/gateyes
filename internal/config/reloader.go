package config

import (
	"context"
	"fmt"
	"log/slog"
)

// Reloadable is implemented by services that can update their runtime
// configuration without a full process restart.
type Reloadable interface {
	Name() string
	Reload(cfg *Config) error
}

// Reloader coordinates hot-reload of runtime-safe configuration.
type Reloader struct {
	configPath string
	items      []Reloadable
}

// NewReloader creates a reloader bound to a config file path.
func NewReloader(configPath string) *Reloader {
	return &Reloader{configPath: configPath}
}

// Register adds a Reloadable service. Safe to call before or after Init.
func (r *Reloader) Register(items ...Reloadable) {
	r.items = append(r.items, items...)
}

// Reload re-reads the config file and pushes changes to all registered
// services. Returns the first error encountered but continues trying
// every registered item.
func (r *Reloader) Reload(ctx context.Context) error {
	cfg, err := Load(r.configPath)
	if err != nil {
		return fmt.Errorf("reload config: %w", err)
	}

	var firstErr error
	for _, item := range r.items {
		if err := item.Reload(cfg); err != nil {
			slog.Error("hot reload failed", "component", item.Name(), "error", err)
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", item.Name(), err)
			}
		} else {
			slog.Info("hot reload succeeded", "component", item.Name())
		}
	}
	return firstErr
}
