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
	doubles "github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestRunCommandExecute(t *testing.T) {
	t.Parallel()

	t.Run("should skip provider when ProviderName filter does not match", func(t *testing.T) {
		// given
		spy := &doubles.SpyProviderRepository{
			ProviderName: "github",
			Token:        "test-token",
		}

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return spy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()

		cmd := commands.NewRunCommand(providerRegistry, updaterRegistry)

		settings := &entities.Settings{
			Providers: []entities.ProviderConfig{
				{Type: "github", Token: "test-token", Organizations: []string{"test-org"}},
			},
		}
		opts := commands.RunOptions{ProviderName: "gitlab"} // filter does not match

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Empty(t, spy.DiscoveredOrgs)
	})

	t.Run("should call DiscoverRepositories for matching provider", func(t *testing.T) {
		// given
		spy := &doubles.SpyProviderRepository{
			ProviderName: "github",
			Token:        "test-token",
			Repositories: []entities.Repository{},
		}

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return spy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()

		cmd := commands.NewRunCommand(providerRegistry, updaterRegistry)

		settings := &entities.Settings{
			Providers: []entities.ProviderConfig{
				{Type: "github", Token: "test-token", Organizations: []string{"test-org"}},
			},
		}
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Contains(t, spy.DiscoveredOrgs, "test-org")
	})

	t.Run("should continue when DiscoverRepositories returns error", func(t *testing.T) {
		// given
		spy := &doubles.SpyProviderRepository{
			ProviderName: "github",
			Token:        "test-token",
			DiscoverErr:  errors.New("network error"),
		}

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return spy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()

		cmd := commands.NewRunCommand(providerRegistry, updaterRegistry)

		settings := &entities.Settings{
			Providers: []entities.ProviderConfig{
				{Type: "github", Token: "test-token", Organizations: []string{"org1", "org2"}},
			},
		}
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err) // should not fail overall
		assert.Len(t, spy.DiscoveredOrgs, 2)
	})

	t.Run("should call updater Detect and CreateUpdatePRs for discovered repos", func(t *testing.T) {
		// given
		repo := entities.Repository{
			ID:            "repo-1",
			Name:          "test-repo",
			Organization:  "test-org",
			DefaultBranch: "refs/heads/main",
		}

		spy := &doubles.SpyProviderRepository{
			ProviderName: "github",
			Token:        "test-token",
			Repositories: []entities.Repository{repo},
		}

		updaterSpy := &doubles.SpyUpdaterRepository{
			UpdaterName:  "terraform",
			DetectResult: true,
			PRs:          []entities.PullRequest{{ID: 42, Title: "Update dep", URL: "https://example.com/pr/42"}},
		}

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return spy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()
		updaterRegistry.Register(updaterSpy)

		cmd := commands.NewRunCommand(providerRegistry, updaterRegistry)

		settings := &entities.Settings{
			Providers: []entities.ProviderConfig{
				{Type: "github", Token: "test-token", Organizations: []string{"test-org"}},
			},
		}
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
		repo := entities.Repository{
			ID:            "repo-1",
			Name:          "test-repo",
			Organization:  "test-org",
			DefaultBranch: "refs/heads/main",
		}

		spy := &doubles.SpyProviderRepository{
			ProviderName: "github",
			Token:        "test-token",
			Repositories: []entities.Repository{repo},
		}

		updaterSpy := &doubles.SpyUpdaterRepository{
			UpdaterName:  "terraform",
			DetectResult: true,
		}

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return spy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()
		updaterRegistry.Register(updaterSpy)

		cmd := commands.NewRunCommand(providerRegistry, updaterRegistry)

		settings := &entities.Settings{
			Providers: []entities.ProviderConfig{
				{Type: "github", Token: "test-token", Organizations: []string{"test-org"}},
			},
			Updaters: map[string]entities.UpdaterConfig{
				"terraform": {Enabled: false},
			},
		}
		opts := commands.RunOptions{}

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Empty(t, updaterSpy.CreatePRsCalls)
	})

	t.Run("should respect UpdaterName filter", func(t *testing.T) {
		// given
		repo := entities.Repository{
			ID:            "repo-1",
			Name:          "test-repo",
			Organization:  "test-org",
			DefaultBranch: "refs/heads/main",
		}

		spy := &doubles.SpyProviderRepository{
			ProviderName: "github",
			Token:        "test-token",
			Repositories: []entities.Repository{repo},
		}

		terraformSpy := &doubles.SpyUpdaterRepository{
			UpdaterName:  "terraform",
			DetectResult: true,
		}
		golangSpy := &doubles.SpyUpdaterRepository{
			UpdaterName:  "golang",
			DetectResult: true,
		}

		providerRegistry := infraRepos.NewProviderRegistry()
		providerRegistry.Register("github", func(_ string) repositories.ProviderRepository {
			return spy
		})

		updaterRegistry := infraRepos.NewUpdaterRegistry()
		updaterRegistry.Register(terraformSpy)
		updaterRegistry.Register(golangSpy)

		cmd := commands.NewRunCommand(providerRegistry, updaterRegistry)

		settings := &entities.Settings{
			Providers: []entities.ProviderConfig{
				{Type: "github", Token: "test-token", Organizations: []string{"test-org"}},
			},
		}
		opts := commands.RunOptions{UpdaterName: "golang"} // only run golang updater

		// when
		err := cmd.Execute(context.Background(), settings, opts)

		// then
		require.NoError(t, err)
		assert.Empty(t, terraformSpy.CreatePRsCalls)
		assert.Len(t, golangSpy.DetectedRepos, 1)
	})
}
