package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/domain"
	testdoubles "github.com/rios0rios0/autoupdate/test"
)

func TestInterfaceCompliance(t *testing.T) {
	t.Parallel()

	t.Run("should satisfy Provider interface with a dummy", func(t *testing.T) {
		t.Parallel()

		// given
		var provider domain.Provider = &testdoubles.DummyProvider{}

		// then
		assert.NotNil(t, provider)
		assert.Implements(t, (*domain.Provider)(nil), provider)
	})

	t.Run("should satisfy Updater interface with a dummy", func(t *testing.T) {
		t.Parallel()

		// given
		var updater domain.Updater = &testdoubles.DummyUpdater{}

		// then
		assert.NotNil(t, updater)
		assert.Implements(t, (*domain.Updater)(nil), updater)
	})

	t.Run("should satisfy Provider interface with a spy", func(t *testing.T) {
		t.Parallel()

		// given
		var provider domain.Provider = &testdoubles.SpyProvider{ProviderName: "github", Token: "tok"}

		// then
		assert.NotNil(t, provider)
		assert.Equal(t, "github", provider.Name())
		assert.Equal(t, "tok", provider.AuthToken())
	})

	t.Run("should satisfy Updater interface with a spy", func(t *testing.T) {
		t.Parallel()

		// given
		var updater domain.Updater = &testdoubles.SpyUpdater{UpdaterName: "terraform"}

		// then
		assert.NotNil(t, updater)
		assert.Equal(t, "terraform", updater.Name())
	})
}

func TestModels(t *testing.T) {
	t.Parallel()

	t.Run("should create Repository with all fields", func(t *testing.T) {
		t.Parallel()

		// given / when
		repo := domain.Repository{
			ID:            "123",
			Name:          "my-repo",
			Organization:  "my-org",
			Project:       "my-project",
			DefaultBranch: "refs/heads/main",
			RemoteURL:     "https://github.com/my-org/my-repo.git",
			SSHURL:        "git@github.com:my-org/my-repo.git",
			ProviderName:  "github",
		}

		// then
		assert.Equal(t, "123", repo.ID)
		assert.Equal(t, "my-repo", repo.Name)
		assert.Equal(t, "my-org", repo.Organization)
		assert.Equal(t, "my-project", repo.Project)
		assert.Equal(t, "refs/heads/main", repo.DefaultBranch)
		assert.Equal(t, "https://github.com/my-org/my-repo.git", repo.RemoteURL)
		assert.Equal(t, "git@github.com:my-org/my-repo.git", repo.SSHURL)
		assert.Equal(t, "github", repo.ProviderName)
	})

	t.Run("should create Dependency with version info", func(t *testing.T) {
		t.Parallel()

		// given / when
		dep := domain.Dependency{
			Name:       "networking",
			Source:     "git::https://github.com/org/terraform-module-networking",
			CurrentVer: "v1.0.0",
			LatestVer:  "v2.0.0",
			FilePath:   "/main.tf",
			Line:       10,
		}

		// then
		assert.Equal(t, "networking", dep.Name)
		assert.Equal(t, "git::https://github.com/org/terraform-module-networking", dep.Source)
		assert.Equal(t, "v1.0.0", dep.CurrentVer)
		assert.Equal(t, "v2.0.0", dep.LatestVer)
		assert.Equal(t, "/main.tf", dep.FilePath)
		assert.Equal(t, 10, dep.Line)
	})

	t.Run("should create PullRequest with status", func(t *testing.T) {
		t.Parallel()

		// given / when
		pr := domain.PullRequest{
			ID:     42,
			Title:  "chore(deps): Upgrade networking to v2.0.0",
			URL:    "https://github.com/org/repo/pull/42",
			Status: "open",
		}

		// then
		assert.Equal(t, 42, pr.ID)
		assert.Equal(t, "chore(deps): Upgrade networking to v2.0.0", pr.Title)
		assert.Equal(t, "open", pr.Status)
		assert.Contains(t, pr.URL, "/pull/42")
	})

	t.Run("should create UpdateOptions with defaults", func(t *testing.T) {
		t.Parallel()

		// given / when
		opts := domain.UpdateOptions{}

		// then
		assert.False(t, opts.DryRun)
		assert.False(t, opts.Verbose)
		assert.False(t, opts.AutoComplete)
		assert.Empty(t, opts.TargetBranch)
	})

	t.Run("should create FileChange for edit", func(t *testing.T) {
		t.Parallel()

		// given / when
		change := domain.FileChange{
			Path:       "/main.tf",
			Content:    "updated content",
			ChangeType: "edit",
		}

		// then
		assert.Equal(t, "/main.tf", change.Path)
		assert.Equal(t, "updated content", change.Content)
		assert.Equal(t, "edit", change.ChangeType)
	})

	t.Run("should create BranchInput with changes", func(t *testing.T) {
		t.Parallel()

		// given
		changes := []domain.FileChange{
			{Path: "/a.tf", Content: "a", ChangeType: "edit"},
			{Path: "/b.tf", Content: "b", ChangeType: "edit"},
		}

		// when
		input := domain.BranchInput{
			BranchName:    "upgrade/batch",
			BaseBranch:    "refs/heads/main",
			Changes:       changes,
			CommitMessage: "chore(deps): batch upgrade",
		}

		// then
		assert.Equal(t, "upgrade/batch", input.BranchName)
		assert.Equal(t, "refs/heads/main", input.BaseBranch)
		assert.Equal(t, "chore(deps): batch upgrade", input.CommitMessage)
		assert.Len(t, input.Changes, 2)
	})

	t.Run("should create PullRequestInput with auto-complete", func(t *testing.T) {
		t.Parallel()

		// given / when
		input := domain.PullRequestInput{
			SourceBranch: "refs/heads/upgrade/v2",
			TargetBranch: "refs/heads/main",
			Title:        "Upgrade deps",
			Description:  "Automated upgrade",
			AutoComplete: true,
		}

		// then
		assert.True(t, input.AutoComplete)
		assert.Equal(t, "refs/heads/upgrade/v2", input.SourceBranch)
		assert.Equal(t, "refs/heads/main", input.TargetBranch)
		assert.Equal(t, "Upgrade deps", input.Title)
		assert.Equal(t, "Automated upgrade", input.Description)
	})
}
