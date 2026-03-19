//go:build unit

package commands_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	infraRepos "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
	entitybuilders "github.com/rios0rios0/autoupdate/test/domain/entitybuilders"
	doubles "github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestRunCommandExecute(t *testing.T) {
	t.Parallel()

	t.Run("should skip provider when ProviderName filter does not match", func(t *testing.T) {
		// given
		spy := doubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("github").
			WithToken("test-token").
			BuildSpy()

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return spy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()

		cmd := commands.NewRunCommand(providerRegistry, updaterRegistry)

		settings := entitybuilders.NewSettingsBuilder().
			WithProviders([]entities.ProviderConfig{
				entitybuilders.NewProviderConfigBuilder().
					WithType("github").
					WithToken("test-token").
					WithOrganizations([]string{"test-org"}).
					BuildProviderConfig(),
			}).
			BuildSettings()
		opts := commands.RunOptions{ProviderName: "gitlab"} // filter does not match

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Empty(t, spy.DiscoveredOrgs)
	})

	t.Run("should call DiscoverRepositories for matching provider", func(t *testing.T) {
		// given
		spy := doubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("github").
			WithToken("test-token").
			WithRepositories([]entities.Repository{}).
			BuildSpy()

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return spy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()

		cmd := commands.NewRunCommand(providerRegistry, updaterRegistry)

		settings := entitybuilders.NewSettingsBuilder().
			WithProviders([]entities.ProviderConfig{
				entitybuilders.NewProviderConfigBuilder().
					WithType("github").
					WithToken("test-token").
					WithOrganizations([]string{"test-org"}).
					BuildProviderConfig(),
			}).
			BuildSettings()
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Contains(t, spy.DiscoveredOrgs, "test-org")
	})

	t.Run("should continue when DiscoverRepositories returns error", func(t *testing.T) {
		// given
		spy := doubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("github").
			WithToken("test-token").
			WithDiscoverErr(errors.New("network error")).
			BuildSpy()

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return spy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()

		cmd := commands.NewRunCommand(providerRegistry, updaterRegistry)

		settings := entitybuilders.NewSettingsBuilder().
			WithProviders([]entities.ProviderConfig{
				entitybuilders.NewProviderConfigBuilder().
					WithType("github").
					WithToken("test-token").
					WithOrganizations([]string{"org1", "org2"}).
					BuildProviderConfig(),
			}).
			BuildSettings()
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err) // should not fail overall
		assert.Len(t, spy.DiscoveredOrgs, 2)
	})

	t.Run("should call updater Detect and CreateUpdatePRs for discovered repos", func(t *testing.T) {
		// given
		repo := entitybuilders.NewRepositoryBuilder().
			WithID("repo-1").
			WithName("test-repo").
			WithOrganization("test-org").
			WithDefaultBranch("refs/heads/main").
			BuildRepository()

		spy := doubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("github").
			WithToken("test-token").
			WithRepositories([]entities.Repository{repo}).
			BuildSpy()

		updaterSpy := doubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("terraform").
			WithDetectResult(true).
			WithPRs([]entities.PullRequest{{ID: 42, Title: "Update dep", URL: "https://example.com/pr/42"}}).
			BuildSpy()

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return spy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()
		updaterRegistry.Register(updaterSpy)

		cmd := commands.NewRunCommand(providerRegistry, updaterRegistry)

		settings := entitybuilders.NewSettingsBuilder().
			WithProviders([]entities.ProviderConfig{
				entitybuilders.NewProviderConfigBuilder().
					WithType("github").
					WithToken("test-token").
					WithOrganizations([]string{"test-org"}).
					BuildProviderConfig(),
			}).
			BuildSettings()
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Len(t, updaterSpy.DetectedRepos, 1)
		assert.Equal(t, "test-repo", updaterSpy.DetectedRepos[0].Name)
		assert.Len(t, updaterSpy.CreatePRsCalls, 1)
		assert.Equal(t, "test-repo", updaterSpy.CreatePRsCalls[0].Repo.Name)
	})

	t.Run("should skip disabled updaters", func(t *testing.T) {
		// given
		repo := entitybuilders.NewRepositoryBuilder().
			WithID("repo-1").
			WithName("test-repo").
			WithOrganization("test-org").
			WithDefaultBranch("refs/heads/main").
			BuildRepository()

		spy := doubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("github").
			WithToken("test-token").
			WithRepositories([]entities.Repository{repo}).
			BuildSpy()

		updaterSpy := doubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("terraform").
			WithDetectResult(true).
			BuildSpy()

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return spy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()
		updaterRegistry.Register(updaterSpy)

		cmd := commands.NewRunCommand(providerRegistry, updaterRegistry)

		settings := entitybuilders.NewSettingsBuilder().
			WithProviders([]entities.ProviderConfig{
				entitybuilders.NewProviderConfigBuilder().
					WithType("github").
					WithToken("test-token").
					WithOrganizations([]string{"test-org"}).
					BuildProviderConfig(),
			}).
			WithUpdaters(map[string]entities.UpdaterConfig{
				"terraform": entitybuilders.NewUpdaterConfigBuilder().
					WithEnabled(false).
					BuildUpdaterConfig(),
			}).
			BuildSettings()
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Empty(t, updaterSpy.CreatePRsCalls)
	})

	t.Run("should not skip updater when enabled is omitted from config", func(t *testing.T) {
		// given
		repo := entitybuilders.NewRepositoryBuilder().
			WithID("repo-1").
			WithName("test-repo").
			WithOrganization("test-org").
			WithDefaultBranch("refs/heads/main").
			BuildRepository()

		spy := doubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("github").
			WithToken("test-token").
			WithRepositories([]entities.Repository{repo}).
			BuildSpy()

		updaterSpy := doubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("terraform").
			WithDetectResult(true).
			WithPRs([]entities.PullRequest{{ID: 42, Title: "Update dep", URL: "https://example.com/pr/42"}}).
			BuildSpy()

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return spy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()
		updaterRegistry.Register(updaterSpy)

		cmd := commands.NewRunCommand(providerRegistry, updaterRegistry)

		settings := entitybuilders.NewSettingsBuilder().
			WithProviders([]entities.ProviderConfig{
				entitybuilders.NewProviderConfigBuilder().
					WithType("github").
					WithToken("test-token").
					WithOrganizations([]string{"test-org"}).
					BuildProviderConfig(),
			}).
			WithUpdaters(map[string]entities.UpdaterConfig{
				"terraform": entitybuilders.NewUpdaterConfigBuilder().
					WithTargetBranch("main").
					BuildUpdaterConfig(),
			}).
			BuildSettings()
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Len(t, updaterSpy.CreatePRsCalls, 1)
		assert.Equal(t, "test-repo", updaterSpy.CreatePRsCalls[0].Repo.Name)
	})

	t.Run("should respect UpdaterName filter", func(t *testing.T) {
		// given
		repo := entitybuilders.NewRepositoryBuilder().
			WithID("repo-1").
			WithName("test-repo").
			WithOrganization("test-org").
			WithDefaultBranch("refs/heads/main").
			BuildRepository()

		spy := doubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("github").
			WithToken("test-token").
			WithRepositories([]entities.Repository{repo}).
			BuildSpy()

		terraformSpy := doubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("terraform").
			WithDetectResult(true).
			BuildSpy()
		golangSpy := doubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("golang").
			WithDetectResult(true).
			BuildSpy()

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return spy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()
		updaterRegistry.Register(terraformSpy)
		updaterRegistry.Register(golangSpy)

		cmd := commands.NewRunCommand(providerRegistry, updaterRegistry)

		settings := entitybuilders.NewSettingsBuilder().
			WithProviders([]entities.ProviderConfig{
				entitybuilders.NewProviderConfigBuilder().
					WithType("github").
					WithToken("test-token").
					WithOrganizations([]string{"test-org"}).
					BuildProviderConfig(),
			}).
			BuildSettings()
		opts := commands.RunOptions{UpdaterName: "golang"} // only run golang updater

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Empty(t, terraformSpy.CreatePRsCalls)
		assert.Len(t, golangSpy.DetectedRepos, 1)
	})
}
