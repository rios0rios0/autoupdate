package repositories

import (
	"fmt"

	domainRepos "github.com/rios0rios0/autoupdate/internal/domain/repositories"
	gitforgeEntities "github.com/rios0rios0/gitforge/pkg/global/domain/entities"
	registryInfra "github.com/rios0rios0/gitforge/pkg/registry/infrastructure"
)

// ProviderFactory is a constructor function that creates a ProviderRepository given an auth token.
type ProviderFactory func(token string) domainRepos.ProviderRepository

// ProviderRegistry wraps gitforge's ProviderRegistry, adapting Get() to return FileAccessProvider.
type ProviderRegistry struct {
	*registryInfra.ProviderRegistry
}

// NewProviderRegistry creates a new provider registry backed by gitforge.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		ProviderRegistry: registryInfra.NewProviderRegistry(),
	}
}

// Register adds a FileAccessProvider factory under the given name.
// This wraps the factory into gitforge's ForgeProvider-based registration.
func (r *ProviderRegistry) Register(name string, factory ProviderFactory) {
	r.RegisterFactory(name, func(token string) gitforgeEntities.ForgeProvider {
		return factory(token)
	})
}

// Get returns a configured FileAccessProvider instance for the given name and token.
func (r *ProviderRegistry) Get(name, token string) (domainRepos.ProviderRepository, error) {
	provider, err := r.ProviderRegistry.Get(name, token)
	if err != nil {
		return nil, err
	}
	fp, ok := provider.(domainRepos.ProviderRepository)
	if !ok {
		return nil, fmt.Errorf("provider %q does not implement FileAccessProvider", name)
	}
	return fp, nil
}
