//go:build unit

package commands_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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

func TestBuildAggregateBranchName(t *testing.T) {
	t.Parallel()

	t.Run("should produce a chore/autoupdate-YYYY-MM-DD branch in UTC", func(t *testing.T) {
		t.Parallel()

		// given
		loc, err := time.LoadLocation("America/Toronto")
		require.NoError(t, err)
		// 23:30 in Toronto on Apr 15 (UTC-5 in this period before DST quirks
		// — pick a date safely inside DST so the offset is deterministic).
		ts := time.Date(2026, time.July, 15, 23, 30, 0, 0, loc)

		// when
		branch := commands.BuildAggregateBranchName(ts)

		// then
		// 23:30 EDT (UTC-4) on July 15 → 03:30 UTC on July 16
		assert.Equal(t, "chore/autoupdate-2026-07-16", branch)
	})

	t.Run("should produce the same branch for two same-day calls", func(t *testing.T) {
		t.Parallel()

		// given
		morning := time.Date(2026, time.April, 15, 8, 0, 0, 0, time.UTC)
		evening := time.Date(2026, time.April, 15, 22, 0, 0, 0, time.UTC)

		// when
		branchA := commands.BuildAggregateBranchName(morning)
		branchB := commands.BuildAggregateBranchName(evening)

		// then
		assert.Equal(t, branchA, branchB)
		assert.Equal(t, "chore/autoupdate-2026-04-15", branchA)
	})
}

func TestBuildAggregateCommitMessage(t *testing.T) {
	t.Parallel()

	t.Run("should pass the single updater message through verbatim", func(t *testing.T) {
		t.Parallel()

		// given
		applied := []commands.AppliedUpdaterResult{
			commands.NewAppliedUpdaterResult("golang", &repositories.LocalUpdateResult{
				CommitMessage: "chore(deps): upgraded Go version to `1.26.2` and updated all dependencies",
			}),
		}

		// when
		msg := commands.BuildAggregateCommitMessage(applied)

		// then
		assert.Equal(t, "chore(deps): upgraded Go version to `1.26.2` and updated all dependencies", msg)
	})

	t.Run("should aggregate multi-updater messages with a bullet list of first lines", func(t *testing.T) {
		t.Parallel()

		// given
		applied := []commands.AppliedUpdaterResult{
			commands.NewAppliedUpdaterResult("golang", &repositories.LocalUpdateResult{
				CommitMessage: "chore(deps): upgraded Go version to `1.26.2` and updated all dependencies\n\nbody",
			}),
			commands.NewAppliedUpdaterResult("dockerfile", &repositories.LocalUpdateResult{
				CommitMessage: "chore(deps): upgraded `golang` from `1.26.1-alpine` to `1.26.2-alpine`",
			}),
		}

		// when
		msg := commands.BuildAggregateCommitMessage(applied)

		// then
		require.True(t, strings.HasPrefix(msg, "chore(deps): bumped dependencies via autoupdate\n\n"))
		assert.Contains(t, msg, "- [golang] chore(deps): upgraded Go version to `1.26.2` and updated all dependencies")
		assert.Contains(t, msg, "- [dockerfile] chore(deps): upgraded `golang` from `1.26.1-alpine` to `1.26.2-alpine`")
		assert.NotContains(t, msg, "body", "only the first line of each source message should be included")
	})
}

func TestBuildAggregatePRTitle(t *testing.T) {
	t.Parallel()

	t.Run("should pass the single updater PR title through verbatim", func(t *testing.T) {
		t.Parallel()

		// given
		applied := []commands.AppliedUpdaterResult{
			commands.NewAppliedUpdaterResult("golang", &repositories.LocalUpdateResult{
				PRTitle: "chore(deps): upgraded Go version to `1.26.2` and updated all dependencies",
			}),
		}

		// when
		title := commands.BuildAggregatePRTitle(applied)

		// then
		assert.Equal(t, "chore(deps): upgraded Go version to `1.26.2` and updated all dependencies", title)
	})

	t.Run("should list contributing updater names for multi-updater runs", func(t *testing.T) {
		t.Parallel()

		// given
		applied := []commands.AppliedUpdaterResult{
			commands.NewAppliedUpdaterResult("golang", &repositories.LocalUpdateResult{PRTitle: "Go bump"}),
			commands.NewAppliedUpdaterResult("dockerfile", &repositories.LocalUpdateResult{PRTitle: "Dockerfile bump"}),
		}

		// when
		title := commands.BuildAggregatePRTitle(applied)

		// then
		assert.Equal(t, "chore(deps): bumped dependencies (golang, dockerfile)", title)
	})
}

func TestBuildAggregatePRDescription(t *testing.T) {
	t.Parallel()

	t.Run("should pass the single updater PR description through verbatim", func(t *testing.T) {
		t.Parallel()

		// given
		applied := []commands.AppliedUpdaterResult{
			commands.NewAppliedUpdaterResult("golang", &repositories.LocalUpdateResult{
				PRDescription: "## Summary\n\nGo upgrade.",
			}),
		}

		// when
		desc := commands.BuildAggregatePRDescription(applied)

		// then
		assert.Equal(t, "## Summary\n\nGo upgrade.", desc)
	})

	t.Run("should render Summary, contributing list, and per-updater sections", func(t *testing.T) {
		t.Parallel()

		// given
		applied := []commands.AppliedUpdaterResult{
			commands.NewAppliedUpdaterResult("golang", &repositories.LocalUpdateResult{
				PRTitle:       "chore(deps): upgraded Go to `1.26.2`",
				PRDescription: "go body",
			}),
			commands.NewAppliedUpdaterResult("dockerfile", &repositories.LocalUpdateResult{
				PRTitle:       "chore(deps): upgraded `golang` to `1.26.2-alpine`",
				PRDescription: "dockerfile body",
			}),
		}

		// when
		desc := commands.BuildAggregatePRDescription(applied)

		// then
		assert.Contains(t, desc, "## Summary")
		assert.Contains(t, desc, "Contributing updaters:")
		assert.Contains(t, desc, "- `golang` — chore(deps): upgraded Go to `1.26.2`")
		assert.Contains(t, desc, "- `dockerfile` — chore(deps): upgraded `golang` to `1.26.2-alpine`")
		assert.Contains(t, desc, "## golang")
		assert.Contains(t, desc, "go body")
		assert.Contains(t, desc, "## dockerfile")
		assert.Contains(t, desc, "dockerfile body")
	})

	t.Run("should use a placeholder when an updater provides no description", func(t *testing.T) {
		t.Parallel()

		// given
		applied := []commands.AppliedUpdaterResult{
			commands.NewAppliedUpdaterResult("golang", &repositories.LocalUpdateResult{
				PRTitle:       "Go bump",
				PRDescription: "",
			}),
			commands.NewAppliedUpdaterResult("dockerfile", &repositories.LocalUpdateResult{
				PRTitle:       "Dockerfile bump",
				PRDescription: "real body",
			}),
		}

		// when
		desc := commands.BuildAggregatePRDescription(applied)

		// then
		assert.Contains(t, desc, "_(no description provided by updater)_")
		assert.Contains(t, desc, "real body")
	})
}

func TestAnyAutoComplete(t *testing.T) {
	t.Parallel()

	t.Run("should return false for an empty updater slice", func(t *testing.T) {
		t.Parallel()

		// given
		var updaters []commands.ApplicableUpdater

		// when
		result := commands.AnyAutoComplete(updaters)

		// then
		assert.False(t, result)
	})

	t.Run("should return false when no updater requested auto-complete", func(t *testing.T) {
		t.Parallel()

		// given
		updaters := []commands.ApplicableUpdater{
			commands.NewApplicableUpdaterForTest(
				&doubles.DummyUpdaterRepository{},
				entities.UpdateOptions{AutoComplete: false},
			),
			commands.NewApplicableUpdaterForTest(
				&doubles.DummyUpdaterRepository{},
				entities.UpdateOptions{AutoComplete: false},
			),
		}

		// when
		result := commands.AnyAutoComplete(updaters)

		// then
		assert.False(t, result)
	})

	t.Run("should return true when at least one updater requested auto-complete", func(t *testing.T) {
		t.Parallel()

		// given
		updaters := []commands.ApplicableUpdater{
			commands.NewApplicableUpdaterForTest(
				&doubles.DummyUpdaterRepository{},
				entities.UpdateOptions{AutoComplete: false},
			),
			commands.NewApplicableUpdaterForTest(
				&doubles.DummyUpdaterRepository{},
				entities.UpdateOptions{AutoComplete: true},
			),
		}

		// when
		result := commands.AnyAutoComplete(updaters)

		// then
		assert.True(t, result)
	})
}

func TestAllDryRun(t *testing.T) {
	t.Parallel()

	t.Run("should return false for an empty updater slice", func(t *testing.T) {
		t.Parallel()

		// given
		var updaters []commands.ApplicableUpdater

		// when
		result := commands.AllDryRun(updaters)

		// then
		assert.False(t, result)
	})

	t.Run("should return true when every updater is dry-run", func(t *testing.T) {
		t.Parallel()

		// given
		updaters := []commands.ApplicableUpdater{
			commands.NewApplicableUpdaterForTest(
				&doubles.DummyUpdaterRepository{}, entities.UpdateOptions{DryRun: true}),
			commands.NewApplicableUpdaterForTest(
				&doubles.DummyUpdaterRepository{}, entities.UpdateOptions{DryRun: true}),
		}

		// when
		result := commands.AllDryRun(updaters)

		// then
		assert.True(t, result)
	})

	t.Run("should return false when at least one updater is not dry-run", func(t *testing.T) {
		t.Parallel()

		// given
		updaters := []commands.ApplicableUpdater{
			commands.NewApplicableUpdaterForTest(
				&doubles.DummyUpdaterRepository{}, entities.UpdateOptions{DryRun: true}),
			commands.NewApplicableUpdaterForTest(
				&doubles.DummyUpdaterRepository{}, entities.UpdateOptions{DryRun: false}),
		}

		// when
		result := commands.AllDryRun(updaters)

		// then
		assert.False(t, result)
	})
}

func TestResolveAggregateTargetBranch(t *testing.T) {
	t.Parallel()

	t.Run("should fall back to the repo default branch when no override is set", func(t *testing.T) {
		t.Parallel()

		// given
		repo := entities.Repository{DefaultBranch: "refs/heads/main"}
		updaters := []commands.ApplicableUpdater{
			commands.NewApplicableUpdaterForTest(
				&doubles.DummyUpdaterRepository{}, entities.UpdateOptions{}),
		}

		// when
		target := commands.ResolveAggregateTargetBranch(repo, updaters)

		// then
		assert.Equal(t, "refs/heads/main", target)
	})

	t.Run("should honor the first non-empty TargetBranch override", func(t *testing.T) {
		t.Parallel()

		// given
		repo := entities.Repository{DefaultBranch: "refs/heads/main"}
		updaters := []commands.ApplicableUpdater{
			commands.NewApplicableUpdaterForTest(
				&doubles.DummyUpdaterRepository{},
				entities.UpdateOptions{TargetBranch: "develop"},
			),
		}

		// when
		target := commands.ResolveAggregateTargetBranch(repo, updaters)

		// then
		assert.Equal(t, "refs/heads/develop", target)
	})
}

func TestFirstLine(t *testing.T) {
	t.Parallel()

	t.Run("should return the first newline-delimited segment", func(t *testing.T) {
		t.Parallel()

		// given
		s := "subject line\nbody line 1\nbody line 2"

		// when
		result := commands.FirstLine(s)

		// then
		assert.Equal(t, "subject line", result)
	})

	t.Run("should return the whole string when there is no newline", func(t *testing.T) {
		t.Parallel()

		// given
		s := "single line"

		// when
		result := commands.FirstLine(s)

		// then
		assert.Equal(t, "single line", result)
	})
}
