package repositories

import (
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	csRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/csharp"
	dartRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/dart"
	dfRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/dockerfile"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlocal"
	goRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
	jvRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/java"
	jsRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/javascript"
	plRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/pipeline"
	pyRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/python"
	rbRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/ruby"
	suRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/selfupdate"
	tfRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/terraform"
	credentialInfra "github.com/rios0rios0/gitforge/v4/pkg/credentials/infrastructure"
	"github.com/rios0rios0/gitforge/v4/pkg/providers/infrastructure/azuredevops"
	"github.com/rios0rios0/gitforge/v4/pkg/providers/infrastructure/github"
	"github.com/rios0rios0/gitforge/v4/pkg/providers/infrastructure/gitlab"
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

	// Register the credential resolver `autoupdate .` falls back to when --token is empty:
	// gitforge's default chain, the forge's environment variables and then its CLI. Building
	// it runs nothing; gh or az only run when a local run asks for a token.
	if err := container.Provide(func() repositories.CredentialResolver {
		return credentialInfra.NewDefaultCredentialResolver()
	}); err != nil {
		return err
	}

	// git's token without --token: the environment alone, read by the same resolver the chain
	// above starts with, so both tokens come from one reading of the variables.
	if err := container.Provide(func() repositories.GitCredentialResolver {
		return credentialInfra.NewEnvCredentialResolver()
	}); err != nil {
		return err
	}

	// What a push to the checkout's origin dials, and which server origin is on.
	if err := container.Provide(func() repositories.OriginRepository {
		return gitlocal.NewOriginRepository()
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
	if err := container.Provide(NewDefaultUpdaterRegistry); err != nil {
		return err
	}

	return nil
}

// NewDefaultUpdaterRegistry returns a registry holding every updater AutoUpdate ships,
// which is the one both modes run with.
func NewDefaultUpdaterRegistry() *UpdaterRegistry {
	reg := NewUpdaterRegistry()
	reg.Register(tfRepo.NewUpdaterRepository())
	reg.Register(goRepo.NewUpdaterRepository())
	reg.Register(pyRepo.NewUpdaterRepository())
	reg.Register(jsRepo.NewUpdaterRepository())
	reg.Register(rbRepo.NewUpdaterRepository())
	reg.Register(jvRepo.NewUpdaterRepository())
	reg.Register(csRepo.NewUpdaterRepository())
	reg.Register(dartRepo.NewUpdaterRepository())
	reg.Register(plRepo.NewUpdaterRepository())
	reg.Register(dfRepo.NewUpdaterRepository())
	return reg
}
