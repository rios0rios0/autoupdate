package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/application"
	"github.com/rios0rios0/autoupdate/config"
	"github.com/rios0rios0/autoupdate/domain"
	providerPkg "github.com/rios0rios0/autoupdate/infrastructure/provider"
	updaterPkg "github.com/rios0rios0/autoupdate/infrastructure/updater"
	testdoubles "github.com/rios0rios0/autoupdate/test"
)

// --- helpers ---

func buildTestConfig(
	orgs []string,
	updaters map[string]config.UpdaterConfig,
) *config.Config {
	return &config.Config{
		Providers: []config.ProviderConfig{
			{Type: "github", Token: "tok", Organizations: orgs},
		},
		Updaters: updaters,
	}
}

func buildRegistries(
	provFactory providerPkg.Factory,
	updaterInstances ...domain.Updater,
) (*providerPkg.Registry, *updaterPkg.Registry) {
	provReg := providerPkg.NewRegistry()
	provReg.Register("github", provFactory)

	updReg := updaterPkg.NewRegistry()
	for _, u := range updaterInstances {
		updReg.Register(u)
	}

	return provReg, updReg
}

// --- tests ---

func TestUpdateService_Run(t *testing.T) {
	t.Parallel()

	t.Run("should discover repos and run updaters that detect the ecosystem", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()

		spyProv := &testdoubles.SpyProvider{
			ProviderName: "github",
			Token:        "tok",
			Repositories: []domain.Repository{
				{
					ID:            "1",
					Name:          "repo-a",
					Organization:  "test-org",
					DefaultBranch: "refs/heads/main",
					ProviderName:  "github",
				},
			},
		}

		spyUpdater := &testdoubles.SpyUpdater{
			UpdaterName:  "terraform",
			DetectResult: true,
			PRs: []domain.PullRequest{
				{
					ID:    42,
					Title: "chore(deps): upgrade networking to v2.0.0",
					URL:   "https://github.com/test-org/repo-a/pull/42",
				},
			},
		}

		provReg, updReg := buildRegistries(
			func(_ string) domain.Provider { return spyProv },
			spyUpdater,
		)
		svc := application.NewUpdateService(provReg, updReg)

		cfg := buildTestConfig(
			[]string{"test-org"},
			map[string]config.UpdaterConfig{
				"terraform": {Enabled: true},
			},
		)

		// when
		err := svc.Run(ctx, cfg, application.RunOptions{})

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"test-org"}, spyProv.DiscoveredOrgs)
		require.Len(t, spyUpdater.DetectedRepos, 1)
		assert.Equal(t, "repo-a", spyUpdater.DetectedRepos[0].Name)
		require.Len(t, spyUpdater.CreatePRsCalls, 1)
		assert.Equal(t, "repo-a", spyUpdater.CreatePRsCalls[0].Repo.Name)
	})

	t.Run("should skip updaters that are disabled in config", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()

		spyProv := &testdoubles.SpyProvider{
			ProviderName: "github",
			Token:        "tok",
			Repositories: []domain.Repository{
				{
					ID:           "1",
					Name:         "repo",
					Organization: "org",
					ProviderName: "github",
				},
			},
		}

		spyUpdater := &testdoubles.SpyUpdater{
			UpdaterName: "golang",
		}

		provReg, updReg := buildRegistries(
			func(_ string) domain.Provider { return spyProv },
			spyUpdater,
		)
		svc := application.NewUpdateService(provReg, updReg)

		cfg := buildTestConfig(
			[]string{"org"},
			map[string]config.UpdaterConfig{
				"golang": {Enabled: false},
			},
		)

		// when
		err := svc.Run(ctx, cfg, application.RunOptions{})

		// then
		require.NoError(t, err)
		assert.Empty(
			t, spyUpdater.DetectedRepos,
			"Detect should not be called for disabled updaters",
		)
		assert.Empty(
			t, spyUpdater.CreatePRsCalls,
			"CreateUpdatePRs should not be called for disabled updaters",
		)
	})

	t.Run("should skip repos where updater does not detect ecosystem", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()

		spyProv := &testdoubles.SpyProvider{
			ProviderName: "github",
			Token:        "tok",
			Repositories: []domain.Repository{
				{
					ID:           "1",
					Name:         "not-terraform",
					Organization: "org",
					ProviderName: "github",
				},
			},
		}

		spyUpdater := &testdoubles.SpyUpdater{
			UpdaterName:  "terraform",
			DetectResult: false, // does NOT detect the ecosystem
		}

		provReg, updReg := buildRegistries(
			func(_ string) domain.Provider { return spyProv },
			spyUpdater,
		)
		svc := application.NewUpdateService(provReg, updReg)

		cfg := buildTestConfig(
			[]string{"org"},
			map[string]config.UpdaterConfig{
				"terraform": {Enabled: true},
			},
		)

		// when
		err := svc.Run(ctx, cfg, application.RunOptions{})

		// then
		require.NoError(t, err)
		require.Len(
			t, spyUpdater.DetectedRepos, 1,
			"Detect should have been called once",
		)
		assert.Empty(
			t, spyUpdater.CreatePRsCalls,
			"CreateUpdatePRs should not be called when detection fails",
		)
	})

	t.Run("should respect provider filter from run options", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()

		spyProv := &testdoubles.SpyProvider{
			ProviderName: "github",
			Token:        "tok",
		}

		provReg, updReg := buildRegistries(
			func(_ string) domain.Provider { return spyProv },
		)
		svc := application.NewUpdateService(provReg, updReg)

		cfg := &config.Config{
			Providers: []config.ProviderConfig{
				{
					Type:          "github",
					Token:         "tok",
					Organizations: []string{"org"},
				},
			},
		}

		// when — filter for gitlab (which doesn't exist in config)
		err := svc.Run(ctx, cfg, application.RunOptions{ProviderName: "gitlab"})

		// then
		require.NoError(t, err)
		assert.Empty(
			t, spyProv.DiscoveredOrgs,
			"DiscoverRepositories should not be called when provider is filtered out",
		)
	})

	t.Run("should respect org override from run options", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()

		spyProv := &testdoubles.SpyProvider{
			ProviderName: "github",
			Token:        "tok",
			Repositories: []domain.Repository{},
		}

		provReg, updReg := buildRegistries(
			func(_ string) domain.Provider { return spyProv },
		)
		svc := application.NewUpdateService(provReg, updReg)

		cfg := &config.Config{
			Providers: []config.ProviderConfig{
				{
					Type:          "github",
					Token:         "tok",
					Organizations: []string{"skip-org", "target-org"},
				},
			},
		}

		// when
		err := svc.Run(ctx, cfg, application.RunOptions{OrgOverride: "target-org"})

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"target-org"}, spyProv.DiscoveredOrgs)
	})

	t.Run("should respect updater filter from run options", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()

		spyProv := &testdoubles.SpyProvider{
			ProviderName: "github",
			Token:        "tok",
			Repositories: []domain.Repository{
				{
					ID:           "1",
					Name:         "repo",
					Organization: "org",
					ProviderName: "github",
				},
			},
		}

		tfUpdater := &testdoubles.SpyUpdater{
			UpdaterName:  "terraform",
			DetectResult: true,
			PRs:          []domain.PullRequest{},
		}

		goUpdater := &testdoubles.SpyUpdater{
			UpdaterName: "golang",
		}

		provReg, updReg := buildRegistries(
			func(_ string) domain.Provider { return spyProv },
			tfUpdater,
			goUpdater,
		)
		svc := application.NewUpdateService(provReg, updReg)

		cfg := buildTestConfig(
			[]string{"org"},
			map[string]config.UpdaterConfig{
				"terraform": {Enabled: true},
				"golang":    {Enabled: true},
			},
		)

		// when — filter for terraform only
		err := svc.Run(ctx, cfg, application.RunOptions{UpdaterName: "terraform"})

		// then
		require.NoError(t, err)
		assert.NotEmpty(
			t, tfUpdater.DetectedRepos,
			"terraform updater should have been called",
		)
		assert.Empty(
			t, goUpdater.DetectedRepos,
			"golang updater should NOT have been called",
		)
	})

	t.Run("should continue processing on updater error", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()

		spyProv := &testdoubles.SpyProvider{
			ProviderName: "github",
			Token:        "tok",
			Repositories: []domain.Repository{
				{
					ID:           "1",
					Name:         "repo",
					Organization: "org",
					ProviderName: "github",
				},
			},
		}

		failingUpdater := &testdoubles.SpyUpdater{
			UpdaterName:  "terraform",
			DetectResult: true,
			CreatePRsErr: errors.New("API rate limit exceeded"),
		}

		provReg, updReg := buildRegistries(
			func(_ string) domain.Provider { return spyProv },
			failingUpdater,
		)
		svc := application.NewUpdateService(provReg, updReg)

		cfg := buildTestConfig(
			[]string{"org"},
			map[string]config.UpdaterConfig{
				"terraform": {Enabled: true},
			},
		)

		// when
		err := svc.Run(ctx, cfg, application.RunOptions{})

		// then — should not return error, should continue gracefully
		require.NoError(t, err)
		require.Len(
			t, failingUpdater.CreatePRsCalls, 1,
			"CreateUpdatePRs should still have been called",
		)
	})

	t.Run("should continue processing on discovery error", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()

		spyProv := &testdoubles.SpyProvider{
			ProviderName: "github",
			Token:        "tok",
			DiscoverErr:  errors.New("organization not found"),
		}

		provReg, updReg := buildRegistries(
			func(_ string) domain.Provider { return spyProv },
		)
		svc := application.NewUpdateService(provReg, updReg)

		cfg := buildTestConfig([]string{"bad-org"}, nil)

		// when
		err := svc.Run(ctx, cfg, application.RunOptions{})

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"bad-org"}, spyProv.DiscoveredOrgs)
	})

	t.Run("should pass dry-run option through to updaters", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()

		spyProv := &testdoubles.SpyProvider{
			ProviderName: "github",
			Token:        "tok",
			Repositories: []domain.Repository{
				{
					ID:           "1",
					Name:         "repo",
					Organization: "org",
					ProviderName: "github",
				},
			},
		}

		spyUpdater := &testdoubles.SpyUpdater{
			UpdaterName:  "terraform",
			DetectResult: true,
			PRs:          []domain.PullRequest{},
		}

		provReg, updReg := buildRegistries(
			func(_ string) domain.Provider { return spyProv },
			spyUpdater,
		)
		svc := application.NewUpdateService(provReg, updReg)

		cfg := buildTestConfig(
			[]string{"org"},
			map[string]config.UpdaterConfig{
				"terraform": {Enabled: true},
			},
		)

		// when
		err := svc.Run(ctx, cfg, application.RunOptions{DryRun: true})

		// then
		require.NoError(t, err)
		require.Len(t, spyUpdater.CreatePRsCalls, 1)
		assert.True(
			t, spyUpdater.CreatePRsCalls[0].Opts.DryRun,
			"DryRun option should be propagated",
		)
	})

	t.Run("should pass auto_complete and target_branch from updater config", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()

		spyProv := &testdoubles.SpyProvider{
			ProviderName: "github",
			Token:        "tok",
			Repositories: []domain.Repository{
				{
					ID:           "1",
					Name:         "repo",
					Organization: "org",
					ProviderName: "github",
				},
			},
		}

		spyUpdater := &testdoubles.SpyUpdater{
			UpdaterName:  "terraform",
			DetectResult: true,
			PRs:          []domain.PullRequest{},
		}

		provReg, updReg := buildRegistries(
			func(_ string) domain.Provider { return spyProv },
			spyUpdater,
		)
		svc := application.NewUpdateService(provReg, updReg)

		cfg := buildTestConfig(
			[]string{"org"},
			map[string]config.UpdaterConfig{
				"terraform": {
					Enabled:      true,
					AutoComplete: true,
					TargetBranch: "develop",
				},
			},
		)

		// when
		err := svc.Run(ctx, cfg, application.RunOptions{})

		// then
		require.NoError(t, err)
		require.Len(t, spyUpdater.CreatePRsCalls, 1)
		assert.True(
			t, spyUpdater.CreatePRsCalls[0].Opts.AutoComplete,
			"AutoComplete should be propagated from updater config",
		)
		assert.Equal(
			t, "develop", spyUpdater.CreatePRsCalls[0].Opts.TargetBranch,
			"TargetBranch should be propagated from updater config",
		)
	})

	t.Run("should process multiple repositories from single org", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()

		spyProv := &testdoubles.SpyProvider{
			ProviderName: "github",
			Token:        "tok",
			Repositories: []domain.Repository{
				{
					ID:           "1",
					Name:         "repo-a",
					Organization: "org",
					ProviderName: "github",
				},
				{
					ID:           "2",
					Name:         "repo-b",
					Organization: "org",
					ProviderName: "github",
				},
				{
					ID:           "3",
					Name:         "repo-c",
					Organization: "org",
					ProviderName: "github",
				},
			},
		}

		spyUpdater := &testdoubles.SpyUpdater{
			UpdaterName:  "terraform",
			DetectResult: true,
			PRs:          []domain.PullRequest{},
		}

		provReg, updReg := buildRegistries(
			func(_ string) domain.Provider { return spyProv },
			spyUpdater,
		)
		svc := application.NewUpdateService(provReg, updReg)

		cfg := buildTestConfig(
			[]string{"org"},
			map[string]config.UpdaterConfig{
				"terraform": {Enabled: true},
			},
		)

		// when
		err := svc.Run(ctx, cfg, application.RunOptions{})

		// then
		require.NoError(t, err)
		assert.Len(
			t, spyUpdater.DetectedRepos, 3,
			"Detect should be called once per repo",
		)
		assert.Len(
			t, spyUpdater.CreatePRsCalls, 3,
			"CreateUpdatePRs should be called once per repo",
		)
	})
}
