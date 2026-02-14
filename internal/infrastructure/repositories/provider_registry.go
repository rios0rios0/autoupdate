package repositories

import (
	"fmt"

	domainRepos "github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// ProviderFactory is a constructor function that creates a ProviderRepository given an auth token.
type ProviderFactory func(token string) domainRepos.ProviderRepository

// ProviderRegistry manages all registered Git provider implementations.
type ProviderRegistry struct {
	providers map[string]ProviderFactory
}

// NewProviderRegistry creates an empty provider registry.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		providers: make(map[string]ProviderFactory),
	}
}

// Register adds a provider factory under the given name (e.g. "github").
func (r *ProviderRegistry) Register(name string, factory ProviderFactory) {
	r.providers[name] = factory
}

// Get returns a configured provider instance for the given name and token.
func (r *ProviderRegistry) Get(name, token string) (domainRepos.ProviderRepository, error) {
	factory, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider type: %q", name)
	}
	return factory(token), nil
}

// Names returns the list of registered provider names.
func (r *ProviderRegistry) Names() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}
