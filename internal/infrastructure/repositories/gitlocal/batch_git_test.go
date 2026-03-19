//go:build unit

package gitlocal_test

import (
	"os"
	"path/filepath"
	"testing"

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

// newBatchGitContext creates a BatchGitContext from a local repo for testing.
func newBatchGitContext(t *testing.T, repoDir string) *gitlocal.BatchGitContext {
	t.Helper()
	ctx, err := gitlocal.NewBatchGitContextFromLocal(repoDir, "main")
	require.NoError(t, err)
	return ctx
}
