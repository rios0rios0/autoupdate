package gitlocal_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlocal"
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

	t.Run(
		"should hard-reset the worktree to the given hash without losing earlier accumulated changes",
		func(t *testing.T) {
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
			require.NoError(t, statErrA, "fileA from a prior accumulated snapshot should be preserved")

			_, statErrB := os.Stat(filepath.Join(repoDir, "fileB.txt"))
			assert.True(t, os.IsNotExist(statErrB), "fileB from the failed updater should be discarded")
		},
	)
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

		hasChanges, hcErr := ctx.HasChanges()
		require.NoError(t, hcErr)
		assert.False(t, hasChanges, "worktree should be clean after a snapshot commit")
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
		require.NoError(t, statErrA)
		_, statErrB := os.Stat(filepath.Join(repoDir, "fileB.txt"))
		require.NoError(t, statErrB)

		hasChanges, hcErr := ctx.HasChanges()
		require.NoError(t, hcErr)
		assert.True(t, hasChanges, "after flattening, the cumulative diff should be visible in the worktree")
	})
}

func TestFlattenOnto(t *testing.T) {
	t.Parallel()

	t.Run("should collapse the snapshots above a base without rewriting the commit below them", func(t *testing.T) {
		t.Parallel()

		// given -- a branch that already carries a commit of its own (the one a dedicated
		// upgrade pushed), and two snapshots on top of it
		repoDir := createTestRepoWithCommit(t)
		ctx := newBatchGitContext(t, repoDir)
		require.NoError(t, ctx.CreateBranchFromDefault("chore/autoupdate-2026-04-15"))

		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "pushed.txt"), []byte("pushed"), 0o600))
		pushed, err := ctx.AdvanceSnapshot(plumbing.ZeroHash)
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "fileA.txt"), []byte("A"), 0o600))
		snapA, err := ctx.AdvanceSnapshot(pushed)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "fileB.txt"), []byte("B"), 0o600))
		_, err = ctx.AdvanceSnapshot(snapA)
		require.NoError(t, err)

		// when
		require.NoError(t, ctx.FlattenOnto(pushed))

		// then -- HEAD is back on the pushed commit, which still holds its file, and the
		// snapshots' files are the worktree's pending change
		head, headErr := ctx.HeadHash()
		require.NoError(t, headErr)
		assert.Equal(t, pushed, head)

		repo, openErr := git.PlainOpen(repoDir)
		require.NoError(t, openErr)
		status, statusErr := worktreeStatus(repo)
		require.NoError(t, statusErr)
		assert.Contains(t, status, "fileA.txt")
		assert.Contains(t, status, "fileB.txt")
		assert.NotContains(t, status, "pushed.txt")
	})
}

// worktreeStatus returns the paths the worktree reports as changed against HEAD.
func worktreeStatus(repo *git.Repository) ([]string, error) {
	wt, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	status, err := wt.Status()
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(status))
	for path, fileStatus := range status {
		if fileStatus.Worktree != git.Unmodified || fileStatus.Staging != git.Unmodified {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func TestOpenCheckoutRestoreSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("should roll an updater back without touching what git ignores", func(t *testing.T) {
		t.Parallel()

		// given -- a checkout holding ignored files (a cache and a secret), and a failing
		// updater that edited a tracked file and wrote a new one
		repoDir := createTestRepoWithCommit(t)
		writeFile(t, repoDir, ".gitignore", ".terraform/\n.env\n")
		commitAll(t, repoDir, "chore: ignored the local state")
		writeFile(t, repoDir, ".terraform/modules/modules.json", "{}")
		writeFile(t, repoDir, ".env", "TOKEN=fixture-token-placeholder")
		ctx, err := gitlocal.OpenCheckout(repoDir)
		require.NoError(t, err)
		base, err := ctx.HeadHash()
		require.NoError(t, err)
		writeFile(t, repoDir, "README.md", "half-written")
		writeFile(t, repoDir, "fragment.yaml", "kind: 'Changed'")

		// when
		require.NoError(t, ctx.RestoreSnapshot(base))

		// then -- the updater's writes are gone, the ignored files are not
		assert.Equal(t, "# Test", readTestFile(t, repoDir, "README.md"))
		assert.NoFileExists(t, filepath.Join(repoDir, "fragment.yaml"))
		assert.Equal(t, "{}", readTestFile(t, repoDir, ".terraform/modules/modules.json"))
		assert.Equal(t, "TOKEN=fixture-token-placeholder", readTestFile(t, repoDir, ".env"))
	})

	t.Run("should never commit an ignored file into a snapshot", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		writeFile(t, repoDir, ".gitignore", ".env\n")
		commitAll(t, repoDir, "chore: ignored the local state")
		writeFile(t, repoDir, ".env", "TOKEN=fixture-token-placeholder")
		ctx, err := gitlocal.OpenCheckout(repoDir)
		require.NoError(t, err)
		base, err := ctx.HeadHash()
		require.NoError(t, err)
		writeFile(t, repoDir, "README.md", "upgraded")

		// when
		snapshot, err := ctx.AdvanceSnapshot(base)

		// then
		require.NoError(t, err)
		repo, openErr := git.PlainOpen(repoDir)
		require.NoError(t, openErr)
		commit, commitErr := repo.CommitObject(snapshot)
		require.NoError(t, commitErr)
		tree, treeErr := commit.Tree()
		require.NoError(t, treeErr)
		_, findErr := tree.File(".env")
		require.Error(t, findErr)
		_, readmeErr := tree.File("README.md")
		require.NoError(t, readmeErr)
	})
}

// writeFile writes content to a repository-relative path, creating its directories.
func writeFile(t *testing.T, repoDir, name, content string) {
	t.Helper()

	full := filepath.Join(repoDir, filepath.FromSlash(name))
	// A directory needs the owner search bit, so 0o700 is the least-privilege mode here.
	// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o700))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
}

// readTestFile reads a repository-relative path.
func readTestFile(t *testing.T, repoDir, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(name)))
	require.NoError(t, err)
	return string(data)
}

// commitAll stages every change, honouring .gitignore, and commits it.
func commitAll(t *testing.T, repoDir, message string) {
	t.Helper()

	repo, err := git.PlainOpen(repoDir)
	require.NoError(t, err)
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, wt.AddWithOptions(&git.AddOptions{All: true}))
	_, err = wt.Commit(message, &git.CommitOptions{Author: &object.Signature{
		Name: "test", Email: "test@test.com", When: time.Now(),
	}})
	require.NoError(t, err)
}
