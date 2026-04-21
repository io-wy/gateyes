package responses

import (
	"context"
	"encoding/json"
	"strings"
	"time"

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

	if s.router != nil {
		ordered, routerTrace := s.router.ExplainOrderCandidates(routable, buildRouteContext(req, sessionID))
		trace.Router = routerTrace
		trace.OrderedCandidates = providerNamesFromSlice(ordered)
		trace.touch()
		return ordered, trace
	}

	trace.OrderedCandidates = providerNamesFromSlice(routable)
	trace.touch()
	return routable, trace
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
