package repositories

import (
	"fmt"

	domainRepos "github.com/rios0rios0/autoupdate/internal/domain/repositories"
	globalEntities "github.com/rios0rios0/gitforge/pkg/global/domain/entities"
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
	r.RegisterFactory(name, func(token string) globalEntities.ForgeProvider {
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

// GetAdapterByURL returns the LocalGitAuthProvider adapter matching the given
// remote URL, or nil if no registered adapter matches.
func (r *ProviderRegistry) GetAdapterByURL(url string) globalEntities.LocalGitAuthProvider {
	adapter := r.ProviderRegistry.GetAdapterByURL(url)
	if adapter == nil {
		return nil
	}
	lgap, ok := adapter.(globalEntities.LocalGitAuthProvider)
	if !ok {
		return nil
	}
	return lgap
}

// GetAdapterByServiceType returns the LocalGitAuthProvider adapter for the
// given service type, or nil if none is registered.
func (r *ProviderRegistry) GetAdapterByServiceType(
	serviceType globalEntities.ServiceType,
) globalEntities.LocalGitAuthProvider {
	return r.ProviderRegistry.GetAdapterByServiceType(serviceType)
}

// GetAuthProvider creates a token-enabled provider for the given service type
// and returns it as a LocalGitAuthProvider for transport authentication.
// It maps the ServiceType to the internal provider name before lookup.
func (r *ProviderRegistry) GetAuthProvider(
	serviceType globalEntities.ServiceType, token string,
) (globalEntities.LocalGitAuthProvider, error) {
	name := registryInfra.ServiceTypeToProviderName(serviceType)
	if name == "" {
		return nil, fmt.Errorf("unsupported service type: %v", serviceType)
	}
	provider, err := r.ProviderRegistry.Get(name, token)
	if err != nil {
		return nil, err
	}
	lgap, ok := provider.(globalEntities.LocalGitAuthProvider)
	if !ok {
		return nil, fmt.Errorf("provider %q does not implement LocalGitAuthProvider", name)
	}
	return lgap, nil
}
