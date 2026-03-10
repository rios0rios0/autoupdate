//go:build unit

package gitlocal_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlocal"
)

func TestNewLocalGitContext(t *testing.T) {
	t.Parallel()

	t.Run("should open a valid git repository", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)

		// when
		ctx, err := gitlocal.NewLocalGitContext(repoDir, nil)

		// then
		require.NoError(t, err)
		assert.NotNil(t, ctx)
	})

	t.Run("should return error for non-repository directory", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()

		// when
		ctx, err := gitlocal.NewLocalGitContext(tmpDir, nil)

		// then
		require.Error(t, err)
		assert.Nil(t, ctx)
		assert.Contains(t, err.Error(), "failed to open repository")
	})
}

func TestEnsureClean(t *testing.T) {
	t.Parallel()

	t.Run("should succeed when worktree is clean", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		ctx, err := gitlocal.NewLocalGitContext(repoDir, nil)
		require.NoError(t, err)

		// when
		err = ctx.EnsureClean()

		// then
		require.NoError(t, err)
	})

	t.Run("should return error when worktree has uncommitted changes", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "dirty.txt"), []byte("dirty"), 0o600))
		ctx, err := gitlocal.NewLocalGitContext(repoDir, nil)
		require.NoError(t, err)

		// when
		err = ctx.EnsureClean()

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "uncommitted changes")
	})
}

func TestCreateBranch(t *testing.T) {
	t.Parallel()

	t.Run("should create and switch to new branch", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		ctx, err := gitlocal.NewLocalGitContext(repoDir, nil)
		require.NoError(t, err)

		// when
		err = ctx.CreateBranch("chore/test-branch")

		// then
		require.NoError(t, err)
		repo, openErr := git.PlainOpen(repoDir)
		require.NoError(t, openErr)
		head, headErr := repo.Head()
		require.NoError(t, headErr)
		assert.Equal(t, "refs/heads/chore/test-branch", head.Name().String())
	})
}

func TestHasChanges(t *testing.T) {
	t.Parallel()

	t.Run("should return false when worktree is clean", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		ctx, err := gitlocal.NewLocalGitContext(repoDir, nil)
		require.NoError(t, err)

		// when
		hasChanges, err := ctx.HasChanges()

		// then
		require.NoError(t, err)
		assert.False(t, hasChanges)
	})

	t.Run("should return true when worktree has new files", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "new.txt"), []byte("new"), 0o600))
		ctx, err := gitlocal.NewLocalGitContext(repoDir, nil)
		require.NoError(t, err)

		// when
		hasChanges, err := ctx.HasChanges()

		// then
		require.NoError(t, err)
		assert.True(t, hasChanges)
	})
}

func TestStageCommitAndPush(t *testing.T) {
	t.Parallel()

	t.Run("should return false when no changes exist", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		ctx, err := gitlocal.NewLocalGitContext(repoDir, nil)
		require.NoError(t, err)

		// when
		pushed, err := ctx.StageCommitAndPush("main", "test commit", "fake-token")

		// then
		require.NoError(t, err)
		assert.False(t, pushed)
	})

	t.Run("should stage and commit changes locally", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		ctx, err := gitlocal.NewLocalGitContext(repoDir, nil)
		require.NoError(t, err)
		require.NoError(t, ctx.CreateBranch("chore/test-branch"))
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "update.txt"), []byte("data"), 0o600))

		// when
		// Push will fail because there is no remote, but we verify
		// that staging and committing succeed by checking the error
		// message references the push step, not stage/commit.
		_, err = ctx.StageCommitAndPush("chore/test-branch", "chore(deps): test commit", "fake-token")

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to push")

		// Verify the commit was created despite push failure
		repo, openErr := git.PlainOpen(repoDir)
		require.NoError(t, openErr)
		head, headErr := repo.Head()
		require.NoError(t, headErr)
		commit, commitErr := repo.CommitObject(head.Hash())
		require.NoError(t, commitErr)
		assert.Contains(t, commit.Message, "chore(deps): test commit")
	})

	t.Run("should return error when HTTPS push has nil resolver", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithHTTPSRemote(t)
		ctx, err := gitlocal.NewLocalGitContext(repoDir, nil)
		require.NoError(t, err)
		require.NoError(t, ctx.CreateBranch("chore/test-branch"))
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "file.txt"), []byte("x"), 0o600))

		// when
		_, err = ctx.StageCommitAndPush("chore/test-branch", "msg", "token")

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "push auth resolver is required")
	})
}

// --- test helpers ---

func createTestRepoWithHTTPSRemote(t *testing.T) string {
	t.Helper()

	repoDir := createTestRepoWithCommit(t)

	repo, err := git.PlainOpen(repoDir)
	require.NoError(t, err)

	_, err = repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://github.com/test/test-repo.git"},
	})
	require.NoError(t, err)

	return repoDir
}

func createTestRepoWithCommit(t *testing.T) string {
	t.Helper()

	repoDir := t.TempDir()

	repo, err := git.PlainInit(repoDir, false)
	require.NoError(t, err)

	// Configure user identity so that commits via empty CommitOptions
	// (like the ones gitforge's CommitChanges creates) succeed even
	// when no global git config exists (e.g. in CI runners).
	cfg, err := repo.Config()
	require.NoError(t, err)
	cfg.User.Name = "test"
	cfg.User.Email = "test@test.com"
	cfg.Raw.Section("commit").SetOption("gpgsign", "false")
	require.NoError(t, repo.SetConfig(cfg))

	wt, err := repo.Worktree()
	require.NoError(t, err)

	readmePath := filepath.Join(repoDir, "README.md")
	require.NoError(t, os.WriteFile(readmePath, []byte("# Test"), 0o600))

	_, err = wt.Add("README.md")
	require.NoError(t, err)

	_, err = wt.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "test",
			Email: "test@test.com",
			When:  time.Now(),
		},
	})
	require.NoError(t, err)

	return repoDir
}
