package repositories

import (
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	csRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/csharp"
	dfRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/dockerfile"
	goRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
	jvRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/java"
	jsRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/javascript"
	plRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/pipeline"
	pyRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/python"
	rbRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/ruby"
	suRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/selfupdate"
	tfRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/terraform"
	"github.com/rios0rios0/gitforge/pkg/providers/infrastructure/azuredevops"
	"github.com/rios0rios0/gitforge/pkg/providers/infrastructure/github"
	"github.com/rios0rios0/gitforge/pkg/providers/infrastructure/gitlab"
	"go.uber.org/dig"
)

// RegisterProviders registers all repository providers with the DIG container.
func RegisterProviders(container *dig.Container) error {
	// Register provider registry using gitforge's factory registration
	if err := container.Provide(func() *ProviderRegistry {
		reg := NewProviderRegistry()
		// Register token-less adapters for URL/service-type matching (used by push auth)
		reg.RegisterAdapter(github.NewProvider(""))
		reg.RegisterAdapter(gitlab.NewProvider(""))
		reg.RegisterAdapter(azuredevops.NewProvider(""))
		// Register factories for creating token-bound provider instances
		reg.RegisterFactory("github", github.NewProvider)
		reg.RegisterFactory("gitlab", gitlab.NewProvider)
		reg.RegisterFactory("azuredevops", azuredevops.NewProvider)
		return reg
	}); err != nil {
		return err
	}

	// Register self-update repository and bind interface to implementation
	if err := container.Provide(suRepo.NewRepository); err != nil {
		return err
	}
	if err := container.Provide(func(impl *suRepo.Repository) repositories.SelfUpdateRepository {
		return impl
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
		reg.Register(rbRepo.NewUpdaterRepository())
		reg.Register(jvRepo.NewUpdaterRepository())
		reg.Register(csRepo.NewUpdaterRepository())
		reg.Register(plRepo.NewUpdaterRepository())
		reg.Register(dfRepo.NewUpdaterRepository())
		return reg
	}); err != nil {
		return err
	}

	return nil
}
