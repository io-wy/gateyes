package gateway

import (
	"context"
	"log/slog"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/guardrail"
	"github.com/gateyes/gateway/internal/service/provider"
)

func SeedConfiguredAPIKeys(ctx context.Context, store repository.IdentityStore, tenantID string, configured []config.APIKeyConfig) error {
	for _, item := range configured {
		if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
			TenantID:   tenantID,
			Key:        item.Key,
			SecretHash: repository.HashSecret(item.Secret),
			Name:       "bootstrap-" + item.Key,
			Role:       repository.RoleTenantUser,
			Quota:      item.Quota,
			QPS:        item.QPS,
			Models:     item.Models,
		}); err != nil {
			return err
		}
	}
	return nil
}

func SeedBootstrapAdmin(ctx context.Context, store repository.IdentityStore, tenantID string, cfg config.AdminConfig) error {
	if cfg.BootstrapKey == "" || cfg.BootstrapSecret == "" {
		return nil
	}

	return store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID:   tenantID,
		Key:        cfg.BootstrapKey,
		SecretHash: repository.HashSecret(cfg.BootstrapSecret),
		Name:       "bootstrap-admin",
		Role:       repository.RoleSuperAdmin,
		Quota:      -1,
		QPS:        0,
		Models:     nil,
	})
}

func EnabledProviderNames(providers []config.ProviderConfig) []string {
	names := make([]string, 0, len(providers))
	for _, provider := range providers {
		if provider.Enabled {
			names = append(names, provider.Name)
		}
	}
	return names
}

func SeedTenantProviders(ctx context.Context, store repository.TenantStore, tenantID string, names []string) error {
	existing, err := store.ListTenantProviders(ctx, tenantID)
	if err != nil {
		return err
	}
	merged := append([]string(nil), existing...)
	seen := make(map[string]struct{}, len(existing))
	for _, name := range existing {
		seen[name] = struct{}{}
	}
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		merged = append(merged, name)
	}
	return store.ReplaceTenantProviders(ctx, tenantID, merged)
}

func BuildGuardrails(cfgs []config.GuardrailConfig) *guardrail.Manager {
	if len(cfgs) == 0 {
		return nil
	}
	chain := make([]guardrail.Guardrail, 0, len(cfgs))
	for _, c := range cfgs {
		switch c.Type {
		case "regex":
			chain = append(chain, guardrail.NewRegexBlocklist(c.Name, c.RequestPatterns, c.ResponsePatterns))
		case "wasm":
			g, err := guardrail.NewWASMGuardrail(c.Name, c.Path, c.TimeoutMs, c.MemoryPages)
			if err != nil {
				slog.Warn("failed to load wasm guardrail, skipping", "name", c.Name, "error", err)
				continue
			}
			chain = append(chain, g)
		default:
			slog.Warn("unsupported guardrail type, skipping", "name", c.Name, "type", c.Type)
		}
	}
	if len(chain) == 0 {
		return nil
	}
	return guardrail.New(chain)
}

func SeedProviderRegistry(ctx context.Context, store repository.ProviderRegistryStore, providers []config.ProviderConfig) error {
	for _, item := range providers {
		record := provider.DefaultRegistryRecordFromConfig(item)
		existing, err := store.GetProviderRegistry(ctx, item.Name)
		if err == nil {
			record.RuntimeConfig = existing.RuntimeConfig
			record.CreatedAt = existing.CreatedAt
		} else if err != repository.ErrNotFound {
			return err
		}
		if err := store.UpsertProviderRegistry(ctx, record); err != nil {
			return err
		}
	}
	return nil
}
