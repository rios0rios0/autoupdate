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

	t.Run("should skip updater when Detect returns false", func(t *testing.T) {
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
			WithDetectResult(false). // Detect returns false
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
		assert.Empty(t, updaterSpy.CreatePRsCalls)
	})

	t.Run("should propagate AutoComplete from updater config", func(t *testing.T) {
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
			WithPRs([]entities.PullRequest{{ID: 1, Title: "Update", URL: "https://example.com/pr/1"}}).
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
					WithAutoComplete(true).
					BuildUpdaterConfig(),
			}).
			BuildSettings()
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		require.Len(t, updaterSpy.CreatePRsCalls, 1)
		assert.True(t, updaterSpy.CreatePRsCalls[0].Opts.AutoComplete)
	})

	t.Run("should use target_branch from updater config", func(t *testing.T) {
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
			WithPRs([]entities.PullRequest{{ID: 1, Title: "Update", URL: "https://example.com/pr/1"}}).
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
					WithTargetBranch("develop").
					BuildUpdaterConfig(),
			}).
			BuildSettings()
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		require.Len(t, updaterSpy.CreatePRsCalls, 1)
		assert.Equal(t, "develop", updaterSpy.CreatePRsCalls[0].Opts.TargetBranch)
	})

	t.Run("should continue processing when CreateUpdatePRs returns error", func(t *testing.T) {
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
			WithCreatePRsErr(errors.New("API error")).
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
		require.NoError(t, err) // Execute should not fail overall
		assert.Len(t, updaterSpy.CreatePRsCalls, 1)
	})

	t.Run("should propagate DryRun and Verbose from RunOptions", func(t *testing.T) {
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
			WithPRs([]entities.PullRequest{}).
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
		opts := commands.RunOptions{DryRun: true, Verbose: true}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		require.Len(t, updaterSpy.CreatePRsCalls, 1)
		assert.True(t, updaterSpy.CreatePRsCalls[0].Opts.DryRun)
		assert.True(t, updaterSpy.CreatePRsCalls[0].Opts.Verbose)
	})

	t.Run("should skip organization when OrgOverride does not match", func(t *testing.T) {
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
					WithOrganizations([]string{"org-a", "org-b"}).
					BuildProviderConfig(),
			}).
			BuildSettings()
		opts := commands.RunOptions{OrgOverride: "org-b"}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"org-b"}, spy.DiscoveredOrgs)
	})

	t.Run("should process multiple updaters for same repository", func(t *testing.T) {
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
			WithPRs([]entities.PullRequest{{ID: 1, Title: "TF Update", URL: "https://example.com/pr/1"}}).
			BuildSpy()

		pipelineSpy := doubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("pipeline").
			WithDetectResult(true).
			WithPRs([]entities.PullRequest{{ID: 2, Title: "Pipeline Update", URL: "https://example.com/pr/2"}}).
			BuildSpy()

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return spy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()
		updaterRegistry.Register(terraformSpy)
		updaterRegistry.Register(pipelineSpy)

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
		assert.Len(t, terraformSpy.CreatePRsCalls, 1)
		assert.Len(t, pipelineSpy.CreatePRsCalls, 1)
	})

	t.Run("should handle provider creation error gracefully", func(t *testing.T) {
		// given
		providerRegistry := infraRepos.NewProviderRegistry()
		// Do NOT register any factory for "github", so Get() will fail

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
		require.NoError(t, err) // Execute returns nil even when provider fails
	})

	t.Run("should process multiple providers independently", func(t *testing.T) {
		// given
		githubSpy := doubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("github").
			WithToken("gh-token").
			WithRepositories([]entities.Repository{}).
			BuildSpy()

		gitlabSpy := doubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("gitlab").
			WithToken("gl-token").
			WithRepositories([]entities.Repository{}).
			BuildSpy()

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return githubSpy
		})
		providerRegistry.Register("gitlab", func(_ string) repositories.ProviderRepository {
			return gitlabSpy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()

		cmd := commands.NewRunCommand(providerRegistry, updaterRegistry)

		settings := entitybuilders.NewSettingsBuilder().
			WithProviders([]entities.ProviderConfig{
				entitybuilders.NewProviderConfigBuilder().
					WithType("github").
					WithToken("gh-token").
					WithOrganizations([]string{"gh-org"}).
					BuildProviderConfig(),
				entitybuilders.NewProviderConfigBuilder().
					WithType("gitlab").
					WithToken("gl-token").
					WithOrganizations([]string{"gl-org"}).
					BuildProviderConfig(),
			}).
			BuildSettings()
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"gh-org"}, githubSpy.DiscoveredOrgs)
		assert.Equal(t, []string{"gl-org"}, gitlabSpy.DiscoveredOrgs)
	})

	t.Run("should filter out forks when ExcludeForks is enabled", func(t *testing.T) {
		// given
		forkRepo := entitybuilders.NewRepositoryBuilder().
			WithID("fork-1").
			WithName("forked-repo").
			WithOrganization("test-org").
			WithDefaultBranch("refs/heads/main").
			BuildRepository()
		forkRepo.IsFork = true

		normalRepo := entitybuilders.NewRepositoryBuilder().
			WithID("normal-1").
			WithName("normal-repo").
			WithOrganization("test-org").
			WithDefaultBranch("refs/heads/main").
			BuildRepository()

		spy := doubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("github").
			WithToken("test-token").
			WithRepositories([]entities.Repository{forkRepo, normalRepo}).
			BuildSpy()

		updaterSpy := doubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("terraform").
			WithDetectResult(true).
			WithPRs([]entities.PullRequest{{ID: 1, Title: "Update", URL: "https://example.com/pr/1"}}).
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
			WithExcludeForks(true).
			BuildSettings()
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Len(t, updaterSpy.DetectedRepos, 1)
		assert.Equal(t, "normal-repo", updaterSpy.DetectedRepos[0].Name)
	})

	t.Run("should filter out archived repos when ExcludeArchived is enabled", func(t *testing.T) {
		// given
		archivedRepo := entitybuilders.NewRepositoryBuilder().
			WithID("archived-1").
			WithName("archived-repo").
			WithOrganization("test-org").
			WithDefaultBranch("refs/heads/main").
			BuildRepository()
		archivedRepo.IsArchived = true

		activeRepo := entitybuilders.NewRepositoryBuilder().
			WithID("active-1").
			WithName("active-repo").
			WithOrganization("test-org").
			WithDefaultBranch("refs/heads/main").
			BuildRepository()

		spy := doubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("github").
			WithToken("test-token").
			WithRepositories([]entities.Repository{archivedRepo, activeRepo}).
			BuildSpy()

		updaterSpy := doubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("terraform").
			WithDetectResult(true).
			WithPRs([]entities.PullRequest{{ID: 1, Title: "Update", URL: "https://example.com/pr/1"}}).
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
			WithExcludeArchived(true).
			BuildSettings()
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Len(t, updaterSpy.DetectedRepos, 1)
		assert.Equal(t, "active-repo", updaterSpy.DetectedRepos[0].Name)
	})

	t.Run("should continue processing remaining providers when one fails", func(t *testing.T) {
		// given
		gitlabSpy := doubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("gitlab").
			WithToken("gl-token").
			WithRepositories([]entities.Repository{}).
			BuildSpy()

		providerRegistry := infraRepos.NewProviderRegistry()
		// Do NOT register "github" so it will fail
		providerRegistry.Register("gitlab", func(_ string) repositories.ProviderRepository {
			return gitlabSpy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()

		cmd := commands.NewRunCommand(providerRegistry, updaterRegistry)

		settings := entitybuilders.NewSettingsBuilder().
			WithProviders([]entities.ProviderConfig{
				entitybuilders.NewProviderConfigBuilder().
					WithType("github").
					WithToken("gh-token").
					WithOrganizations([]string{"gh-org"}).
					BuildProviderConfig(),
				entitybuilders.NewProviderConfigBuilder().
					WithType("gitlab").
					WithToken("gl-token").
					WithOrganizations([]string{"gl-org"}).
					BuildProviderConfig(),
			}).
			BuildSettings()
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"gl-org"}, gitlabSpy.DiscoveredOrgs)
	})

	t.Run("should process multiple orgs in single provider", func(t *testing.T) {
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
					WithOrganizations([]string{"org-a", "org-b", "org-c"}).
					BuildProviderConfig(),
			}).
			BuildSettings()
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"org-a", "org-b", "org-c"}, spy.DiscoveredOrgs)
	})

	t.Run("should handle updater that returns error alongside a successful updater", func(t *testing.T) {
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

		failingSpy := doubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("terraform").
			WithDetectResult(true).
			WithCreatePRsErr(errors.New("terraform API error")).
			BuildSpy()

		succeedingSpy := doubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("pipeline").
			WithDetectResult(true).
			WithPRs([]entities.PullRequest{{ID: 5, Title: "Pipeline PR", URL: "https://example.com/pr/5"}}).
			BuildSpy()

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return spy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()
		updaterRegistry.Register(failingSpy)
		updaterRegistry.Register(succeedingSpy)

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
		assert.Len(t, failingSpy.CreatePRsCalls, 1)
		assert.Len(t, succeedingSpy.CreatePRsCalls, 1)
	})

	t.Run("should process multiple repos each with detected updaters", func(t *testing.T) {
		// given
		repo1 := entitybuilders.NewRepositoryBuilder().
			WithID("repo-1").
			WithName("repo-alpha").
			WithOrganization("test-org").
			WithDefaultBranch("refs/heads/main").
			BuildRepository()
		repo2 := entitybuilders.NewRepositoryBuilder().
			WithID("repo-2").
			WithName("repo-beta").
			WithOrganization("test-org").
			WithDefaultBranch("refs/heads/main").
			BuildRepository()

		spy := doubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("github").
			WithToken("test-token").
			WithRepositories([]entities.Repository{repo1, repo2}).
			BuildSpy()

		updaterSpy := doubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("terraform").
			WithDetectResult(true).
			WithPRs([]entities.PullRequest{{ID: 1, Title: "Update", URL: "https://example.com/pr/1"}}).
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
		assert.Len(t, updaterSpy.DetectedRepos, 2)
		assert.Equal(t, "repo-alpha", updaterSpy.DetectedRepos[0].Name)
		assert.Equal(t, "repo-beta", updaterSpy.DetectedRepos[1].Name)
		assert.Len(t, updaterSpy.CreatePRsCalls, 2)
	})

	t.Run("should skip repos with no detected updaters", func(t *testing.T) {
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
			WithDetectResult(false). // no detection
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
		assert.Empty(t, updaterSpy.CreatePRsCalls)
	})

	t.Run("should handle empty repositories list from provider", func(t *testing.T) {
		// given
		spy := doubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("github").
			WithToken("test-token").
			WithRepositories([]entities.Repository{}).
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
			BuildSettings()
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Contains(t, spy.DiscoveredOrgs, "test-org")
		assert.Empty(t, updaterSpy.DetectedRepos)
		assert.Empty(t, updaterSpy.CreatePRsCalls)
	})

	t.Run("should filter both forks and archived repos together", func(t *testing.T) {
		// given
		forkRepo := entitybuilders.NewRepositoryBuilder().
			WithID("fork-1").
			WithName("forked-repo").
			WithOrganization("test-org").
			WithDefaultBranch("refs/heads/main").
			BuildRepository()
		forkRepo.IsFork = true

		archivedRepo := entitybuilders.NewRepositoryBuilder().
			WithID("archived-1").
			WithName("archived-repo").
			WithOrganization("test-org").
			WithDefaultBranch("refs/heads/main").
			BuildRepository()
		archivedRepo.IsArchived = true

		normalRepo := entitybuilders.NewRepositoryBuilder().
			WithID("normal-1").
			WithName("normal-repo").
			WithOrganization("test-org").
			WithDefaultBranch("refs/heads/main").
			BuildRepository()

		spy := doubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("github").
			WithToken("test-token").
			WithRepositories([]entities.Repository{forkRepo, archivedRepo, normalRepo}).
			BuildSpy()

		updaterSpy := doubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("terraform").
			WithDetectResult(true).
			WithPRs([]entities.PullRequest{{ID: 1, Title: "Update", URL: "https://example.com/pr/1"}}).
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
			WithExcludeForks(true).
			WithExcludeArchived(true).
			BuildSettings()
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Len(t, updaterSpy.DetectedRepos, 1)
		assert.Equal(t, "normal-repo", updaterSpy.DetectedRepos[0].Name)
	})

	t.Run("should handle updater that returns empty PR list", func(t *testing.T) {
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
			WithPRs([]entities.PullRequest{}).
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
		assert.Len(t, updaterSpy.CreatePRsCalls, 1)
	})

	t.Run("should combine ProviderName and OrgOverride filters", func(t *testing.T) {
		// given
		githubSpy := doubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("github").
			WithToken("gh-token").
			WithRepositories([]entities.Repository{}).
			BuildSpy()

		gitlabSpy := doubles.NewSpyProviderRepositoryBuilder().
			WithProviderName("gitlab").
			WithToken("gl-token").
			WithRepositories([]entities.Repository{}).
			BuildSpy()

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return githubSpy
		})
		providerRegistry.Register("gitlab", func(_ string) repositories.ProviderRepository {
			return gitlabSpy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()

		cmd := commands.NewRunCommand(providerRegistry, updaterRegistry)

		settings := entitybuilders.NewSettingsBuilder().
			WithProviders([]entities.ProviderConfig{
				entitybuilders.NewProviderConfigBuilder().
					WithType("github").
					WithToken("gh-token").
					WithOrganizations([]string{"gh-org-a", "gh-org-b"}).
					BuildProviderConfig(),
				entitybuilders.NewProviderConfigBuilder().
					WithType("gitlab").
					WithToken("gl-token").
					WithOrganizations([]string{"gl-org"}).
					BuildProviderConfig(),
			}).
			BuildSettings()
		opts := commands.RunOptions{
			ProviderName: "github",
			OrgOverride:  "gh-org-b",
		}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"gh-org-b"}, githubSpy.DiscoveredOrgs)
		assert.Empty(t, gitlabSpy.DiscoveredOrgs) // gitlab was filtered out
	})

	t.Run("should combine UpdaterName filter with multiple updaters", func(t *testing.T) {
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
			WithPRs([]entities.PullRequest{{ID: 1, Title: "TF", URL: "https://example.com/pr/1"}}).
			BuildSpy()

		pipelineSpy := doubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("pipeline").
			WithDetectResult(true).
			WithPRs([]entities.PullRequest{{ID: 2, Title: "Pipeline", URL: "https://example.com/pr/2"}}).
			BuildSpy()

		golangSpy := doubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("golang").
			WithDetectResult(true).
			WithPRs([]entities.PullRequest{{ID: 3, Title: "Go", URL: "https://example.com/pr/3"}}).
			BuildSpy()

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return spy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()
		updaterRegistry.Register(terraformSpy)
		updaterRegistry.Register(pipelineSpy)
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
		opts := commands.RunOptions{UpdaterName: "pipeline"}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Empty(t, terraformSpy.DetectedRepos)
		assert.Len(t, pipelineSpy.DetectedRepos, 1)
		assert.Len(t, pipelineSpy.CreatePRsCalls, 1)
		assert.Empty(t, golangSpy.DetectedRepos)
	})

	t.Run("should process empty settings with no providers", func(t *testing.T) {
		// given
		providerRegistry := infraRepos.NewProviderRegistry()
		updaterRegistry := infraRepos.NewUpdaterRegistry()

		cmd := commands.NewRunCommand(providerRegistry, updaterRegistry)

		settings := entitybuilders.NewSettingsBuilder().
			WithProviders([]entities.ProviderConfig{}).
			BuildSettings()
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
	})
}

func TestFilterRepositories(t *testing.T) {
	t.Parallel()

	t.Run("should return all repos when no exclusions are set", func(t *testing.T) {
		t.Parallel()

		// given
		repos := []entities.Repository{
			{Name: "repo1", IsFork: true},
			{Name: "repo2", IsArchived: true},
			{Name: "repo3"},
		}
		settings := &entities.Settings{}

		// when
		result := commands.FilterRepositories(repos, settings)

		// then
		assert.Len(t, result, 3)
	})

	t.Run("should exclude forks when ExcludeForks is true", func(t *testing.T) {
		t.Parallel()

		// given
		repos := []entities.Repository{
			{Name: "fork-repo", IsFork: true},
			{Name: "regular-repo"},
		}
		settings := &entities.Settings{ExcludeForks: true}

		// when
		result := commands.FilterRepositories(repos, settings)

		// then
		assert.Len(t, result, 1)
		assert.Equal(t, "regular-repo", result[0].Name)
	})

	t.Run("should exclude archived repos when ExcludeArchived is true", func(t *testing.T) {
		t.Parallel()

		// given
		repos := []entities.Repository{
			{Name: "archived-repo", IsArchived: true},
			{Name: "active-repo"},
		}
		settings := &entities.Settings{ExcludeArchived: true}

		// when
		result := commands.FilterRepositories(repos, settings)

		// then
		assert.Len(t, result, 1)
		assert.Equal(t, "active-repo", result[0].Name)
	})

	t.Run("should exclude both forks and archived repos", func(t *testing.T) {
		t.Parallel()

		// given
		repos := []entities.Repository{
			{Name: "fork", IsFork: true},
			{Name: "archived", IsArchived: true},
			{Name: "both", IsFork: true, IsArchived: true},
			{Name: "normal"},
		}
		settings := &entities.Settings{ExcludeForks: true, ExcludeArchived: true}

		// when
		result := commands.FilterRepositories(repos, settings)

		// then
		assert.Len(t, result, 1)
		assert.Equal(t, "normal", result[0].Name)
	})

	t.Run("should return empty list when all repos are excluded", func(t *testing.T) {
		t.Parallel()

		// given
		repos := []entities.Repository{
			{Name: "fork", IsFork: true},
		}
		settings := &entities.Settings{ExcludeForks: true}

		// when
		result := commands.FilterRepositories(repos, settings)

		// then
		assert.Empty(t, result)
	})
}
