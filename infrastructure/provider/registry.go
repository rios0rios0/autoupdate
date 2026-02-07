package provider

import (
	"fmt"

	"github.com/rios0rios0/autoupdate/domain"
)

// Registry manages all registered Git provider implementations.
type Registry struct {
	providers map[string]Factory
}

// Factory is a constructor function that creates a Provider given an auth token.
type Factory func(token string) domain.Provider

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Factory),
	}
}

// Register adds a provider factory under the given name (e.g. "github").
func (r *Registry) Register(name string, factory Factory) {
	r.providers[name] = factory
}

// Get returns a configured provider instance for the given name and token.
func (r *Registry) Get(name, token string) (domain.Provider, error) {
	factory, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider type: %q", name)
	}
	return factory(token), nil
}

// Names returns the list of registered provider names.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}
