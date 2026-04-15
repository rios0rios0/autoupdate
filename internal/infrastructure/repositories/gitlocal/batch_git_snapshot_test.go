//go:build unit

package gitlocal_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeadHash(t *testing.T) {
	t.Parallel()

	t.Run("should return the current HEAD hash after creating an aggregate branch", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		ctx := newBatchGitContext(t, repoDir)
		require.NoError(t, ctx.CreateBranchFromDefault("chore/autoupdate-2026-04-15"))

		// when
		hash, err := ctx.HeadHash()

		// then
		require.NoError(t, err)
		assert.NotEqual(t, plumbing.ZeroHash, hash)
	})
}

func TestRestoreSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("should hard-reset the worktree to the given hash without losing earlier accumulated changes", func(t *testing.T) {
		t.Parallel()

		// given — clone, branch, write fileA, advance snapshot, then write fileB on top
		repoDir := createTestRepoWithCommit(t)
		ctx := newBatchGitContext(t, repoDir)
		require.NoError(t, ctx.CreateBranchFromDefault("chore/autoupdate-2026-04-15"))

		baseHash, err := ctx.HeadHash()
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "fileA.txt"), []byte("A"), 0o600))
		snapAfterA, err := ctx.AdvanceSnapshot(baseHash)
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "fileB.txt"), []byte("B"), 0o600))

		// when — restore back to the snapshot taken after fileA
		require.NoError(t, ctx.RestoreSnapshot(snapAfterA))

		// then — fileA is still present (committed into the snapshot), fileB is gone
		_, statErrA := os.Stat(filepath.Join(repoDir, "fileA.txt"))
		assert.NoError(t, statErrA, "fileA from a prior accumulated snapshot should be preserved")

		_, statErrB := os.Stat(filepath.Join(repoDir, "fileB.txt"))
		assert.True(t, os.IsNotExist(statErrB), "fileB from the failed updater should be discarded")
	})
}

func TestAdvanceSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("should produce a new HEAD hash and leave the worktree clean", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		ctx := newBatchGitContext(t, repoDir)
		require.NoError(t, ctx.CreateBranchFromDefault("chore/autoupdate-2026-04-15"))

		baseHash, err := ctx.HeadHash()
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "fileA.txt"), []byte("A"), 0o600))

		// when
		newHash, advErr := ctx.AdvanceSnapshot(baseHash)

		// then
		require.NoError(t, advErr)
		assert.NotEqual(t, plumbing.ZeroHash, newHash)
		assert.NotEqual(t, baseHash, newHash)

		head, headErr := ctx.HeadHash()
		require.NoError(t, headErr)
		assert.Equal(t, newHash, head)

		clean, hcErr := ctx.HasChanges()
		require.NoError(t, hcErr)
		assert.False(t, clean, "worktree should be clean after a snapshot commit")
	})
}

func TestFlattenToWorktree(t *testing.T) {
	t.Parallel()

	t.Run("should collapse two snapshot commits back into the worktree as the cumulative diff", func(t *testing.T) {
		t.Parallel()

		// given — branch from default, accumulate two snapshots
		repoDir := createTestRepoWithCommit(t)
		ctx := newBatchGitContext(t, repoDir)
		require.NoError(t, ctx.CreateBranchFromDefault("chore/autoupdate-2026-04-15"))

		baseHash, err := ctx.HeadHash()
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "fileA.txt"), []byte("A"), 0o600))
		snapA, err := ctx.AdvanceSnapshot(baseHash)
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "fileB.txt"), []byte("B"), 0o600))
		_, err = ctx.AdvanceSnapshot(snapA)
		require.NoError(t, err)

		// when
		require.NoError(t, ctx.FlattenToWorktree())

		// then — both files still exist on disk and the worktree reports changes
		_, statErrA := os.Stat(filepath.Join(repoDir, "fileA.txt"))
		assert.NoError(t, statErrA)
		_, statErrB := os.Stat(filepath.Join(repoDir, "fileB.txt"))
		assert.NoError(t, statErrB)

		hasChanges, hcErr := ctx.HasChanges()
		require.NoError(t, hcErr)
		assert.True(t, hasChanges, "after flattening, the cumulative diff should be visible in the worktree")
	})
}
