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

// ConfigValidator is implemented by services that can validate a new
// configuration before it is applied.
type ConfigValidator interface {
	Validate(cfg *Config) error
}

// Reloader coordinates hot-reload of runtime-safe configuration.
type Reloader struct {
	configPath string
	items      []Reloadable
	cfg        *Config // cached current config
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
// services using two-phase commit: validate all, then apply all.
// If an apply fails, already-applied components are rolled back to the
// previous configuration.
func (r *Reloader) Reload(ctx context.Context) error {
	newCfg, err := Load(r.configPath)
	if err != nil {
		return fmt.Errorf("reload config: %w", err)
	}

	// Phase 1: Validate
	for _, item := range r.items {
		if v, ok := item.(ConfigValidator); ok {
			if err := v.Validate(newCfg); err != nil {
				return fmt.Errorf("validate %s: %w", item.Name(), err)
			}
		}
	}

	// Phase 2: Apply (with rollback on failure)
	oldCfg := r.cfg
	var applied []Reloadable
	var firstErr error
	for _, item := range r.items {
		if err := item.Reload(newCfg); err != nil {
			slog.Error("hot reload failed", "component", item.Name(), "error", err)
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", item.Name(), err)
			}
			// Rollback already applied components
			for _, a := range applied {
				if oldCfg == nil {
					continue
				}
				if rbErr := a.Reload(oldCfg); rbErr != nil {
					slog.Error("rollback failed", "component", a.Name(), "error", rbErr)
				} else {
					slog.Info("rollback succeeded", "component", a.Name())
				}
			}
			break
		}
		applied = append(applied, item)
		slog.Info("hot reload succeeded", "component", item.Name())
	}

	if firstErr == nil {
		r.cfg = newCfg
	}
	return firstErr
}
