//go:build unit

package gitlocal_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlocal"
)

func TestCreateBranchFromDefault_PreservesChangesWithStash(t *testing.T) {
	t.Parallel()

	t.Run("should lose uncommitted changes when force-checkout creates branch without stash", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		batchCtx := newBatchGitContext(t, repoDir)
		trackedFile := filepath.Join(repoDir, "README.md")
		require.NoError(t, os.WriteFile(trackedFile, []byte("upgraded content"), 0o600))

		// verify the change exists before branch creation
		content, err := os.ReadFile(trackedFile)
		require.NoError(t, err)
		assert.Equal(t, "upgraded content", string(content))

		// when - force-checkout branch creation (this is the bug scenario)
		err = batchCtx.CreateBranchFromDefault("chore/upgrade-test")
		require.NoError(t, err)

		// then - changes are LOST due to force-checkout
		content, err = os.ReadFile(trackedFile)
		require.NoError(t, err)
		assert.Equal(t, "# Test", string(content), "force-checkout should have wiped the changes")
	})

	t.Run("should preserve uncommitted changes when stash is used around branch creation", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		batchCtx := newBatchGitContext(t, repoDir)
		trackedFile := filepath.Join(repoDir, "README.md")
		require.NoError(t, os.WriteFile(trackedFile, []byte("upgraded content"), 0o600))

		// when - stash before branch, pop after (the fix)
		stashed, stashErr := batchCtx.StashChanges()
		require.NoError(t, stashErr)
		assert.True(t, stashed, "StashChanges should indicate a stash was created")
		require.NoError(t, batchCtx.CreateBranchFromDefault("chore/upgrade-test"))
		require.NoError(t, batchCtx.PopStash())

		// then - changes are preserved
		content, err := os.ReadFile(trackedFile)
		require.NoError(t, err)
		assert.Equal(t, "upgraded content", string(content), "stash/pop should preserve upgrade changes")

		// verify we're on the new branch
		repo, err := git.PlainOpen(repoDir)
		require.NoError(t, err)
		head, err := repo.Head()
		require.NoError(t, err)
		assert.Equal(t, "refs/heads/chore/upgrade-test", head.Name().String())
	})
}

func TestCleanupStaleTempDirs(t *testing.T) {
	// Not parallel: t.Setenv modifies process-wide env vars and the function
	// under test operates on the OS temp directory.

	t.Run("should remove stale autoupdate temp directories and changelog files", func(t *testing.T) {
		// given — scope os.TempDir() to a test-owned directory so we don't
		// interfere with other tests or processes on the same machine.
		tempBase := t.TempDir()
		t.Setenv("TMPDIR", tempBase)

		batchDir, err := os.MkdirTemp("", "autoupdate-batch-*")
		require.NoError(t, err)
		localDir, err := os.MkdirTemp("", "autoupdate-local-*")
		require.NoError(t, err)
		changelogFile, err := os.CreateTemp("", "autoupdate-changelog-*.md")
		require.NoError(t, err)
		_ = changelogFile.Close()

		// Backdate the paths so they exceed the stale threshold
		past := time.Now().Add(-time.Hour)
		require.NoError(t, os.Chtimes(batchDir, past, past))
		require.NoError(t, os.Chtimes(localDir, past, past))
		require.NoError(t, os.Chtimes(changelogFile.Name(), past, past))

		// when
		gitlocal.CleanupStaleTempDirs()

		// then
		_, statErr := os.Stat(batchDir)
		assert.True(t, os.IsNotExist(statErr))
		_, statErr = os.Stat(localDir)
		assert.True(t, os.IsNotExist(statErr))
		_, statErr = os.Stat(changelogFile.Name())
		assert.True(t, os.IsNotExist(statErr))
	})
}

func TestNewBatchGitContextFromLocal(t *testing.T) {
	t.Parallel()

	t.Run("should open a valid git repository", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)

		// when
		ctx, err := gitlocal.NewBatchGitContextFromLocal(repoDir, "main")

		// then
		require.NoError(t, err)
		assert.NotNil(t, ctx)
		assert.Equal(t, repoDir, ctx.RepoDir())
	})

	t.Run("should return error for non-repository directory", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()

		// when
		ctx, err := gitlocal.NewBatchGitContextFromLocal(tmpDir, "main")

		// then
		require.Error(t, err)
		assert.Nil(t, ctx)
		assert.Contains(t, err.Error(), "failed to open repository")
	})

	t.Run("should trim refs/heads/ prefix from default branch", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)

		// when
		ctx, err := gitlocal.NewBatchGitContextFromLocal(repoDir, "refs/heads/main")

		// then
		require.NoError(t, err)
		assert.NotNil(t, ctx)

		// SwitchToDefault should work, which proves the prefix was trimmed
		err = ctx.SwitchToDefault()
		require.NoError(t, err)
	})
}

func TestRepoDir(t *testing.T) {
	t.Parallel()

	t.Run("should return the expected repository path", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		ctx := newBatchGitContext(t, repoDir)

		// when
		result := ctx.RepoDir()

		// then
		assert.Equal(t, repoDir, result)
	})
}

func TestClose(t *testing.T) {
	// Not parallel: Close removes directories, and testing existence
	// immediately after is order-dependent.

	t.Run("should remove the temporary directory", func(t *testing.T) {
		// given
		repoDir := createTestRepoWithCommit(t)
		ctx := newBatchGitContext(t, repoDir)

		// verify directory exists before close
		_, err := os.Stat(repoDir)
		require.NoError(t, err)

		// when
		ctx.Close()

		// then
		_, statErr := os.Stat(repoDir)
		assert.True(t, os.IsNotExist(statErr), "directory should have been removed")
	})

	t.Run("should not panic when tmpDir is empty", func(t *testing.T) {
		// given - a context with empty tmpDir (edge case)
		repoDir := createTestRepoWithCommit(t)
		ctx := newBatchGitContext(t, repoDir)
		// Close once to clear the directory
		ctx.Close()

		// when / then - calling Close again should not panic
		assert.NotPanics(t, func() {
			ctx.Close()
		})
	})
}

func TestBatchHasChanges(t *testing.T) {
	t.Parallel()

	t.Run("should return false when worktree is clean", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		ctx := newBatchGitContext(t, repoDir)

		// when
		hasChanges, err := ctx.HasChanges()

		// then
		require.NoError(t, err)
		assert.False(t, hasChanges)
	})

	t.Run("should return true when worktree has new untracked files", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "new-file.txt"), []byte("content"), 0o600))
		ctx := newBatchGitContext(t, repoDir)

		// when
		hasChanges, err := ctx.HasChanges()

		// then
		require.NoError(t, err)
		assert.True(t, hasChanges)
	})

	t.Run("should return true when tracked file is modified", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("modified"), 0o600))
		ctx := newBatchGitContext(t, repoDir)

		// when
		hasChanges, err := ctx.HasChanges()

		// then
		require.NoError(t, err)
		assert.True(t, hasChanges)
	})
}


func TestSwitchToDefault(t *testing.T) {
	t.Parallel()

	t.Run("should switch back to default branch after creating another branch", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		ctx := newBatchGitContext(t, repoDir)
		require.NoError(t, ctx.CreateBranchFromDefault("chore/feature"))

		// verify we are on the feature branch
		repo, err := git.PlainOpen(repoDir)
		require.NoError(t, err)
		head, err := repo.Head()
		require.NoError(t, err)
		assert.Equal(t, "refs/heads/chore/feature", head.Name().String())

		// when
		err = ctx.SwitchToDefault()

		// then
		require.NoError(t, err)
		head, err = repo.Head()
		require.NoError(t, err)
		assert.Equal(t, "refs/heads/main", head.Name().String())
	})

	t.Run("should succeed when already on default branch", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		ctx := newBatchGitContext(t, repoDir)

		// when
		err := ctx.SwitchToDefault()

		// then
		require.NoError(t, err)
	})
}

func TestResetToDefault(t *testing.T) {
	t.Parallel()

	t.Run("should discard uncommitted changes and switch to default branch", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		ctx := newBatchGitContext(t, repoDir)
		require.NoError(t, ctx.CreateBranchFromDefault("chore/dirty-branch"))
		trackedFile := filepath.Join(repoDir, "README.md")
		require.NoError(t, os.WriteFile(trackedFile, []byte("dirty content"), 0o600))

		// verify file is modified
		content, err := os.ReadFile(trackedFile)
		require.NoError(t, err)
		assert.Equal(t, "dirty content", string(content))

		// when
		err = ctx.ResetToDefault()

		// then
		require.NoError(t, err)

		// verify file content is reset
		content, err = os.ReadFile(trackedFile)
		require.NoError(t, err)
		assert.Equal(t, "# Test", string(content))

		// verify we are back on the default branch
		repo, openErr := git.PlainOpen(repoDir)
		require.NoError(t, openErr)
		head, headErr := repo.Head()
		require.NoError(t, headErr)
		assert.Equal(t, "refs/heads/main", head.Name().String())
	})

	t.Run("should succeed when worktree is already clean on default branch", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		ctx := newBatchGitContext(t, repoDir)

		// when
		err := ctx.ResetToDefault()

		// then
		require.NoError(t, err)
	})
}

func TestStashChanges(t *testing.T) {
	t.Parallel()

	t.Run("should return false when worktree is clean", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		ctx := newBatchGitContext(t, repoDir)

		// when
		stashed, err := ctx.StashChanges()

		// then
		require.NoError(t, err)
		assert.False(t, stashed)
	})

	t.Run("should stash and pop tracked file modifications", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		ctx := newBatchGitContext(t, repoDir)
		trackedFile := filepath.Join(repoDir, "README.md")
		require.NoError(t, os.WriteFile(trackedFile, []byte("stash me"), 0o600))

		// when
		stashed, err := ctx.StashChanges()

		// then
		require.NoError(t, err)
		assert.True(t, stashed)

		// verify file is reverted
		content, readErr := os.ReadFile(trackedFile)
		require.NoError(t, readErr)
		assert.Equal(t, "# Test", string(content))

		// pop and verify restoration
		require.NoError(t, ctx.PopStash())
		content, readErr = os.ReadFile(trackedFile)
		require.NoError(t, readErr)
		assert.Equal(t, "stash me", string(content))
	})
}

func TestDropStash(t *testing.T) {
	t.Parallel()

	t.Run("should not panic when no stash exists", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		ctx := newBatchGitContext(t, repoDir)

		// when / then
		assert.NotPanics(t, func() {
			ctx.DropStash()
		})
	})

	t.Run("should drop stash entry without restoring changes", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		ctx := newBatchGitContext(t, repoDir)
		trackedFile := filepath.Join(repoDir, "README.md")
		require.NoError(t, os.WriteFile(trackedFile, []byte("will be dropped"), 0o600))
		stashed, err := ctx.StashChanges()
		require.NoError(t, err)
		require.True(t, stashed)

		// when
		ctx.DropStash()

		// then - file should remain at its committed state (stash was dropped, not popped)
		content, readErr := os.ReadFile(trackedFile)
		require.NoError(t, readErr)
		assert.Equal(t, "# Test", string(content))
	})
}

// newBatchGitContext creates a BatchGitContext from a local repo for testing.
func newBatchGitContext(t *testing.T, repoDir string) *gitlocal.BatchGitContext {
	t.Helper()
	ctx, err := gitlocal.NewBatchGitContextFromLocal(repoDir, "main")
	require.NoError(t, err)
	return ctx
}
