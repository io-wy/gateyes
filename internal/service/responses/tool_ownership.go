package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/service/provider"
)

type ToolOwner string

const (
	ToolOwnerGateway  ToolOwner = "gateway-owned"
	ToolOwnerClient   ToolOwner = "client-owned"
	ToolOwnerProvider ToolOwner = "provider-owned"
)

var (
	ErrInvalidToolOwnership    = errors.New("invalid tool ownership")
	ErrToolExecutorUnavailable = errors.New("gateway tool executor unavailable")
	ErrToolLoopLimit           = errors.New("gateway tool loop limit exceeded")
)

type ToolDecision struct {
	Name  string    `json:"name,omitempty"`
	Type  string    `json:"type,omitempty"`
	Owner ToolOwner `json:"owner"`
}

type ToolExecution struct {
	Name      string
	Type      string
	Arguments string
}

type ToolExecutor interface {
	Execute(context.Context, ToolExecution) (string, error)
}

func (s *Service) SetToolExecutor(executor ToolExecutor) { s.toolExecutor = executor }

func (s *Service) classifyTools(req *provider.ResponseRequest) ([]ToolDecision, error) {
	if req == nil || len(req.Tools) == 0 || s.cfg == nil || !s.cfg.ToolOwnership.Enabled {
		return nil, nil
	}
	cfg := s.cfg.ToolOwnership
	decisions := make([]ToolDecision, 0, len(req.Tools))
	for _, raw := range req.Tools {
		name, typ, ok := toolIdentity(raw)
		if !ok {
			return nil, fmt.Errorf("%w: tool definition has unknown shape", ErrInvalidToolOwnership)
		}
		matches := matchingToolRules(cfg.Rules, name, typ)
		if len(matches) == 0 && strings.TrimSpace(cfg.DefaultOwner) != "" {
			matches = []config.ToolOwnershipRule{{Owner: cfg.DefaultOwner}}
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf("%w: tool %q (%s) must match exactly one rule", ErrInvalidToolOwnership, name, typ)
		}
		owner := ToolOwner(strings.ToLower(strings.TrimSpace(matches[0].Owner)))
		if owner != ToolOwnerGateway && owner != ToolOwnerClient && owner != ToolOwnerProvider {
			return nil, fmt.Errorf("%w: unsupported owner %q", ErrInvalidToolOwnership, owner)
		}
		decisions = append(decisions, ToolDecision{Name: name, Type: typ, Owner: owner})
	}
	return decisions, nil
}

func matchingToolRules(rules []config.ToolOwnershipRule, name, typ string) []config.ToolOwnershipRule {
	var matches []config.ToolOwnershipRule
	for _, rule := range rules {
		if rule.Name != "" && !strings.EqualFold(strings.TrimSpace(rule.Name), name) {
			continue
		}
		if rule.Type != "" && !strings.EqualFold(strings.TrimSpace(rule.Type), typ) {
			continue
		}
		matches = append(matches, rule)
	}
	return matches
}

func toolIdentity(raw any) (name, typ string, ok bool) {
	value, ok := raw.(map[string]any)
	if !ok {
		body, err := json.Marshal(raw)
		if err != nil || json.Unmarshal(body, &value) != nil {
			return "", "", false
		}
	}
	typ, _ = value["type"].(string)
	name, _ = value["name"].(string)
	if function, exists := value["function"].(map[string]any); exists {
		if name == "" {
			name, _ = function["name"].(string)
		}
		if typ == "" {
			typ = "function"
		}
	}
	if strings.TrimSpace(typ) == "" && strings.TrimSpace(name) == "" {
		return "", "", false
	}
	return strings.TrimSpace(name), strings.TrimSpace(typ), true
}

func (s *Service) validateToolOwnership(req *provider.ResponseRequest) ([]ToolDecision, error) {
	decisions, err := s.classifyTools(req)
	if err != nil {
		return nil, err
	}
	for _, decision := range decisions {
		if decision.Owner == ToolOwnerGateway && s.toolExecutor == nil {
			return nil, fmt.Errorf("%w: %s", ErrToolExecutorUnavailable, decision.Name)
		}
	}
	return decisions, nil
}

func toolOwnerByName(decisions []ToolDecision) map[string]ToolOwner {
	owners := make(map[string]ToolOwner, len(decisions))
	for _, decision := range decisions {
		owners[decision.Name] = decision.Owner
	}
	return owners
}

func hasGatewayOwnedTool(decisions []ToolDecision) bool {
	for _, decision := range decisions {
		if decision.Owner == ToolOwnerGateway {
			return true
		}
	}
	return false
}

func (s *Service) runGatewayToolLoop(ctx context.Context, exec *execution, req *provider.ResponseRequest, initial *provider.Response, owners map[string]ToolOwner, trace *routeTrace) (*provider.Response, error) {
	if exec == nil || exec.provider == nil || req == nil || initial == nil {
		return initial, nil
	}
	maxRounds := 4
	if s.cfg != nil && s.cfg.ToolOwnership.MaxLoopRounds > 0 {
		maxRounds = s.cfg.ToolOwnership.MaxLoopRounds
	}
	resp := initial
	aggregateUsage := initial.Usage
	continuedReq := *req
	for round := 0; ; round++ {
		calls := resp.OutputToolCalls()
		gatewayCalls := make([]provider.ToolCall, 0, len(calls))
		for _, call := range calls {
			owner, exists := owners[call.Function.Name]
			if !exists {
				return nil, fmt.Errorf("%w: upstream returned unowned tool %q", ErrInvalidToolOwnership, call.Function.Name)
			}
			if owner == ToolOwnerGateway {
				gatewayCalls = append(gatewayCalls, call)
			}
		}
		if len(gatewayCalls) == 0 {
			recordExternalToolCalls(trace, calls, owners)
			resp.Usage = aggregateUsage
			return resp, nil
		}
		if len(gatewayCalls) != len(calls) {
			return nil, fmt.Errorf("%w: upstream turn mixes gateway-owned and external tool calls", ErrInvalidToolOwnership)
		}
		if round >= maxRounds {
			return nil, fmt.Errorf("%w: max rounds %d", ErrToolLoopLimit, maxRounds)
		}

		messages := append(continuedReq.InputMessages(), toolLoopResponseMessages(resp)...)
		for _, call := range gatewayCalls {
			entry := routeTraceToolCall{CallID: call.ID, Name: call.Function.Name, Owner: ToolOwnerGateway}
			output, err := s.toolExecutor.Execute(ctx, ToolExecution{Name: call.Function.Name, Type: call.Type, Arguments: call.Function.Arguments})
			if err != nil {
				entry.Outcome = "error"
				entry.Error = err.Error()
				if trace != nil {
					trace.ToolCalls = append(trace.ToolCalls, entry)
				}
				return nil, fmt.Errorf("execute gateway tool %q: %w", call.Function.Name, err)
			}
			entry.Outcome = "success"
			if trace != nil {
				trace.ToolCalls = append(trace.ToolCalls, entry)
			}
			messages = append(messages, provider.Message{Role: "tool", Type: "function_call_output", ToolCallID: call.ID, Content: provider.TextBlocks(output)})
		}
		continuedReq.Messages, continuedReq.Input = messages, messages
		exec.upstreamRequest = buildUpstreamRequest(&continuedReq)
		next, retries, err := s.callWithRetry(ctx, nil, exec)
		exec.continuationRetries += retries
		if err != nil {
			return nil, err
		}
		resp = s.normalizeResponse(exec, next)
		aggregateUsage.PromptTokens += resp.Usage.PromptTokens
		aggregateUsage.CompletionTokens += resp.Usage.CompletionTokens
		aggregateUsage.TotalTokens += resp.Usage.TotalTokens
		aggregateUsage.CachedTokens += resp.Usage.CachedTokens
	}
}

func toolLoopResponseMessages(resp *provider.Response) []provider.Message {
	if resp == nil {
		return nil
	}
	message := provider.Message{Role: "assistant", Type: "message"}
	for _, output := range resp.Output {
		if output.Type == "message" {
			for _, content := range output.Content {
				if content.Text != "" {
					message.Content = append(message.Content, provider.ContentBlock{Type: "text", Text: content.Text})
				}
			}
		}
		if output.Type == "function_call" {
			callID := output.ID
			if callID == "" {
				callID = output.CallID
			}
			message.ToolCalls = append(message.ToolCalls, provider.ToolCall{ID: callID, Type: "function", Function: provider.FunctionCall{Name: output.Name, Arguments: output.Args}})
		}
	}
	if len(message.Content) == 0 && len(message.ToolCalls) == 0 {
		return nil
	}
	return []provider.Message{message}
}

func recordExternalToolCalls(trace *routeTrace, calls []provider.ToolCall, owners map[string]ToolOwner) {
	if trace == nil {
		return
	}
	for _, call := range calls {
		trace.ToolCalls = append(trace.ToolCalls, routeTraceToolCall{CallID: call.ID, Name: call.Function.Name, Owner: owners[call.Function.Name], Outcome: "returned"})
	}
}
