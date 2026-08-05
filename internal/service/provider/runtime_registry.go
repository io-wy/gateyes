package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/gateyes/gateway/internal/repository"
)

type RuntimeRegistryService struct {
	store   repository.Store
	manager *Manager
}

type ProviderView struct {
	Provider Provider
	Registry *repository.ProviderRegistryRecord
	Stats    *ProviderStats
	Usage    repository.ProviderUsageStats
}

type RegistryPatch struct {
	Enabled                  *bool             `json:"enabled"`
	Drain                    *bool             `json:"drain"`
	HealthStatus             *string           `json:"health_status"`
	RoutingWeight            *int              `json:"routing_weight"`
	Type                     *string           `json:"type"`
	Vendor                   *string           `json:"vendor"`
	BaseURL                  *string           `json:"base_url"`
	Endpoint                 *string           `json:"endpoint"`
	APIKey                   *string           `json:"api_key"`
	Model                    *string           `json:"model"`
	PriceInput               *float64          `json:"price_input"`
	PriceOutput              *float64          `json:"price_output"`
	MaxTokens                *int              `json:"max_tokens"`
	Timeout                  *int              `json:"timeout"`
	Headers                  map[string]string `json:"headers"`
	ExtraBody                map[string]any    `json:"extra_body"`
	Labels                   map[string]string `json:"labels"`
	SupportsChat             *bool             `json:"supports_chat"`
	SupportsResponses        *bool             `json:"supports_responses"`
	SupportsMessages         *bool             `json:"supports_messages"`
	SupportsStream           *bool             `json:"supports_stream"`
	SupportsTools            *bool             `json:"supports_tools"`
	SupportsImages           *bool             `json:"supports_images"`
	SupportsStructuredOutput *bool             `json:"supports_structured_output"`
	SupportsLongContext      *bool             `json:"supports_long_context"`
	SupportsEmbeddings       *bool             `json:"supports_embeddings"`
}

func NewRuntimeRegistryService(store repository.Store, manager *Manager) *RuntimeRegistryService {
	return &RuntimeRegistryService{
		store:   store,
		manager: manager,
	}
}

func (s *RuntimeRegistryService) List(ctx context.Context, tenantID string) ([]ProviderView, error) {
	usageByProvider, err := s.store.GetProviderUsageSummary(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	var providers []Provider
	if tenantID == "" {
		providers = s.manager.List()
	} else {
		providerNames, err := s.store.ListTenantProviders(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		providers = s.manager.ListByNames(providerNames)
	}

	statsByName := make(map[string]*ProviderStats)
	for _, item := range s.manager.Stats.List() {
		statsByName[item.Name] = item
	}

	result := make([]ProviderView, 0, len(providers))
	for _, providerItem := range providers {
		view := ProviderView{
			Provider: providerItem,
			Stats:    statsByName[providerItem.Name()],
			Usage:    usageByProvider[providerItem.Name()],
		}
		if record, ok := s.manager.Registry(providerItem.Name()); ok {
			copied := record
			view.Registry = &copied
		}
		result = append(result, view)
	}
	return result, nil
}

func (s *RuntimeRegistryService) Get(ctx context.Context, tenantID string, name string) (*ProviderView, error) {
	views, err := s.List(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, view := range views {
		if view.Provider.Name() == name {
			return &view, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (s *RuntimeRegistryService) CreateForTenant(ctx context.Context, tenantID string, record repository.ProviderRegistryRecord) (*repository.ProviderRegistryRecord, error) {
	created, err := s.Upsert(ctx, record)
	if err != nil {
		return nil, err
	}
	if tenantID != "" {
		if err := s.appendTenantProvider(ctx, tenantID, created.Name); err != nil {
			return nil, err
		}
	}
	return created, nil
}

func (s *RuntimeRegistryService) Update(ctx context.Context, name string, patch RegistryPatch) (*repository.ProviderRegistryRecord, error) {
	if patch.HealthStatus != nil && !validProviderHealthStatus(*patch.HealthStatus) {
		return nil, fmt.Errorf("invalid health_status")
	}
	current, err := s.store.GetProviderRegistry(ctx, name)
	if err != nil {
		return nil, err
	}
	return s.Upsert(ctx, mergeRegistryPatch(*current, patch))
}

func (s *RuntimeRegistryService) DeleteForTenant(ctx context.Context, tenantID string, name string) error {
	if err := s.Delete(ctx, name); err != nil {
		return err
	}
	if tenantID != "" {
		_ = s.removeTenantProvider(ctx, tenantID, name)
	}
	return nil
}

func mergeRegistryPatch(current repository.ProviderRegistryRecord, patch RegistryPatch) repository.ProviderRegistryRecord {
	next := current
	if patch.Type != nil {
		next.Type = *patch.Type
	}
	if patch.Vendor != nil {
		next.Vendor = *patch.Vendor
	}
	if patch.BaseURL != nil {
		next.BaseURL = *patch.BaseURL
	}
	if patch.Endpoint != nil {
		next.Endpoint = *patch.Endpoint
	}
	if patch.Model != nil {
		next.Model = *patch.Model
	}
	if patch.Enabled != nil {
		next.Enabled = *patch.Enabled
	}
	if patch.Drain != nil {
		next.Drain = *patch.Drain
	}
	if patch.HealthStatus != nil {
		next.HealthStatus = *patch.HealthStatus
	}
	if patch.RoutingWeight != nil {
		next.RoutingWeight = *patch.RoutingWeight
	}
	if next.RuntimeConfig == nil {
		next.RuntimeConfig = &repository.ProviderRuntimeConfig{Enabled: next.Enabled}
	}
	if patch.APIKey != nil {
		next.RuntimeConfig.APIKey = *patch.APIKey
	}
	if patch.PriceInput != nil {
		next.RuntimeConfig.PriceInput = *patch.PriceInput
	}
	if patch.PriceOutput != nil {
		next.RuntimeConfig.PriceOutput = *patch.PriceOutput
	}
	if patch.MaxTokens != nil {
		next.RuntimeConfig.MaxTokens = *patch.MaxTokens
	}
	if patch.Timeout != nil {
		next.RuntimeConfig.Timeout = *patch.Timeout
	}
	if patch.Headers != nil {
		next.RuntimeConfig.Headers = patch.Headers
	}
	if patch.ExtraBody != nil {
		next.RuntimeConfig.ExtraBody = patch.ExtraBody
	}
	if patch.Labels != nil {
		next.RuntimeConfig.Labels = patch.Labels
	}
	next.RuntimeConfig.Enabled = next.Enabled
	applyCapabilityPatch(&next, patch)
	return next
}

func applyCapabilityPatch(record *repository.ProviderRegistryRecord, patch RegistryPatch) {
	if patch.SupportsChat != nil {
		record.SupportsChat = *patch.SupportsChat
	}
	if patch.SupportsResponses != nil {
		record.SupportsResponses = *patch.SupportsResponses
	}
	if patch.SupportsMessages != nil {
		record.SupportsMessages = *patch.SupportsMessages
	}
	if patch.SupportsStream != nil {
		record.SupportsStream = *patch.SupportsStream
	}
	if patch.SupportsTools != nil {
		record.SupportsTools = *patch.SupportsTools
	}
	if patch.SupportsImages != nil {
		record.SupportsImages = *patch.SupportsImages
	}
	if patch.SupportsStructuredOutput != nil {
		record.SupportsStructuredOutput = *patch.SupportsStructuredOutput
	}
	if patch.SupportsLongContext != nil {
		record.SupportsLongContext = *patch.SupportsLongContext
	}
	if patch.SupportsEmbeddings != nil {
		record.SupportsEmbeddings = *patch.SupportsEmbeddings
	}
}

func validProviderHealthStatus(value string) bool {
	switch value {
	case ProviderHealthHealthy, ProviderHealthDegraded, ProviderHealthUnhealthy:
		return true
	default:
		return false
	}
}

func (s *RuntimeRegistryService) Upsert(ctx context.Context, record repository.ProviderRegistryRecord) (*repository.ProviderRegistryRecord, error) {
	record.Name = strings.TrimSpace(record.Name)
	if record.Name == "" {
		return nil, fmt.Errorf("provider name is required")
	}
	previous, previousErr := s.store.GetProviderRegistry(ctx, record.Name)
	if err := s.manager.UpsertRuntimeProvider(record); err != nil {
		return nil, err
	}
	if err := s.store.UpsertProviderRegistry(ctx, record); err != nil {
		if previousErr == nil && previous != nil {
			_ = s.manager.UpsertRuntimeProvider(*previous)
		} else {
			s.manager.RemoveRuntimeProvider(record.Name)
		}
		return nil, err
	}
	return s.store.GetProviderRegistry(ctx, record.Name)
}

func (s *RuntimeRegistryService) Delete(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("provider name is required")
	}
	if err := s.store.DeleteProviderRegistry(ctx, name); err != nil {
		return err
	}
	s.manager.RemoveRuntimeProvider(name)
	return nil
}

func (s *RuntimeRegistryService) appendTenantProvider(ctx context.Context, tenantID, name string) error {
	names, err := s.store.ListTenantProviders(ctx, tenantID)
	if err != nil {
		return err
	}
	for _, item := range names {
		if item == name {
			return nil
		}
	}
	names = append(names, name)
	return s.store.ReplaceTenantProviders(ctx, tenantID, names)
}

func (s *RuntimeRegistryService) removeTenantProvider(ctx context.Context, tenantID, name string) error {
	names, err := s.store.ListTenantProviders(ctx, tenantID)
	if err != nil {
		return err
	}
	filtered := make([]string, 0, len(names))
	for _, item := range names {
		if item != name {
			filtered = append(filtered, item)
		}
	}
	return s.store.ReplaceTenantProviders(ctx, tenantID, filtered)
}
