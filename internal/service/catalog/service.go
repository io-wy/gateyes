package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/alert"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/budget"
	"github.com/gateyes/gateway/internal/service/limiter"
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

// ---- CRUD operations ----

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
			TenantID:   firstNonEmpty(tenantID, subscription.TenantID),
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
