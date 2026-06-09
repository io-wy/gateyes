package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/domain/plugin"
	pluginv1 "github.com/gateyes/gateway/pkg/plugin/v1"
)

// Manager manages gRPC plugin connections, health checks, and lifecycle.
type Manager struct {
	clients map[string]*clientWrapper
	mu      sync.RWMutex
	cfg     []config.GRPCPluginConfig
	logger  *slog.Logger
}

type clientWrapper struct {
	name       string
	pluginType string
	phases     []string
	conn       *grpc.ClientConn
	health     plugin.HealthStatus
	timeout    time.Duration
	logger     *slog.Logger
}

// NewManager creates a manager from config. It dials each configured plugin
// and starts a background health-check loop.
func NewManager(cfgs []config.GRPCPluginConfig) (*Manager, error) {
	m := &Manager{
		clients: make(map[string]*clientWrapper),
		cfg:     cfgs,
		logger:  slog.With("component", "grpc_plugin_manager"),
	}
	for _, c := range cfgs {
		if err := m.add(c); err != nil {
			m.logger.Warn("failed to dial grpc plugin", "name", c.Name, "address", c.Address, "error", err)
			// continue: one bad plugin should not prevent others from loading
		}
	}
	// Start periodic health checks.
	go m.healthCheckLoop()
	return m, nil
}

func (m *Manager) add(cfg config.GRPCPluginConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(cfg.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("dial %s: %w", cfg.Address, err)
	}

	timeout := time.Duration(cfg.Timeout) * time.Millisecond
	if timeout <= 0 {
		timeout = plugin.DefaultTimeout
	}

	cw := &clientWrapper{
		name:       cfg.Name,
		pluginType: cfg.Type,
		phases:     cfg.Phases,
		conn:       conn,
		health:     plugin.HealthUnknown,
		timeout:    timeout,
		logger:     m.logger.With("plugin", cfg.Name),
	}

	// Do an initial health check.
	cw.checkHealth(ctx)

	m.mu.Lock()
	m.clients[cfg.Name] = cw
	m.mu.Unlock()
	return nil
}

// GetByType returns the first healthy plugin of the given type, or nil.
// Supported types: "router", and future types registered here.
func (m *Manager) GetByType(pluginType string) *clientWrapper {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, cw := range m.clients {
		if cw.pluginType != pluginType {
			continue
		}
		if cw.health == plugin.HealthHealthy {
			return cw
		}
	}
	return nil
}

// Router returns the first healthy router plugin, or nil.
func (m *Manager) Router() plugin.Router {
	cw := m.GetByType("router")
	if cw == nil {
		return nil
	}
	return &routerPluginClient{wrapper: cw}
}

// GetByPhase returns all healthy gateway plugins subscribed to the given phase.
func (m *Manager) GetByPhase(phase plugin.Phase) []plugin.Gateway {
	m.mu.RLock()
	defer m.mu.RUnlock()

	phaseStr := string(phase)
	var result []plugin.Gateway
	for _, cw := range m.clients {
		if cw.pluginType != "gateway" {
			continue
		}
		if cw.health != plugin.HealthHealthy {
			continue
		}
		// Check if this plugin subscribes to the requested phase.
		subscribed := false
		for _, p := range cw.phases {
			if p == phaseStr {
				subscribed = true
				break
			}
		}
		if !subscribed {
			continue
		}
		result = append(result, &gatewayPluginClient{wrapper: cw})
	}
	return result
}

// Close shuts down all plugin connections.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for _, cw := range m.clients {
		if err := cw.conn.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *Manager) healthCheckLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		m.runHealthChecks()
	}
}

func (m *Manager) runHealthChecks() {
	m.mu.RLock()
	wrappers := make([]*clientWrapper, 0, len(m.clients))
	for _, cw := range m.clients {
		wrappers = append(wrappers, cw)
	}
	m.mu.RUnlock()

	for _, cw := range wrappers {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		cw.checkHealth(ctx)
		cancel()
	}
}

func (cw *clientWrapper) checkHealth(ctx context.Context) {
	state := cw.conn.GetState()
	if state != connectivity.Ready && state != connectivity.Idle {
		cw.health = plugin.HealthUnhealthy
		cw.logger.Info("grpc plugin connection not ready", "state", state.String())
		return
	}

	// Use the standard gRPC health checking protocol.
	hc := grpc_health_v1.NewHealthClient(cw.conn)
	resp, err := hc.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		cw.health = plugin.HealthUnhealthy
		cw.logger.Info("grpc plugin health check failed", "error", err)
		return
	}
	if resp.Status == grpc_health_v1.HealthCheckResponse_SERVING {
		if cw.health != plugin.HealthHealthy {
			cw.logger.Info("grpc plugin became healthy")
		}
		cw.health = plugin.HealthHealthy
	} else {
		cw.health = plugin.HealthUnhealthy
		cw.logger.Info("grpc plugin not serving", "status", resp.Status)
	}
}

// routerPluginClient wraps a gRPC connection to provide the plugin.Router interface.
type routerPluginClient struct {
	wrapper *clientWrapper
}

func (r *routerPluginClient) Name() string                { return r.wrapper.name }
func (r *routerPluginClient) Type() string                { return r.wrapper.pluginType }
func (r *routerPluginClient) Health() plugin.HealthStatus { return r.wrapper.health }
func (r *routerPluginClient) Close() error                { return r.wrapper.conn.Close() }

func (r *routerPluginClient) OrderCandidates(ctx context.Context, candidates []plugin.CandidateInfo, routeCtx plugin.RouteContext) ([]string, bool) {
	if r.wrapper.health != plugin.HealthHealthy {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(ctx, r.wrapper.timeout)
	defer cancel()

	cli := pluginv1.NewRouterPluginClient(r.wrapper.conn)
	req := buildOrderCandidatesRequest(candidates, routeCtx)

	resp, err := cli.OrderCandidates(ctx, req)
	if err != nil {
		r.wrapper.logger.Info("router plugin call failed, marking unhealthy", "error", err)
		r.wrapper.health = plugin.HealthUnhealthy
		return nil, false
	}
	if len(resp.OrderedNames) == 0 {
		return nil, false
	}
	return resp.OrderedNames, true
}

// gatewayPluginClient wraps a gRPC connection to provide the plugin.Gateway interface.
type gatewayPluginClient struct {
	wrapper *clientWrapper
}

func (g *gatewayPluginClient) Name() string                { return g.wrapper.name }
func (g *gatewayPluginClient) Type() string                { return g.wrapper.pluginType }
func (g *gatewayPluginClient) Health() plugin.HealthStatus { return g.wrapper.health }
func (g *gatewayPluginClient) Close() error                { return g.wrapper.conn.Close() }

func (g *gatewayPluginClient) Process(ctx context.Context, phase plugin.Phase, payload []byte, traceID, tenantID, userID, model string, stream bool) ([]plugin.Command, error) {
	if g.wrapper.health != plugin.HealthHealthy {
		return nil, fmt.Errorf("plugin unhealthy")
	}

	ctx, cancel := context.WithTimeout(ctx, g.wrapper.timeout)
	defer cancel()

	cli := pluginv1.NewGatewayPluginClient(g.wrapper.conn)
	streamCli, err := cli.Process(ctx)
	if err != nil {
		g.wrapper.logger.Info("gateway plugin process stream failed", "error", err)
		g.wrapper.health = plugin.HealthUnhealthy
		return nil, err
	}

	phaseKey := "PHASE_" + strings.ToUpper(string(phase))
	event := &pluginv1.Event{
		Phase: pluginv1.Phase(pluginv1.Phase_value[phaseKey]),
		Context: &pluginv1.Context{
			TraceId:  traceID,
			TenantId: tenantID,
			UserId:   userID,
			Model:    model,
			Stream:   stream,
		},
		Payload: payload,
	}

	if err := streamCli.Send(event); err != nil {
		g.wrapper.logger.Info("gateway plugin send failed", "error", err)
		return nil, err
	}

	// Close the send side so the plugin knows the event is complete.
	_ = streamCli.CloseSend()

	var commands []plugin.Command
	for {
		resp, err := streamCli.Recv()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			g.wrapper.logger.Info("gateway plugin recv failed", "error", err)
			return nil, err
		}
		actionStr := strings.TrimPrefix(resp.Action.String(), "ACTION_")
		if actionStr == "UNSPECIFIED" {
			actionStr = ""
		}
		commands = append(commands, plugin.Command{
			Action:  actionStr,
			Payload: resp.Payload,
			Reason:  resp.Reason,
		})
	}

	return commands, nil
}

func buildOrderCandidatesRequest(candidates []plugin.CandidateInfo, routeCtx plugin.RouteContext) *pluginv1.OrderCandidatesRequest {
	cands := make([]*pluginv1.Candidate, len(candidates))
	for i, c := range candidates {
		cands[i] = &pluginv1.Candidate{
			Name:        c.Name,
			Model:       c.Model,
			Weight:      int32(c.Weight),
			UnitCost:    c.UnitCost,
			CurrentLoad: c.Load,
			Tpm:         c.TPM,
			Healthy:     c.Healthy,
		}
	}
	return &pluginv1.OrderCandidatesRequest{
		Candidates: cands,
		Context: &pluginv1.RouteContext{
			Model:               routeCtx.Model,
			SessionId:           routeCtx.SessionID,
			InputText:           routeCtx.InputText,
			PromptTokens:        int32(routeCtx.PromptTokens),
			Stream:              routeCtx.Stream,
			HasTools:            routeCtx.HasTools,
			HasImages:           routeCtx.HasImages,
			HasStructuredOutput: routeCtx.HasStructuredOutput,
		},
	}
}
