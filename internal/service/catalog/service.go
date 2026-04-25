package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/alert"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/budget"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

var (
	ErrServiceNotPublished   = errors.New("service not published")
	ErrServiceDisabled       = errors.New("service disabled")
	ErrServiceSurfaceDenied  = errors.New("service surface not allowed")
	ErrServiceAccessDenied   = errors.New("service access denied")
	ErrPromptTemplateMissing = errors.New("prompt template not configured")
	ErrPromptVariableMissing = errors.New("prompt variable missing")
	ErrPolicyViolation       = errors.New("policy violation")
	ErrRateLimited           = errors.New("rate limit exceeded")
)

type Dependencies struct {
	Store     repository.Store
	Auth      *auth.Auth
	Limiter   *limiter.Limiter
	BudgetSvc *budget.Service
	AlertSvc  *alert.AlertService
	Responses *responseSvc.Service
}

type Service struct {
	store     repository.Store
	auth      *auth.Auth
	limiter   *limiter.Limiter
	budgetSvc *budget.Service
	alertSvc  *alert.AlertService
	responses *responseSvc.Service
}

type CreateServiceResult struct {
	Service        *repository.ServiceRecord
	InitialVersion *repository.ServiceVersionRecord
}

type PromptInvokeRequest struct {
	Variables       map[string]any `json:"variables"`
	Stream          bool           `json:"stream,omitempty"`
	MaxOutputTokens int            `json:"max_output_tokens,omitempty"`
	MaxTokens       int            `json:"max_tokens,omitempty"`
	Tools           []any          `json:"tools,omitempty"`
}

type ReviewSubscriptionResult struct {
	Subscription *repository.ServiceSubscriptionRecord
	APIKey       *repository.APIKeyRecord
	APISecret    string
}

type serviceRuntime struct {
	service  *repository.ServiceRecord
	version  *repository.ServiceVersionRecord
	snapshot repository.ServiceSnapshot
}

func New(deps *Dependencies) *Service {
	return &Service{
		store:     deps.Store,
		auth:      deps.Auth,
		limiter:   deps.Limiter,
		budgetSvc: deps.BudgetSvc,
		alertSvc:  deps.AlertSvc,
		responses: deps.Responses,
	}
}

func (s *Service) CreateService(ctx context.Context, params repository.CreateServiceParams) (*CreateServiceResult, error) {
	record, err := s.store.CreateService(ctx, params)
	if err != nil {
		return nil, err
	}
	version, err := s.store.CreateServiceVersion(ctx, record.TenantID, repository.CreateServiceVersionParams{
		ServiceID: record.ID,
		Snapshot:  snapshotFromService(*record),
	})
	if err != nil {
		return nil, err
	}
	return &CreateServiceResult{Service: record, InitialVersion: version}, nil
}

func (s *Service) CreateServiceVersion(ctx context.Context, tenantID, serviceID string) (*repository.ServiceVersionRecord, error) {
	record, err := s.store.GetService(ctx, tenantID, serviceID)
	if err != nil {
		return nil, err
	}
	return s.store.CreateServiceVersion(ctx, tenantID, repository.CreateServiceVersionParams{
		ServiceID: record.ID,
		Snapshot:  snapshotFromService(*record),
	})
}

func (s *Service) PublishServiceVersion(ctx context.Context, tenantID, serviceID, versionID, mode string) (*repository.ServiceRecord, *repository.ServiceVersionRecord, error) {
	return s.store.PublishServiceVersion(ctx, tenantID, serviceID, repository.PublishServiceVersionParams{
		VersionID: versionID,
		Mode:      mode,
	})
}

func (s *Service) PromoteStagedServiceVersion(ctx context.Context, tenantID, serviceID string) (*repository.ServiceRecord, *repository.ServiceVersionRecord, error) {
	return s.store.PromoteStagedServiceVersion(ctx, tenantID, serviceID)
}

func (s *Service) RollbackServiceVersion(ctx context.Context, tenantID, serviceID, versionID string) (*repository.ServiceRecord, *repository.ServiceVersionRecord, error) {
	return s.store.RollbackServiceVersion(ctx, tenantID, serviceID, repository.RollbackServiceVersionParams{
		VersionID: versionID,
	})
}

func (s *Service) ReviewSubscription(ctx context.Context, tenantID, subscriptionID, decision, reviewNote string) (*ReviewSubscriptionResult, error) {
	subscription, err := s.store.GetServiceSubscription(ctx, tenantID, subscriptionID)
	if err != nil {
		return nil, err
	}
	if decision != "approve" && decision != "reject" {
		return nil, fmt.Errorf("unsupported review decision: %s", decision)
	}
	if decision == "reject" {
		status := "rejected"
		review := reviewNote
		updated, err := s.store.UpdateServiceSubscription(ctx, tenantID, subscription.ID, repository.UpdateServiceSubscriptionParams{
			Status:     &status,
			ReviewNote: &review,
		})
		if err != nil {
			return nil, err
		}
		return &ReviewSubscriptionResult{Subscription: updated}, nil
	}

	serviceRecord, err := s.store.GetService(ctx, tenantID, subscription.ServiceID)
	if err != nil {
		return nil, err
	}
	runtime, err := s.loadPublishedService(ctx, tenantID, serviceRecord.RequestPrefix)
	if err != nil {
		return nil, err
	}

	userID := subscription.ConsumerUserID
	if userID == "" {
		user, err := s.store.CreateUser(ctx, repository.CreateUserParams{
			TenantID:   tenantID,
			ProjectID:  firstNonEmpty(subscription.ProjectID, serviceRecord.ProjectID),
			Name:       subscription.ConsumerName,
			Email:      subscription.ConsumerEmail,
			Role:       repository.RoleTenantUser,
			Quota:      -1,
			QPS:        0,
			Status:     repository.StatusActive,
			APIKey:     generatePlaceholderKey(subscription.ID),
			SecretHash: repository.HashSecret(subscription.ID),
		})
		if err != nil {
			return nil, err
		}
		userID = user.ID
	}

	apiKey, err := repository.GenerateToken("gk-", 8)
	if err != nil {
		return nil, err
	}
	apiSecret, err := repository.GenerateToken("gs-", 16)
	if err != nil {
		return nil, err
	}

	keyRecord, err := s.store.CreateAPIKey(ctx, repository.CreateAPIKeyParams{
		UserID:           userID,
		ProjectID:        firstNonEmpty(subscription.ProjectID, serviceRecord.ProjectID),
		Key:              apiKey,
		SecretHash:       repository.HashSecret(apiSecret),
		Status:           repository.StatusActive,
		BudgetUSD:        subscription.RequestedBudgetUSD,
		RateLimitQPS:     subscription.RequestedRateLimitQPS,
		AllowedModels:    singleNonEmpty(runtime.snapshot.DefaultModel),
		AllowedProviders: singleNonEmpty(runtime.snapshot.DefaultProvider),
		AllowedServices:  []string{runtime.snapshot.RequestPrefix},
	})
	if err != nil {
		return nil, err
	}

	status := "approved"
	review := reviewNote
	updated, err := s.store.UpdateServiceSubscription(ctx, tenantID, subscription.ID, repository.UpdateServiceSubscriptionParams{
		Status:           &status,
		ReviewNote:       &review,
		ApprovedAPIKeyID: &keyRecord.ID,
		ApprovedUserID:   &userID,
	})
	if err != nil {
		return nil, err
	}

	return &ReviewSubscriptionResult{
		Subscription: updated,
		APIKey:       keyRecord,
		APISecret:    apiSecret,
	}, nil
}

func (s *Service) Create(ctx context.Context, identity *repository.AuthIdentity, prefix, surface string, req *provider.ResponseRequest, sessionID string) (*responseSvc.CreateResult, *repository.ServiceRecord, error) {
	runtime, preparedReq, err := s.prepareRuntimeRequest(ctx, identity, prefix, surface, req)
	if err != nil {
		return nil, nil, err
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

func (s *Service) applyRequestPolicy(policy *repository.ServicePolicyConfig, req *provider.ResponseRequest) error {
	if policy == nil || !policy.Enabled || policy.Request == nil || req == nil {
		return nil
	}
	rules := policy.Request
	if len(rules.AllowModels) > 0 && !containsString(rules.AllowModels, req.Model) {
		return fmt.Errorf("%w: model not in allowlist", ErrPolicyViolation)
	}
	if containsString(rules.BlockModels, req.Model) {
		return fmt.Errorf("%w: model blocked", ErrPolicyViolation)
	}
	text := req.InputText()
	if rules.MaxInputChars > 0 && len(text) > rules.MaxInputChars {
		return fmt.Errorf("%w: input exceeds max_input_chars", ErrPolicyViolation)
	}
	if err := checkBlockedContent(rules, text); err != nil {
		return err
	}
	if len(rules.RedactTerms) > 0 {
		for i := range req.Messages {
			for j := range req.Messages[i].Content {
				if req.Messages[i].Content[j].Text != "" {
					req.Messages[i].Content[j].Text = redactText(req.Messages[i].Content[j].Text, rules.RedactTerms)
				}
			}
		}
		req.Input = req.InputMessages()
	}
	return nil
}

func (s *Service) applyResponsePolicy(ctx context.Context, identity *repository.AuthIdentity, runtime *serviceRuntime, resp *provider.Response) error {
	if runtime == nil || runtime.snapshot.Config.Policy == nil || !runtime.snapshot.Config.Policy.Enabled || runtime.snapshot.Config.Policy.Response == nil || resp == nil {
		return nil
	}
	rules := runtime.snapshot.Config.Policy.Response
	text := resp.OutputText()
	if err := checkBlockedContent(rules, text); err != nil {
		_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
			ID:           resp.ID,
			TenantID:     identity.TenantID,
			ProjectID:    identity.ProjectID,
			ProviderName: runtime.snapshot.DefaultProvider,
			Model:        resp.Model,
			Status:       "error",
		})
		return err
	}
	if rules.MaxOutputChars > 0 && len(text) > rules.MaxOutputChars {
		return fmt.Errorf("%w: output exceeds max_output_chars", ErrPolicyViolation)
	}
	if len(rules.RedactTerms) > 0 {
		for i := range resp.Output {
			for j := range resp.Output[i].Content {
				if resp.Output[i].Content[j].Text != "" {
					resp.Output[i].Content[j].Text = redactText(resp.Output[i].Content[j].Text, rules.RedactTerms)
				}
			}
		}
		body, _ := json.Marshal(resp)
		_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
			ID:           resp.ID,
			TenantID:     identity.TenantID,
			ProjectID:    identity.ProjectID,
			ProviderName: runtime.snapshot.DefaultProvider,
			Model:        resp.Model,
			Status:       "completed",
			ResponseBody: body,
		})
	}
	return nil
}

func (s *Service) filterResponseStream(ctx context.Context, runtime *serviceRuntime, stream *responseSvc.Stream, out chan<- provider.ResponseEvent, errCh chan<- error) {
	defer close(out)
	defer close(errCh)

	var buffer bytes.Buffer
	policy := runtime.snapshot.Config.Policy.Response

	for {
		select {
		case event, ok := <-stream.Events:
			if !ok {
				return
			}
			if event.Type == provider.EventContentDelta && event.Text() != "" {
				text := event.Text()
				if len(policy.RedactTerms) > 0 {
					text = redactText(text, policy.RedactTerms)
				}
				buffer.WriteString(text)
				if err := checkBlockedContent(policy, buffer.String()); err != nil {
					errCh <- err
					return
				}
				if policy.MaxOutputChars > 0 && buffer.Len() > policy.MaxOutputChars {
					errCh <- fmt.Errorf("%w: output exceeds max_output_chars", ErrPolicyViolation)
					return
				}
				event.Delta = text
				event.TextDelta = text
			}
			if event.Type == provider.EventResponseCompleted && event.Response != nil {
				if err := s.applyResponsePolicy(ctx, &repository.AuthIdentity{TenantID: runtime.service.TenantID, ProjectID: runtime.service.ProjectID}, runtime, event.Response); err != nil {
					errCh <- err
					return
				}
			}
			out <- event
		case err, ok := <-stream.Errors:
			if ok && err != nil {
				errCh <- err
			}
			return
		case <-ctx.Done():
			errCh <- ctx.Err()
			return
		}
	}
}

func snapshotFromService(record repository.ServiceRecord) repository.ServiceSnapshot {
	return repository.ServiceSnapshot{
		Name:            record.Name,
		RequestPrefix:   record.RequestPrefix,
		Description:     record.Description,
		DefaultProvider: record.DefaultProvider,
		DefaultModel:    record.DefaultModel,
		Enabled:         record.Enabled,
		Config:          record.Config,
	}
}

func cloneResponseRequest(req *provider.ResponseRequest) *provider.ResponseRequest {
	if req == nil {
		return nil
	}
	cloned := *req
	cloned.Messages = req.InputMessages()
	cloned.Input = cloned.Messages
	cloned.OutputFormat = cloneOutputFormat(req.OutputFormat)
	cloned.Options = provider.CloneRequestOptions(req.Options)
	return &cloned
}

func cloneOutputFormat(value *provider.OutputFormat) *provider.OutputFormat {
	if value == nil {
		return nil
	}
	cloned := *value
	if value.Schema != nil {
		cloned.Schema = map[string]any{}
		for key, item := range value.Schema {
			cloned.Schema[key] = item
		}
	}
	if value.Raw != nil {
		cloned.Raw = map[string]any{}
		for key, item := range value.Raw {
			cloned.Raw[key] = item
		}
	}
	return &cloned
}

func renderTemplate(template string, variables []repository.PromptTemplateVariable, values map[string]any) (string, error) {
	if template == "" {
		return "", nil
	}
	result := template
	for _, variable := range variables {
		value, ok := values[variable.Name]
		if !ok || fmt.Sprint(value) == "" {
			if variable.Required && variable.Default == "" {
				return "", fmt.Errorf("%w: %s", ErrPromptVariableMissing, variable.Name)
			}
			value = variable.Default
		}
		placeholder := regexp.MustCompile(`\{\{\s*` + regexp.QuoteMeta(variable.Name) + `\s*\}\}`)
		result = placeholder.ReplaceAllString(result, fmt.Sprint(value))
	}
	return result, nil
}

func checkBlockedContent(rules *repository.GuardrailRuleSet, text string) error {
	if rules == nil || text == "" {
		return nil
	}
	for _, term := range rules.BlockTerms {
		if term != "" && strings.Contains(strings.ToLower(text), strings.ToLower(term)) {
			return fmt.Errorf("%w: blocked term matched", ErrPolicyViolation)
		}
	}
	for _, pattern := range rules.BlockRegex {
		if pattern == "" {
			continue
		}
		matched, err := regexp.MatchString(pattern, text)
		if err == nil && matched {
			return fmt.Errorf("%w: blocked regex matched", ErrPolicyViolation)
		}
	}
	return nil
}

func redactText(text string, terms []string) string {
	result := text
	for _, term := range terms {
		if term == "" {
			continue
		}
		result = strings.ReplaceAll(result, term, "[REDACTED]")
	}
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func singleNonEmpty(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{strings.TrimSpace(value)}
}

func mergeServicePolicies(base, overlay *repository.ServicePolicyConfig) *repository.ServicePolicyConfig {
	if base == nil && overlay == nil {
		return nil
	}
	merged := cloneServicePolicy(base)
	if merged == nil {
		merged = &repository.ServicePolicyConfig{}
	}
	if overlay != nil {
		merged.Request = mergeGuardrailRuleSets(merged.Request, overlay.Request)
		merged.Response = mergeGuardrailRuleSets(merged.Response, overlay.Response)
		merged.Enabled = merged.Enabled || overlay.Enabled
	}
	if merged.Request == nil && merged.Response == nil && !merged.Enabled {
		return nil
	}
	if policyHasRules(merged) {
		merged.Enabled = true
	}
	return merged
}

func cloneServicePolicy(policy *repository.ServicePolicyConfig) *repository.ServicePolicyConfig {
	if policy == nil {
		return nil
	}
	return &repository.ServicePolicyConfig{
		Enabled:  policy.Enabled,
		Request:  cloneGuardrailRuleSet(policy.Request),
		Response: cloneGuardrailRuleSet(policy.Response),
	}
}

func mergeGuardrailRuleSets(base, overlay *repository.GuardrailRuleSet) *repository.GuardrailRuleSet {
	if base == nil && overlay == nil {
		return nil
	}
	if base == nil {
		return cloneGuardrailRuleSet(overlay)
	}
	if overlay == nil {
		return cloneGuardrailRuleSet(base)
	}
	merged := cloneGuardrailRuleSet(base)
	merged.AllowModels = mergeAllowModels(base.AllowModels, overlay.AllowModels)
	merged.BlockModels = mergeUniqueStrings(base.BlockModels, overlay.BlockModels)
	merged.BlockTerms = mergeUniqueStrings(base.BlockTerms, overlay.BlockTerms)
	merged.BlockRegex = mergeUniqueStrings(base.BlockRegex, overlay.BlockRegex)
	merged.RedactTerms = mergeUniqueStrings(base.RedactTerms, overlay.RedactTerms)
	merged.MaxInputChars = minPositive(base.MaxInputChars, overlay.MaxInputChars)
	merged.MaxOutputChars = minPositive(base.MaxOutputChars, overlay.MaxOutputChars)
	if !guardrailRuleSetHasRules(merged) {
		return nil
	}
	return merged
}

func cloneGuardrailRuleSet(rules *repository.GuardrailRuleSet) *repository.GuardrailRuleSet {
	if rules == nil {
		return nil
	}
	return &repository.GuardrailRuleSet{
		AllowModels:    append([]string(nil), rules.AllowModels...),
		BlockModels:    append([]string(nil), rules.BlockModels...),
		BlockTerms:     append([]string(nil), rules.BlockTerms...),
		BlockRegex:     append([]string(nil), rules.BlockRegex...),
		RedactTerms:    append([]string(nil), rules.RedactTerms...),
		MaxInputChars:  rules.MaxInputChars,
		MaxOutputChars: rules.MaxOutputChars,
	}
}

func mergeAllowModels(base, overlay []string) []string {
	base = normalizeStringList(base)
	overlay = normalizeStringList(overlay)
	if len(base) == 0 {
		return overlay
	}
	if len(overlay) == 0 {
		return base
	}
	allowed := make(map[string]struct{}, len(overlay))
	for _, item := range overlay {
		allowed[item] = struct{}{}
	}
	result := make([]string, 0, len(base))
	for _, item := range base {
		if _, ok := allowed[item]; ok {
			result = append(result, item)
		}
	}
	if len(result) == 0 {
		return []string{"__gateyes_deny_all__"}
	}
	return result
}

func mergeUniqueStrings(base, overlay []string) []string {
	base = normalizeStringList(base)
	overlay = normalizeStringList(overlay)
	if len(base) == 0 {
		return overlay
	}
	if len(overlay) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base)+len(overlay))
	result := make([]string, 0, len(base)+len(overlay))
	for _, items := range [][]string{base, overlay} {
		for _, item := range items {
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}

func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func minPositive(a, b int) int {
	switch {
	case a <= 0:
		return b
	case b <= 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
}

func policyHasRules(policy *repository.ServicePolicyConfig) bool {
	if policy == nil {
		return false
	}
	return guardrailRuleSetHasRules(policy.Request) || guardrailRuleSetHasRules(policy.Response)
}

func guardrailRuleSetHasRules(rules *repository.GuardrailRuleSet) bool {
	if rules == nil {
		return false
	}
	return len(normalizeStringList(rules.AllowModels)) > 0 ||
		len(normalizeStringList(rules.BlockModels)) > 0 ||
		len(normalizeStringList(rules.BlockTerms)) > 0 ||
		len(normalizeStringList(rules.BlockRegex)) > 0 ||
		len(normalizeStringList(rules.RedactTerms)) > 0 ||
		rules.MaxInputChars > 0 ||
		rules.MaxOutputChars > 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func generatePlaceholderKey(seed string) string {
	return "bootstrap-" + strings.ReplaceAll(seed, "-", "")
}

func firstSoftAlertScope(scopes []budget.ScopeResult) string {
	for _, s := range scopes {
		if s.Policy == repository.BudgetPolicySoftAlert {
			return s.Scope
		}
	}
	return "unknown"
}
