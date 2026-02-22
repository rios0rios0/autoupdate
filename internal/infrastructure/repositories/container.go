package repositories

import (
	domainRepos "github.com/rios0rios0/autoupdate/internal/domain/repositories"
	gitforgeRepos "github.com/rios0rios0/gitforge/domain/repositories"
	goRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
	jsRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/javascript"
	pyRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/python"
	tfRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/terraform"
	"github.com/rios0rios0/gitforge/infrastructure/providers/azuredevops"
	"github.com/rios0rios0/gitforge/infrastructure/providers/github"
	"github.com/rios0rios0/gitforge/infrastructure/providers/gitlab"
	"go.uber.org/dig"
)

// asFileAccessProvider converts a ForgeProvider to a FileAccessProvider.
// All gitforge providers implement FileAccessProvider, so this assertion is safe.
func asFileAccessProvider(p gitforgeRepos.ForgeProvider) domainRepos.ProviderRepository {
	//nolint:errcheck // gitforge providers always implement FileAccessProvider
	return p.(domainRepos.ProviderRepository)
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
