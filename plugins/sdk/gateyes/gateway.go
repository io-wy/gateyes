package gateyes

import "encoding/json"

// GatewayEvent is the JSON payload a host sends to a WASM gateway plugin.
type GatewayEvent struct {
	Phase   string         `json:"phase"`
	Context GatewayContext `json:"context"`
	Payload json.RawMessage `json:"payload"`
}

// GatewayContext carries request metadata.
type GatewayContext struct {
	TraceID  string `json:"trace_id"`
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
}

// GatewayCommand is the JSON payload a WASM gateway plugin returns.
type GatewayCommand struct {
	Action  string `json:"action"`
	Payload []byte `json:"payload,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// GatewayAction constants for command actions.
const (
	ActionAllow    = "ALLOW"
	ActionBlock    = "BLOCK"
	ActionTransform = "TRANSFORM"
	ActionCacheHit = "CACHE_HIT"
	ActionSkip     = "SKIP"
)

// AllowGateway returns a command that allows the event through.
func AllowGateway() GatewayCommand {
	return GatewayCommand{Action: ActionAllow}
}

// BlockGateway returns a command that blocks the event.
func BlockGateway(reason string) GatewayCommand {
	return GatewayCommand{Action: ActionBlock, Reason: reason}
}

// TransformGateway returns a command that transforms the payload.
func TransformGateway(payload []byte) GatewayCommand {
	return GatewayCommand{Action: ActionTransform, Payload: payload}
}

// CacheHitGateway returns a command that signals a cache hit.
func CacheHitGateway(payload []byte) GatewayCommand {
	return GatewayCommand{Action: ActionCacheHit, Payload: payload}
}

// SkipGateway returns a command that skips remaining plugins for this phase.
func SkipGateway() GatewayCommand {
	return GatewayCommand{Action: ActionSkip}
}
