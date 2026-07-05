package catalog

import (
	"context"
	"fmt"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/alert"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/budget"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

// ---- Runtime operations ----

func (s *Service) Create(ctx context.Context, identity *repository.AuthIdentity, prefix, surface string, req *provider.ResponseRequest, sessionID string) (*responseSvc.CreateResult, *repository.ServiceRecord, error) {
	runtime, preparedReq, err := s.prepareRuntimeRequest(ctx, identity, prefix, surface, req)
	if err != nil {
		return nil, nil, err
	}
	// Preserve original raw request body from handler context
	if raw := responseSvc.RawBodyFromContext(ctx); len(raw) > 0 {
		ctx = responseSvc.WithRawRequestBody(ctx, raw)
	}
	result, err := s.responses.Create(ctx, identity, preparedReq, sessionID)
	if err != nil {
		return nil, runtime.service, err
	}
	if err := s.applyResponsePolicy(ctx, identity, runtime, result.Response); err != nil {
		return nil, runtime.service, err
	}
	return result, runtime.service, nil
}

func (s *Service) CreateStream(ctx context.Context, identity *repository.AuthIdentity, prefix, surface string, req *provider.ResponseRequest, sessionID string) (*responseSvc.Stream, *repository.ServiceRecord, error) {
	runtime, preparedReq, err := s.prepareRuntimeRequest(ctx, identity, prefix, surface, req)
	if err != nil {
		return nil, nil, err
	}
	stream, err := s.responses.CreateStream(ctx, identity, preparedReq, sessionID)
	if err != nil {
		return nil, runtime.service, err
	}
	if runtime.snapshot.Config.Policy == nil || !runtime.snapshot.Config.Policy.Enabled || runtime.snapshot.Config.Policy.Response == nil {
		return stream, runtime.service, nil
	}

	events := make(chan provider.ResponseEvent)
	errCh := make(chan error, 1)
	go s.filterResponseStream(ctx, runtime, stream, events, errCh)
	return &responseSvc.Stream{
		ResponseID:   stream.ResponseID,
		ProviderName: stream.ProviderName,
		StartedAt:    stream.StartedAt,
		Events:       events,
		Errors:       errCh,
	}, runtime.service, nil
}

func (s *Service) CreatePromptInvocation(ctx context.Context, identity *repository.AuthIdentity, prefix string, req PromptInvokeRequest, sessionID string) (*responseSvc.CreateResult, *repository.ServiceRecord, error) {
	runtime, prepared, err := s.preparePromptRequest(ctx, identity, prefix, req)
	if err != nil {
		return nil, nil, err
	}
	result, err := s.responses.Create(ctx, identity, prepared, sessionID)
	if err != nil {
		return nil, runtime.service, err
	}
	if err := s.applyResponsePolicy(ctx, identity, runtime, result.Response); err != nil {
		return nil, runtime.service, err
	}
	return result, runtime.service, nil
}

func (s *Service) CreatePromptInvocationStream(ctx context.Context, identity *repository.AuthIdentity, prefix string, req PromptInvokeRequest, sessionID string) (*responseSvc.Stream, *repository.ServiceRecord, error) {
	runtime, prepared, err := s.preparePromptRequest(ctx, identity, prefix, req)
	if err != nil {
		return nil, nil, err
	}
	stream, err := s.responses.CreateStream(ctx, identity, prepared, sessionID)
	if err != nil {
		return nil, runtime.service, err
	}
	if runtime.snapshot.Config.Policy == nil || !runtime.snapshot.Config.Policy.Enabled || runtime.snapshot.Config.Policy.Response == nil {
		return stream, runtime.service, nil
	}

	events := make(chan provider.ResponseEvent)
	errCh := make(chan error, 1)
	go s.filterResponseStream(ctx, runtime, stream, events, errCh)
	return &responseSvc.Stream{
		ResponseID:   stream.ResponseID,
		ProviderName: stream.ProviderName,
		StartedAt:    stream.StartedAt,
		Events:       events,
		Errors:       errCh,
	}, runtime.service, nil
}

// ---- Request preparation ----

func (s *Service) prepareRuntimeRequest(ctx context.Context, identity *repository.AuthIdentity, prefix, surface string, req *provider.ResponseRequest) (*serviceRuntime, *provider.ResponseRequest, error) {
	runtime, err := s.loadPublishedService(ctx, identity.TenantID, prefix)
	if err != nil {
		return nil, nil, err
	}
	if !containsString(runtime.snapshot.Config.Surfaces, surface) {
		return nil, nil, ErrServiceSurfaceDenied
	}
	if s.auth != nil && !s.auth.CheckService(identity, runtime.snapshot.RequestPrefix) {
		return nil, nil, ErrServiceAccessDenied
	}
	if err := s.checkSubscriptionSurface(ctx, identity, runtime.service.ID, surface); err != nil {
		return nil, nil, err
	}

	prepared := cloneResponseRequest(req)
	if prepared == nil {
		prepared = &provider.ResponseRequest{}
	}
	if runtime.snapshot.DefaultModel != "" {
		if prepared.Model != "" && prepared.Model != runtime.snapshot.DefaultModel {
			return nil, nil, fmt.Errorf("%w: model override is not allowed", ErrServiceAccessDenied)
		}
		prepared.Model = runtime.snapshot.DefaultModel
	}
	if prepared.Model == "" {
		return nil, nil, fmt.Errorf("%w: default model is required", ErrServiceNotPublished)
	}
	prepared.PreferredProvider = runtime.snapshot.DefaultProvider
	if runtime.snapshot.Config.PromptTemplate != nil && runtime.snapshot.Config.PromptTemplate.SystemTemplate != "" {
		if prepared.Options == nil {
			prepared.Options = &provider.RequestOptions{}
		}
		prepared.Options.System = runtime.snapshot.Config.PromptTemplate.SystemTemplate
	}
	prepared.Normalize()

	if err := s.applyRequestPolicy(runtime.snapshot.Config.Policy, prepared); err != nil {
		return nil, nil, err
	}
	if err := s.precheck(ctx, identity, runtime.snapshot.RequestPrefix, prepared); err != nil {
		return nil, nil, err
	}
	return runtime, prepared, nil
}

func (s *Service) preparePromptRequest(ctx context.Context, identity *repository.AuthIdentity, prefix string, req PromptInvokeRequest) (*serviceRuntime, *provider.ResponseRequest, error) {
	runtime, err := s.loadPublishedService(ctx, identity.TenantID, prefix)
	if err != nil {
		return nil, nil, err
	}
	if !containsString(runtime.snapshot.Config.Surfaces, "invoke") {
		return nil, nil, ErrServiceSurfaceDenied
	}
	if s.auth != nil && !s.auth.CheckService(identity, runtime.snapshot.RequestPrefix) {
		return nil, nil, ErrServiceAccessDenied
	}
	if err := s.checkSubscriptionSurface(ctx, identity, runtime.service.ID, "invoke"); err != nil {
		return nil, nil, err
	}
	template := runtime.snapshot.Config.PromptTemplate
	if template == nil || template.UserTemplate == "" {
		return nil, nil, ErrPromptTemplateMissing
	}
	renderedSystem, err := renderTemplate(template.SystemTemplate, template.Variables, req.Variables)
	if err != nil {
		return nil, nil, err
	}
	renderedUser, err := renderTemplate(template.UserTemplate, template.Variables, req.Variables)
	if err != nil {
		return nil, nil, err
	}
	prepared := &provider.ResponseRequest{
		Model:             runtime.snapshot.DefaultModel,
		PreferredProvider: runtime.snapshot.DefaultProvider,
		Messages: []provider.Message{{
			Role:    "user",
			Content: provider.TextBlocks(renderedUser),
		}},
		Stream:          req.Stream,
		MaxOutputTokens: req.MaxOutputTokens,
		MaxTokens:       req.MaxTokens,
		Tools:           req.Tools,
		Options: &provider.RequestOptions{
			System: renderedSystem,
		},
	}
	prepared.Normalize()

	if err := s.applyRequestPolicy(runtime.snapshot.Config.Policy, prepared); err != nil {
		return nil, nil, err
	}
	if err := s.precheck(ctx, identity, runtime.snapshot.RequestPrefix, prepared); err != nil {
		return nil, nil, err
	}
	return runtime, prepared, nil
}

func (s *Service) precheck(ctx context.Context, identity *repository.AuthIdentity, prefix string, req *provider.ResponseRequest) error {
	if s.auth != nil {
		if !s.auth.CheckService(identity, prefix) {
			return ErrServiceAccessDenied
		}
		if !s.auth.CheckModel(identity, req.Model) {
			return auth.ErrModelNotAllowed
		}
		if !s.auth.HasQuota(identity, req.EstimateAdmissionTokens()) {
			return auth.ErrQuotaExceeded
		}
	}
	if s.budgetSvc != nil {
		budgetResult, err := s.budgetSvc.Check(ctx, budget.CheckRequest{
			Identity:      identity,
			EstimatedCost: 0,
			ProviderName:  "",
			Model:         req.Model,
		})
		if err != nil {
			return fmt.Errorf("budget check: %w", err)
		}
		if !budgetResult.Allowed {
			return budgetResult.RejectError
		}
		if budgetResult.AlertSent && s.alertSvc != nil {
			scope := firstSoftAlertScope(budgetResult.Scopes)
			s.alertSvc.NotifyBudgetExhausted(ctx, alert.BudgetExhausted{
				TenantID:    identity.TenantID,
				ProjectID:   identity.ProjectID,
				APIKeyID:    identity.APIKeyID,
				Model:       req.Model,
				BudgetScope: scope,
			})
		}
	}
	if s.limiter != nil && s.auth != nil {
		if !s.limiter.Allow(ctx, identity.APIKey, s.auth.EffectiveRateLimitQPS(identity), req.EstimateAdmissionTokens()) {
			return ErrRateLimited
		}
		if !s.limiter.CheckTenant(identity.TenantID, req.EstimateAdmissionTokens()) {
			return ErrRateLimited
		}
		if !s.limiter.CheckModel(req.Model, req.EstimateAdmissionTokens()) {
			return ErrRateLimited
		}
	}
	return nil
}

// ---- Service loading ----

func (s *Service) loadPublishedService(ctx context.Context, tenantID, prefix string) (*serviceRuntime, error) {
	serviceRecord, err := s.store.GetServiceByPrefix(ctx, tenantID, prefix)
	if err != nil {
		return nil, err
	}
	if !serviceRecord.Enabled {
		return nil, ErrServiceDisabled
	}
	if serviceRecord.PublishedVersionID == "" {
		return nil, ErrServiceNotPublished
	}
	version, err := s.store.GetServiceVersion(ctx, serviceRecord.TenantID, serviceRecord.ID, serviceRecord.PublishedVersionID)
	if err != nil {
		return nil, err
	}
	snapshot := version.Snapshot
	policy, err := s.resolveEffectivePolicy(ctx, serviceRecord, snapshot.Config.Policy)
	if err != nil {
		return nil, err
	}
	snapshot.Config.Policy = policy
	return &serviceRuntime{
		service:  serviceRecord,
		version:  version,
		snapshot: snapshot,
	}, nil
}

func (s *Service) resolveEffectivePolicy(ctx context.Context, serviceRecord *repository.ServiceRecord, servicePolicy *repository.ServicePolicyConfig) (*repository.ServicePolicyConfig, error) {
	var effective *repository.ServicePolicyConfig
	if serviceRecord == nil {
		return cloneServicePolicy(servicePolicy), nil
	}
	if serviceRecord.TenantID != "" {
		tenant, err := s.store.GetTenant(ctx, serviceRecord.TenantID)
		if err != nil {
			return nil, err
		}
		effective = mergeServicePolicies(effective, tenant.Policy)
	}
	if serviceRecord.ProjectID != "" {
		project, err := s.store.GetProject(ctx, serviceRecord.TenantID, serviceRecord.ProjectID)
		if err != nil {
			return nil, err
		}
		effective = mergeServicePolicies(effective, project.Policy)
	}
	effective = mergeServicePolicies(effective, servicePolicy)
	return effective, nil
}

func (s *Service) checkSubscriptionSurface(ctx context.Context, identity *repository.AuthIdentity, serviceID, surface string) error {
	if identity == nil || identity.APIKeyID == "" {
		return nil
	}
	subscriptions, err := s.store.ListServiceSubscriptions(ctx, identity.TenantID, repository.ServiceSubscriptionFilter{
		ServiceID: serviceID,
		Status:    "approved",
	})
	if err != nil {
		return err
	}
	for _, item := range subscriptions {
		if item.ApprovedAPIKeyID != identity.APIKeyID {
			continue
		}
		if len(item.AllowedSurfaces) == 0 || containsString(item.AllowedSurfaces, surface) {
			return nil
		}
		return ErrServiceSurfaceDenied
	}
	return nil
}
