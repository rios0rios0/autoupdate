package support_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

// remoteUpgradeSpy records which of the run's callbacks were invoked.
type remoteUpgradeSpy struct {
	dryRuns  int
	upgrades int
	result   support.RemoteUpgradeResult
	err      error
}

func (s *remoteUpgradeSpy) run() support.RemoteUpgrade {
	return support.RemoteUpgrade{
		LogPrefix:  "python",
		BranchName: "chore/upgrade-python-deps",
		DryRun:     func() { s.dryRuns++ },
		Upgrade: func(_ context.Context) (support.RemoteUpgradeResult, error) {
			s.upgrades++
			return s.result, s.err
		},
	}
}

func TestRunRemoteUpgrade(t *testing.T) {
	t.Parallel()

	repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
	pushed := support.RemoteUpgradeResult{
		Pushed: true,
		PullRequest: support.PullRequestSpec{
			LogPrefix:   "python",
			BranchName:  "chore/upgrade-python-deps",
			Title:       "chore(deps): updated Python dependencies",
			Description: "## Summary",
		},
	}

	t.Run("should skip the run when a pull request is already open for the branch", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().WithPRExistsResult(true).BuildSpy()
		spy := &remoteUpgradeSpy{result: pushed}

		// when
		prs, err := support.RunRemoteUpgrade(t.Context(), provider, repo, entities.UpdateOptions{}, spy.run())

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
		assert.Equal(t, []string{"chore/upgrade-python-deps"}, provider.PRExistsBranches)
		assert.Equal(t, 0, spy.upgrades)
		assert.Empty(t, provider.PRInputs)
	})

	t.Run("should still run when the pull request check fails", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithPRExistsErr(errors.New("api down")).
			BuildSpy()
		spy := &remoteUpgradeSpy{result: pushed}

		// when
		prs, err := support.RunRemoteUpgrade(t.Context(), provider, repo, entities.UpdateOptions{}, spy.run())

		// then
		require.NoError(t, err)
		assert.Len(t, prs, 1)
		assert.Equal(t, 1, spy.upgrades)
	})

	t.Run("should only log the plan when the run is a dry run", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy()
		spy := &remoteUpgradeSpy{result: pushed}

		// when
		prs, err := support.RunRemoteUpgrade(
			t.Context(),
			provider,
			repo,
			entities.UpdateOptions{DryRun: true},
			spy.run(),
		)

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
		assert.Equal(t, 1, spy.dryRuns)
		assert.Equal(t, 0, spy.upgrades)
		assert.Empty(t, provider.PRInputs)
	})

	t.Run("should open the pull request when the upgrade pushed a branch", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithCreatedPR(&entities.PullRequest{ID: 9, URL: "https://example.com/pr/9"}).
			BuildSpy()
		spy := &remoteUpgradeSpy{result: pushed}

		// when
		prs, err := support.RunRemoteUpgrade(t.Context(), provider, repo, entities.UpdateOptions{}, spy.run())

		// then
		require.NoError(t, err)
		require.Len(t, prs, 1)
		assert.Equal(t, 9, prs[0].ID)
		require.Len(t, provider.PRInputs, 1)
		assert.Equal(t, "chore(deps): updated Python dependencies", provider.PRInputs[0].Title)
		assert.Equal(t, "refs/heads/chore/upgrade-python-deps", provider.PRInputs[0].SourceBranch)
	})

	t.Run("should open no pull request when the upgrade found nothing to change", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy()
		spy := &remoteUpgradeSpy{result: support.RemoteUpgradeResult{Pushed: false}}

		// when
		prs, err := support.RunRemoteUpgrade(t.Context(), provider, repo, entities.UpdateOptions{}, spy.run())

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
		assert.Empty(t, provider.PRInputs)
	})

	t.Run("should report the upgrade failure when the upgrade fails", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy()
		spy := &remoteUpgradeSpy{err: errors.New("failed to upgrade: clone refused")}

		// when
		prs, err := support.RunRemoteUpgrade(t.Context(), provider, repo, entities.UpdateOptions{}, spy.run())

		// then
		require.Error(t, err)
		assert.Nil(t, prs)
		assert.Contains(t, err.Error(), "clone refused")
		assert.Empty(t, provider.PRInputs)
	})
}

// remoteUpgradeRunSpy records the clone target the run handed to the upgrade.
type remoteUpgradeRunSpy struct {
	dryRuns int
	targets []support.CloneTarget
	outcome support.UpgradeOutcome
	err     error
}

func (s *remoteUpgradeRunSpy) run(changelog []string) support.RemoteUpgradeRun {
	return support.RemoteUpgradeRun{
		LogPrefix:  "python",
		BranchName: "chore/upgrade-python-deps",
		DryRun:     func() { s.dryRuns++ },
		Changelog:  changelog,
		Upgrade: func(_ context.Context, target support.CloneTarget) (support.UpgradeOutcome, error) {
			s.targets = append(s.targets, target)
			return s.outcome, s.err
		},
	}
}

func TestRunRemoteUpgradeRun(t *testing.T) {
	t.Parallel()

	repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
	entries := []string{"- changed the Python dependencies to their latest versions"}
	pushed := support.UpgradeOutcome{
		Pushed:      true,
		Title:       "chore(deps): updated Python dependencies",
		Description: "## Summary",
	}

	changelogProvider := func() *repositorydoubles.SpyProviderRepository {
		return repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithToken("secret").
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": "# Changelog\n\n## [Unreleased]\n"}).
			WithCreatedPR(&entities.PullRequest{ID: 7}).
			BuildSpy()
	}

	t.Run("should hand the upgrade the clone target carrying the staged changelog", func(t *testing.T) {
		t.Parallel()

		// given
		provider := changelogProvider()
		spy := &remoteUpgradeRunSpy{outcome: pushed}

		// when
		prs, err := support.RunRemoteUpgradeRun(
			t.Context(), provider, repo, entities.UpdateOptions{}, spy.run(entries),
		)

		// then
		require.NoError(t, err)
		assert.Len(t, prs, 1)
		require.Len(t, spy.targets, 1)
		target := spy.targets[0]
		assert.Equal(t, "chore/upgrade-python-deps", target.BranchName)
		assert.Equal(t, "main", target.DefaultBranch)
		assert.Equal(t, "secret", target.AuthToken)
		assert.Equal(t, "CHANGELOG.md", target.Changelog.RepoPath)
	})

	t.Run("should remove the staged changelog when the run is over", func(t *testing.T) {
		t.Parallel()

		// given
		provider := changelogProvider()
		spy := &remoteUpgradeRunSpy{outcome: pushed}

		// when
		_, err := support.RunRemoteUpgradeRun(
			t.Context(), provider, repo, entities.UpdateOptions{}, spy.run(entries),
		)

		// then
		require.NoError(t, err)
		require.Len(t, spy.targets, 1)
		_, statErr := os.Stat(spy.targets[0].Changelog.TempPath)
		assert.True(t, os.IsNotExist(statErr), "the staged changelog outlived the run")
	})

	t.Run("should describe the pull request from the outcome when the upgrade pushed", func(t *testing.T) {
		t.Parallel()

		// given
		provider := changelogProvider()
		spy := &remoteUpgradeRunSpy{outcome: pushed}

		// when
		prs, err := support.RunRemoteUpgradeRun(
			t.Context(), provider, repo, entities.UpdateOptions{}, spy.run(entries),
		)

		// then
		require.NoError(t, err)
		require.Len(t, prs, 1)
		assert.Equal(t, 7, prs[0].ID)
		require.Len(t, provider.PRInputs, 1)
		assert.Equal(t, pushed.Title, provider.PRInputs[0].Title)
		assert.Equal(t, pushed.Description, provider.PRInputs[0].Description)
		assert.Equal(t, "refs/heads/chore/upgrade-python-deps", provider.PRInputs[0].SourceBranch)
	})

	t.Run("should stage nothing and skip the upgrade when the run is a dry run", func(t *testing.T) {
		t.Parallel()

		// given
		provider := changelogProvider()
		spy := &remoteUpgradeRunSpy{outcome: pushed}

		// when
		prs, err := support.RunRemoteUpgradeRun(
			t.Context(), provider, repo, entities.UpdateOptions{DryRun: true}, spy.run(entries),
		)

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
		assert.Equal(t, 1, spy.dryRuns)
		assert.Empty(t, spy.targets)
		assert.Empty(t, provider.PRInputs)
	})

	t.Run("should open no pull request when the upgrade found nothing to change", func(t *testing.T) {
		t.Parallel()

		// given
		provider := changelogProvider()
		spy := &remoteUpgradeRunSpy{outcome: support.UpgradeOutcome{Pushed: false}}

		// when
		prs, err := support.RunRemoteUpgradeRun(
			t.Context(), provider, repo, entities.UpdateOptions{}, spy.run(entries),
		)

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
		assert.Empty(t, provider.PRInputs)
	})

	t.Run("should report the failure when the upgrade fails", func(t *testing.T) {
		t.Parallel()

		// given
		provider := changelogProvider()
		spy := &remoteUpgradeRunSpy{err: errors.New("clone refused")}

		// when
		prs, err := support.RunRemoteUpgradeRun(
			t.Context(), provider, repo, entities.UpdateOptions{}, spy.run(entries),
		)

		// then
		require.Error(t, err)
		assert.Nil(t, prs)
		assert.Contains(t, err.Error(), "clone refused")
		assert.Empty(t, provider.PRInputs)
	})
}

func TestLogRemoteDryRun(t *testing.T) {
	t.Parallel()

	repo := entities.Repository{Organization: "org", Name: "repo"}

	t.Run("should not panic when the version would move", func(t *testing.T) {
		t.Parallel()

		// given
		plan := support.DryRunPlan{Runtime: "Python", Version: "3.13.1", UpgradesVersion: true}

		// when / then
		assert.NotPanics(t, func() { support.LogRemoteDryRun("python", repo, plan) })
	})

	t.Run("should not panic when only the dependencies would move", func(t *testing.T) {
		t.Parallel()

		// given
		plan := support.DryRunPlan{Dependencies: "Python dependencies"}

		// when / then
		assert.NotPanics(t, func() { support.LogRemoteDryRun("python", repo, plan) })
	})
}
