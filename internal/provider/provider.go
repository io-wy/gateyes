package provider

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

const (
	TypeOpenAI = "openai"
)

type ModelProvider interface {
	Name() string
	ServeHTTP(http.ResponseWriter, *http.Request)
	ForwardRequest(*http.Request, []byte) (*http.Response, context.CancelFunc, error)
}

type Registry struct {
	providers map[string]ModelProvider
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]ModelProvider),
	}
}

func (r *Registry) Register(p ModelProvider) error {
	if r == nil {
		return errors.New("provider registry is nil")
	}
	if p == nil {
		return errors.New("provider is nil")
	}

	name := strings.ToLower(strings.TrimSpace(p.Name()))
	if name == "" {
		return errors.New("provider name is required")
	}
	if _, exists := r.providers[name]; exists {
		return errors.New("provider already registered: " + name)
	}

	r.providers[name] = p
	return nil
}

func (r *Registry) Get(name string) (ModelProvider, bool) {
	if r == nil {
		return nil, false
	}
	providerName := strings.ToLower(strings.TrimSpace(name))
	provider, ok := r.providers[providerName]
	return provider, ok
}

func (r *Registry) Has(name string) bool {
	_, ok := r.Get(name)
	return ok
}

func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}
