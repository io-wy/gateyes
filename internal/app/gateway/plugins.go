package gateway

import (
	"context"
	"log/slog"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/domain/plugin"
	"github.com/gateyes/gateway/internal/repository"
	grpcplugin "github.com/gateyes/gateway/internal/service/extension/plugin/grpc"
	wasmplugin "github.com/gateyes/gateway/internal/service/extension/plugin/wasm"
)

type compositePluginManager struct {
	grpcMgr *grpcplugin.Manager
	wasmMgr *wasmplugin.Manager
}

func NewCompositePluginManager(grpcMgr *grpcplugin.Manager, wasmMgr *wasmplugin.Manager) plugin.Manager {
	if grpcMgr == nil && wasmMgr == nil {
		return nil
	}
	return &compositePluginManager{grpcMgr: grpcMgr, wasmMgr: wasmMgr}
}

func (c *compositePluginManager) Router() plugin.Router {
	if c.grpcMgr == nil {
		return nil
	}
	return c.grpcMgr.Router()
}

func (c *compositePluginManager) GetByPhase(phase plugin.Phase) []plugin.Gateway {
	var result []plugin.Gateway
	if c.grpcMgr != nil {
		result = append(result, c.grpcMgr.GetByPhase(phase)...)
	}
	if c.wasmMgr != nil {
		result = append(result, c.wasmMgr.GetByPhase(phase)...)
	}
	return result
}

func (c *compositePluginManager) Close() error {
	var firstErr error
	if c.grpcMgr != nil {
		if err := c.grpcMgr.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if c.wasmMgr != nil {
		if err := c.wasmMgr.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func HydrateMarketplacePlugins(
	ctx context.Context,
	store repository.Store,
	tenantID string,
	grpcCfgs []config.GRPCPluginConfig,
	wasmCfgs []config.WASMPluginConfig,
) ([]config.GRPCPluginConfig, []config.WASMPluginConfig) {
	if tenantID == "" {
		return grpcCfgs, wasmCfgs
	}

	plugins, err := store.ListPlugins(ctx, tenantID, repository.PluginFilter{Enabled: boolPtr(true)})
	if err != nil {
		slog.Warn("failed to load marketplace plugins", "error", err)
		return grpcCfgs, wasmCfgs
	}

	for _, p := range plugins {
		switch p.Type {
		case "grpc":
			if p.Address == "" {
				continue
			}
			grpcCfgs = append(grpcCfgs, config.GRPCPluginConfig{
				Name:    p.Name,
				Type:    "gateway",
				Address: p.Address,
				Timeout: p.TimeoutMs,
				Phases:  p.Phases,
			})
		case "wasm":
			if p.FilePath == "" {
				continue
			}
			wasmCfgs = append(wasmCfgs, config.WASMPluginConfig{
				Name:        p.Name,
				Path:        p.FilePath,
				Phases:      p.Phases,
				TimeoutMs:   p.TimeoutMs,
				MemoryPages: uint32(p.MemoryPages),
			})
		}
	}
	return grpcCfgs, wasmCfgs
}

func boolPtr(v bool) *bool {
	return &v
}

var _ plugin.Manager = (*compositePluginManager)(nil)
