package responses

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gateyes/gateway/internal/platform/trace"
	pluginSvc "github.com/gateyes/gateway/internal/plugin"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
	routeSvc "github.com/gateyes/gateway/internal/service/router"
)

type routeTrace struct {
	ResponseID        string               `json:"response_id,omitempty"`
	TenantID          string               `json:"tenant_id,omitempty"`
	ProjectID         string               `json:"project_id,omitempty"`
	RequestedModel    string               `json:"requested_model,omitempty"`
	SessionID         string               `json:"session_id,omitempty"`
	InitialCandidates []string             `json:"initial_candidates,omitempty"`
	FilteredOut       []routeTraceFiltered `json:"filtered_out,omitempty"`
	Router            routeSvc.OrderTrace  `json:"router"`
	OrderedCandidates []string             `json:"ordered_candidates,omitempty"`
	Attempts          []routeTraceAttempt  `json:"attempts,omitempty"`
	FinalProvider     string               `json:"final_provider,omitempty"`
	Status            string               `json:"status,omitempty"`
	Error             string               `json:"error,omitempty"`
	UpdatedAt         string               `json:"updated_at,omitempty"`
}

type routeTraceFiltered struct {
	Provider string `json:"provider"`
	Reason   string `json:"reason"`
	Detail   string `json:"detail,omitempty"`
}

type routeTraceAttempt struct {
	Provider string `json:"provider"`
	Retries  int    `json:"retries"`
	Result   string `json:"result"`
	Error    string `json:"error,omitempty"`
}

func (s *Service) planCandidates(ctx context.Context, identity *repository.AuthIdentity, sessionID string, req *provider.ResponseRequest) ([]provider.Provider, *routeTrace) {
	traceID := "unknown"
	if parentSpan, ok := trace.SpanFromContext(ctx); ok {
		traceID = parentSpan.TraceID
	}
	ctx = trace.StartSpan(ctx, traceID, "plan_candidates")
	defer trace.FinishSpan(ctx, map[string]string{
		"model": req.Model,
	})

	trace := &routeTrace{
		TenantID:       identity.TenantID,
		ProjectID:      identity.ProjectID,
		RequestedModel: req.Model,
		SessionID:      sessionID,
		Status:         "planned",
	}

	providerNames, err := s.store.ListTenantProviders(ctx, identity.TenantID)
	if err != nil {
		trace.Status = "error"
		trace.Error = err.Error()
		trace.touch()
		return nil, trace
	}
	trace.InitialCandidates = append([]string(nil), providerNames...)

	rawCandidates := s.providerMgr.ListByNames(providerNames)
	routable := make([]provider.Provider, 0, len(providerNames))
	for _, name := range providerNames {
		if req != nil && strings.TrimSpace(req.PreferredProvider) != "" && name != req.PreferredProvider {
			trace.FilteredOut = append(trace.FilteredOut, routeTraceFiltered{
				Provider: name,
				Reason:   "preferred_provider",
				Detail:   req.PreferredProvider,
			})
			continue
		}
		instance, ok := s.providerMgr.Get(name)
		if !ok {
			trace.FilteredOut = append(trace.FilteredOut, routeTraceFiltered{
				Provider: name,
				Reason:   "provider_missing",
			})
			continue
		}
		if s.auth != nil && !s.auth.CheckProvider(identity, name) {
			trace.FilteredOut = append(trace.FilteredOut, routeTraceFiltered{
				Provider: name,
				Reason:   "key_provider_scope",
			})
			continue
		}
		if record, ok := s.providerMgr.Registry(name); ok {
			if reason, detail := registryFilterReason(record, req); reason != "" {
				trace.FilteredOut = append(trace.FilteredOut, routeTraceFiltered{
					Provider: name,
					Reason:   reason,
					Detail:   detail,
				})
				continue
			}
		}
		routable = append(routable, instance)
	}

	if modelRequiredButUnavailable(req, rawCandidates, routable) {
		trace.Status = "no_provider"
		trace.Error = "exact_model_unavailable"
		trace.touch()
		return nil, trace
	}
	if len(routable) == 0 {
		trace.Status = "no_provider"
		trace.Error = "all_candidates_filtered"
		trace.touch()
		return nil, trace
	}

	ordered := routable
	if s.router != nil {
		var routerTrace routeSvc.OrderTrace
		ordered, routerTrace = s.router.ExplainOrderCandidates(routable, buildRouteContext(req, sessionID))
		trace.Router = routerTrace
	}

	// Try gRPC router plugin (outside lock — it's IO).
	if s.pluginMgr != nil {
		if pr := s.pluginMgr.Router(); pr != nil {
			pluginOrdered := s.tryPluginRouter(ctx, pr, ordered, buildRouteContext(req, sessionID))
			if pluginOrdered != nil {
				ordered = pluginOrdered
				trace.Router.Strategy = "plugin:" + pr.Name()
			}
		}
	}

	trace.OrderedCandidates = providerNamesFromSlice(ordered)
	trace.touch()
	return ordered, trace
}

func (s *Service) tryPluginRouter(ctx context.Context, pr pluginSvc.Router, candidates []provider.Provider, routeCtx routeSvc.RouteContext) []provider.Provider {
	if len(candidates) == 0 {
		return nil
	}

	candInfos := make([]pluginSvc.CandidateInfo, len(candidates))
	for i, p := range candidates {
		candInfos[i] = pluginSvc.CandidateInfo{
			Name:     p.Name(),
			Model:    p.Model(),
			Weight:   p.Weight(),
			UnitCost: p.UnitCost(),
		}
		if s.providerMgr != nil {
			if stats := s.providerMgr.Stats; stats != nil {
				candInfos[i].Load = stats.CurrentLoad(p.Name())
				candInfos[i].TPM = stats.TPM(p.Name())
			}
		}
		if record, ok := s.providerMgr.Registry(p.Name()); ok {
			candInfos[i].Healthy = record.HealthStatus == provider.ProviderHealthHealthy
		}
	}

	pluginCtx := pluginSvc.RouteContext{
		Model:               routeCtx.Model,
		SessionID:           routeCtx.SessionID,
		InputText:           routeCtx.InputText,
		PromptTokens:        routeCtx.PromptTokens,
		Stream:              routeCtx.Stream,
		HasTools:            routeCtx.HasTools,
		HasImages:           routeCtx.HasImages,
		HasStructuredOutput: routeCtx.HasStructuredOutput,
	}

	names, ok := pr.OrderCandidates(ctx, candInfos, pluginCtx)
	if !ok || len(names) == 0 {
		return nil
	}

	// Map ordered names back to provider instances.
	byName := make(map[string]provider.Provider, len(candidates))
	for _, p := range candidates {
		byName[p.Name()] = p
	}
	ordered := make([]provider.Provider, 0, len(names))
	for _, name := range names {
		if p, exists := byName[name]; exists {
			ordered = append(ordered, p)
		}
	}
	// Append any candidates the plugin didn't mention, preserving their relative order.
	for _, p := range candidates {
		if _, exists := byName[p.Name()]; !exists {
			continue
		}
		found := false
		for _, name := range names {
			if name == p.Name() {
				found = true
				break
			}
		}
		if !found {
			ordered = append(ordered, p)
		}
	}
	if len(ordered) == 0 {
		return nil
	}
	return ordered
}

func registryFilterReason(record repository.ProviderRegistryRecord, req *provider.ResponseRequest) (string, string) {
	if !record.Enabled {
		return "provider_disabled", ""
	}
	if record.Drain {
		return "provider_drain", ""
	}
	switch strings.ToLower(strings.TrimSpace(record.HealthStatus)) {
	case "", provider.ProviderHealthHealthy, provider.ProviderHealthDegraded:
	default:
		return "provider_unhealthy", record.HealthStatus
	}
	if req == nil {
		return "", ""
	}
	switch strings.ToLower(strings.TrimSpace(req.Surface)) {
	case "chat":
		if !record.SupportsChat {
			return "capability_surface", "chat"
		}
	case "responses":
		if !record.SupportsResponses {
			return "capability_surface", "responses"
		}
	case "messages":
		if !record.SupportsMessages {
			return "capability_surface", "messages"
		}
	}
	if req.Stream && !record.SupportsStream {
		return "capability_stream", ""
	}
	if req.HasToolsRequested() && !record.SupportsTools {
		return "capability_tools", ""
	}
	if req.HasImageInput() && !record.SupportsImages {
		return "capability_images", ""
	}
	if req.HasStructuredOutputRequest() && !record.SupportsStructuredOutput {
		return "capability_structured_output", ""
	}
	return "", ""
}

func routeTraceBytes(trace *routeTrace) []byte {
	if trace == nil {
		return nil
	}
	trace.touch()
	raw, err := json.Marshal(trace)
	if err != nil {
		return nil
	}
	return raw
}

func appendRouteAttempt(trace *routeTrace, providerName string, retries int, result string, err error) {
	if trace == nil {
		return
	}
	attempt := routeTraceAttempt{
		Provider: providerName,
		Retries:  retries,
		Result:   result,
	}
	if err != nil {
		attempt.Error = err.Error()
	}
	trace.Attempts = append(trace.Attempts, attempt)
	trace.touch()
}

func finalizeRouteTrace(trace *routeTrace, providerName, status string, err error) {
	if trace == nil {
		return
	}
	trace.FinalProvider = providerName
	trace.Status = status
	if err != nil {
		trace.Error = err.Error()
	}
	trace.touch()
}

func (t *routeTrace) touch() {
	if t == nil {
		return
	}
	t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
}

func providerNamesFromSlice(items []provider.Provider) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name())
	}
	return names
}
