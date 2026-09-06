package support_test

import (
	"context"
	"errors"
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
