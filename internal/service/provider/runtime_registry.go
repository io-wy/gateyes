package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/gateyes/gateway/internal/repository"
)

type RuntimeRegistryService struct {
	store   repository.ProviderRegistryStore
	manager *Manager
}

func NewRuntimeRegistryService(store repository.ProviderRegistryStore, manager *Manager) *RuntimeRegistryService {
	return &RuntimeRegistryService{
		store:   store,
		manager: manager,
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
