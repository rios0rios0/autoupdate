package repositories

import (
	adoRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/azuredevops"
	ghRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/github"
	glRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlab"
	goRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
	jsRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/javascript"
	pyRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/python"
	tfRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/terraform"
	"go.uber.org/dig"
)

// RegisterProviders registers all repository providers with the DIG container.
func RegisterProviders(container *dig.Container) error {
	// Register provider registry with all provider factories
	if err := container.Provide(func() *ProviderRegistry {
		reg := NewProviderRegistry()
		reg.Register("github", ghRepo.NewGitHubProviderRepository)
		reg.Register("gitlab", glRepo.NewGitLabProviderRepository)
		reg.Register("azuredevops", adoRepo.NewAzureDevOpsProviderRepository)
		return reg
	}); err != nil {
		return err
	}

	// Register updater registry with all updater implementations
	if err := container.Provide(func() *UpdaterRegistry {
		reg := NewUpdaterRegistry()
		reg.Register(tfRepo.NewTerraformUpdaterRepository())
		reg.Register(goRepo.NewGolangUpdaterRepository())
		reg.Register(pyRepo.NewPythonUpdaterRepository())
		reg.Register(jsRepo.NewJavaScriptUpdaterRepository())
		return reg
	}); err != nil {
		return err
	}

	return nil
}
