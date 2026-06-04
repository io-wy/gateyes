package filter

import (
	"fmt"
	"sync"
)

// Factory creates a Filter from runtime dependencies.
type Factory func() (Filter, error)

// Registry is a code-level filter registration table.
// Filters are registered by name and can be looked up to build a Pipeline.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]Factory),
	}
}

// Register adds a named factory to the registry.
// Returns an error if the name is already registered.
func (r *Registry) Register(name string, factory Factory) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[name]; exists {
		return fmt.Errorf("filter %q already registered", name)
	}
	r.factories[name] = factory
	return nil
}

// MustRegister is like Register but panics on duplicate names.
// Use during program initialization.
func (r *Registry) MustRegister(name string, factory Factory) {
	if err := r.Register(name, factory); err != nil {
		panic(err)
	}
}

// Get looks up a factory by name.
func (r *Registry) Get(name string) (Factory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.factories[name]
	return f, ok
}

// Names returns all registered filter names in insertion order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	return names
}

// BuildPipeline creates a Pipeline from a list of registered filter names.
// Names are resolved in order; the first missing name returns an error.
func (r *Registry) BuildPipeline(names []string) (*Pipeline, error) {
	chain := make([]Filter, 0, len(names))
	for _, name := range names {
		factory, ok := r.Get(name)
		if !ok {
			return nil, fmt.Errorf("unknown filter %q", name)
		}
		f, err := factory()
		if err != nil {
			return nil, fmt.Errorf("create filter %q: %w", name, err)
		}
		chain = append(chain, f)
	}
	return NewPipeline(chain), nil
}
