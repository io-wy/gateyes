package plugin

import (
	"context"
	"time"
)

// HealthStatus describes the health of a gRPC plugin.
type HealthStatus int

const (
	HealthUnknown HealthStatus = iota
	HealthHealthy
	HealthUnhealthy
)

// Phase defines the lifecycle points at which gateway plugins are invoked.
type Phase string

const (
	// PreRoute is fired before the gateway routes the request to providers.
	PreRoute Phase = "pre_route"
	// PostRoute is fired after the gateway has selected/ordered providers.
	PostRoute Phase = "post_route"
	// PreUpstream is fired before the request is sent to the upstream provider.
	PreUpstream Phase = "pre_upstream"
	// PostUpstream is fired after the upstream response has been received.
	PostUpstream Phase = "post_upstream"
	// Audit is fired after the response has been fully written to the client.
	Audit Phase = "audit"
)

// RouteContext holds all information a router plugin needs to decide ordering.
type RouteContext struct {
	Model               string
	SessionID           string
	InputText           string
	PromptTokens        int
	Stream              bool
	HasTools            bool
	HasImages           bool
	HasStructuredOutput bool
}

// CandidateInfo holds the data passed to a router plugin for each provider.
type CandidateInfo struct {
	Name     string
	Model    string
	Weight   int
	UnitCost float64
	Load     int64
	TPM      int64
	Healthy  bool
}

// Client is the common interface for all gRPC plugin clients.
type Client interface {
	// Name returns the plugin name configured in the gateway.
	Name() string
	// Type returns the plugin type, e.g. "router", "gateway".
	Type() string
	// Health returns the current health status of the plugin.
	Health() HealthStatus
	// Close releases the underlying gRPC connection.
	Close() error
}

// Router is the interface for router plugins.
type Router interface {
	Client
	// OrderCandidates asks the plugin to order providers.
	// If the plugin is unhealthy or the call fails, it returns (nil, false).
	OrderCandidates(ctx context.Context, candidates []CandidateInfo, routeCtx RouteContext) ([]string, bool)
}

// Gateway is the interface for universal gateway plugins (cache, audit, transform, etc.).
type Gateway interface {
	Client
	// Process sends an event to the plugin and receives commands.
	// If the plugin is unhealthy or the call fails, it returns (nil, error).
	Process(ctx context.Context, phase Phase, payload []byte, traceID, tenantID, userID, model string, stream bool) ([]Command, error)
}

// Command is an instruction from a plugin to the gateway.
type Command struct {
	// Action is the instruction type.
	Action string
	// Payload is action-specific data (JSON).
	Payload []byte
	// Reason is human-readable (required for block).
	Reason string
}

// Manager manages all gRPC plugin connections and provides lookup.
type Manager interface {
	// Router returns the first healthy router plugin, or nil if none.
	Router() Router
	// GetByPhase returns all healthy gateway plugins for the given phase.
	GetByPhase(phase Phase) []Gateway
	// Close shuts down all plugin connections.
	Close() error
}

// DefaultTimeout is the default gRPC call timeout for plugin RPCs.
const DefaultTimeout = 100 * time.Millisecond
