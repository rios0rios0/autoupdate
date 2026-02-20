package repositories

import (
	domainRepos "github.com/rios0rios0/autoupdate/internal/domain/repositories"
	goRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
	jsRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/javascript"
	pyRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/python"
	tfRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/terraform"
	gitforgeEntities "github.com/rios0rios0/gitforge/pkg/global/domain/entities"
	"github.com/rios0rios0/gitforge/pkg/providers/infrastructure/azuredevops"
	"github.com/rios0rios0/gitforge/pkg/providers/infrastructure/github"
	"github.com/rios0rios0/gitforge/pkg/providers/infrastructure/gitlab"
	"go.uber.org/dig"
)

// asFileAccessProvider converts a ForgeProvider to a ProviderRepository.
// All gitforge providers are expected to implement ProviderRepository; this is checked at runtime.
func asFileAccessProvider(p gitforgeEntities.ForgeProvider) domainRepos.ProviderRepository {
	fp, ok := p.(domainRepos.ProviderRepository)
	if !ok {
		panic("gitforge provider does not implement domainRepos.ProviderRepository")
	}
	return fp
}

// RegisterProviders registers all repository providers with the DIG container.
func RegisterProviders(container *dig.Container) error {
	// Register provider registry with gitforge provider factories
	if err := container.Provide(func() *ProviderRegistry {
		reg := NewProviderRegistry()
		reg.Register("github", func(token string) domainRepos.ProviderRepository {
			return asFileAccessProvider(github.NewProvider(token))
		})
		reg.Register("gitlab", func(token string) domainRepos.ProviderRepository {
			return asFileAccessProvider(gitlab.NewProvider(token))
		})
		reg.Register("azuredevops", func(token string) domainRepos.ProviderRepository {
			return asFileAccessProvider(azuredevops.NewProvider(token))
		})
		return reg
	}); err != nil {
		return err
	}

	// Register updater registry with all updater implementations
	if err := container.Provide(func() *UpdaterRegistry {
		reg := NewUpdaterRegistry()
		reg.Register(tfRepo.NewUpdaterRepository())
		reg.Register(goRepo.NewUpdaterRepository())
		reg.Register(pyRepo.NewUpdaterRepository())
		reg.Register(jsRepo.NewUpdaterRepository())
		return reg
	}); err != nil {
		return err
	}

	return nil
}
