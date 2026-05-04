package discovery

import (
	"context"
	"fmt"
)

// Endpoint is a single discovered service instance.
type Endpoint struct {
	Address  string // host:port
	Weight   int
	Metadata map[string]string
}

// ServiceDiscovery discovers backend endpoints for a service name.
type ServiceDiscovery interface {
	// Watch returns the current endpoints for a service and optionally starts a background watch.
	// The implementation should cache results and refresh periodically or on event.
	Watch(ctx context.Context, serviceName string) ([]Endpoint, error)

	// Close releases resources held by the discovery client.
	Close() error
}

// Registry holds multiple discovery backends keyed by type.
type Registry struct {
	discoveries map[string]ServiceDiscovery
	defaultType string
}

func NewRegistry(defaultType string) *Registry {
	if defaultType == "" {
		defaultType = "static"
	}
	return &Registry{
		discoveries: make(map[string]ServiceDiscovery),
		defaultType: defaultType,
	}
}

func (r *Registry) Register(discoveryType string, sd ServiceDiscovery) {
	r.discoveries[discoveryType] = sd
}

func (r *Registry) Discover(ctx context.Context, discoveryType, serviceName string) ([]Endpoint, error) {
	if discoveryType == "" {
		discoveryType = r.defaultType
	}
	sd, ok := r.discoveries[discoveryType]
	if !ok {
		return nil, fmt.Errorf("unknown discovery type: %s", discoveryType)
	}
	return sd.Watch(ctx, serviceName)
}

func (r *Registry) Close() error {
	var errs []error
	for _, sd := range r.discoveries {
		if err := sd.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("close discovery registry: %v", errs)
	}
	return nil
}
