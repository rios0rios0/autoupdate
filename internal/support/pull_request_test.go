package support_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestTargetBranchRef(t *testing.T) {
	t.Parallel()

	t.Run("should target the default branch when the run does not override it", func(t *testing.T) {
		t.Parallel()

		// given
		repo := entities.Repository{DefaultBranch: "refs/heads/main"}

		// when
		ref := support.TargetBranchRef(repo, entities.UpdateOptions{})

		// then
		assert.Equal(t, "refs/heads/main", ref)
	})

	t.Run("should target the overriding branch as a ref when the run names one", func(t *testing.T) {
		t.Parallel()

		// given
		repo := entities.Repository{DefaultBranch: "refs/heads/main"}

		// when
		ref := support.TargetBranchRef(repo, entities.UpdateOptions{TargetBranch: "develop"})

		// then
		assert.Equal(t, "refs/heads/develop", ref)
	})
}

func TestOpenPullRequest(t *testing.T) {
	t.Parallel()

	repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
	spec := support.PullRequestSpec{
		LogPrefix:   "python",
		BranchName:  "chore/upgrade-python-deps",
		Title:       "chore(deps): updated Python dependencies",
		Description: "## Summary",
	}

	t.Run("should open the pull request from the pushed branch when the provider accepts it", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithCreatedPR(&entities.PullRequest{ID: 42, URL: "https://example.com/pr/42"}).
			BuildSpy()
		opts := entities.UpdateOptions{AutoComplete: true}

		// when
		prs, err := support.OpenPullRequest(t.Context(), provider, repo, opts, spec)

		// then
		require.NoError(t, err)
		require.Len(t, prs, 1)
		assert.Equal(t, 42, prs[0].ID)
		require.Len(t, provider.PRInputs, 1)
		assert.Equal(t, "refs/heads/chore/upgrade-python-deps", provider.PRInputs[0].SourceBranch)
		assert.Equal(t, "refs/heads/main", provider.PRInputs[0].TargetBranch)
		assert.Equal(t, spec.Title, provider.PRInputs[0].Title)
		assert.Equal(t, spec.Description, provider.PRInputs[0].Description)
		assert.True(t, provider.PRInputs[0].AutoComplete)
	})

	t.Run("should target the overriding branch when the run names one", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy()
		opts := entities.UpdateOptions{TargetBranch: "develop"}

		// when
		_, err := support.OpenPullRequest(t.Context(), provider, repo, opts, spec)

		// then
		require.NoError(t, err)
		assert.Equal(t, "refs/heads/develop", provider.PRInputs[0].TargetBranch)
	})

	t.Run("should report the failure when the provider refuses the pull request", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithCreatePRErr(errors.New("api error")).
			BuildSpy()

		// when
		prs, err := support.OpenPullRequest(t.Context(), provider, repo, entities.UpdateOptions{}, spec)

		// then
		require.Error(t, err)
		assert.Nil(t, prs)
		assert.Contains(t, err.Error(), "failed to create PR")
		assert.Contains(t, err.Error(), "api error")
	})
}

func TestPushChangesAndOpenPullRequest(t *testing.T) {
	t.Parallel()

	repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
	pr := support.BranchPullRequest{
		LogPrefix:     "dockerfile",
		BranchName:    "chore/upgrade-node-22",
		Title:         "chore(deps): upgraded node to 22",
		Description:   "## Summary",
		CommitMessage: "chore(deps): upgraded node to 22",
		Changes:       []entities.FileChange{{Path: "Dockerfile", Content: "FROM node:22", ChangeType: "edit"}},
	}

	t.Run("should commit the changes on the branch and open the pull request when both succeed", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithCreatedPR(&entities.PullRequest{ID: 7, URL: "https://example.com/pr/7"}).
			BuildSpy()
		opts := entities.UpdateOptions{TargetBranch: "develop"}

		// when
		prs, err := support.PushChangesAndOpenPullRequest(t.Context(), provider, repo, opts, pr)

		// then
		require.NoError(t, err)
		require.Len(t, prs, 1)
		assert.Equal(t, 7, prs[0].ID)
		require.Len(t, provider.BranchInputs, 1)
		assert.Equal(t, "chore/upgrade-node-22", provider.BranchInputs[0].BranchName)
		assert.Equal(t, "refs/heads/develop", provider.BranchInputs[0].BaseBranch)
		assert.Equal(t, pr.Changes, provider.BranchInputs[0].Changes)
		assert.Equal(t, pr.CommitMessage, provider.BranchInputs[0].CommitMessage)
		require.Len(t, provider.PRInputs, 1)
		assert.Equal(t, "refs/heads/develop", provider.PRInputs[0].TargetBranch)
	})

	t.Run("should not open a pull request when the branch cannot be created", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithCreateBranchErr(errors.New("branch exists")).
			BuildSpy()

		// when
		prs, err := support.PushChangesAndOpenPullRequest(t.Context(), provider, repo, entities.UpdateOptions{}, pr)

		// then
		require.Error(t, err)
		assert.Nil(t, prs)
		assert.Contains(t, err.Error(), "failed to create branch")
		assert.Empty(t, provider.PRInputs)
	})
}
