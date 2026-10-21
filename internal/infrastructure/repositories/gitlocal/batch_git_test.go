package gitlocal_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlocal"
	"github.com/rios0rios0/autoupdate/internal/support"
	gitops "github.com/rios0rios0/gitforge/v4/pkg/git/infrastructure"
)

func TestCreateBranchFromDefault_PreservesUncommittedChanges(t *testing.T) {
	t.Parallel()

	t.Run("should keep uncommitted changes when the branch is created", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		batchCtx := newBatchGitContext(t, repoDir)
		trackedFile := filepath.Join(repoDir, "README.md")
		require.NoError(t, os.WriteFile(trackedFile, []byte("upgraded content"), 0o600))

		// when
		err := batchCtx.CreateBranchFromDefault("chore/upgrade-test")

		// then the switch leaves the working tree alone, as `git switch -c` does
		require.NoError(t, err)
		content, err := os.ReadFile(trackedFile)
		require.NoError(t, err)
		assert.Equal(t, "upgraded content", string(content), "creating the branch should keep the changes")
	})
}

func TestCleanupStaleTempDirs(t *testing.T) {
	// Not parallel: t.Setenv modifies process-wide env vars and the function
	// under test operates on the OS temp directory.

	t.Run("should remove the shared cache a killed run left behind", func(t *testing.T) {
		// given a shared cache nobody has renewed for an hour
		t.Setenv("TMPDIR", t.TempDir())
		//nolint:usetesting // the name and the location are what is being tested
		cacheDir, err := os.MkdirTemp("", support.SharedCachePattern)
		require.NoError(t, err)
		// A directory needs the owner search bit, so 0o700 is the least-privilege mode.
		// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
		require.NoError(t, os.MkdirAll(filepath.Join(cacheDir, "go", "mod"), 0o700))
		past := time.Now().Add(-time.Hour)
		require.NoError(t, os.Chtimes(cacheDir, past, past))

		// when
		gitlocal.CleanupStaleTempDirs()

		// then
		assert.NoDirExists(t, cacheDir)
	})

	t.Run("should keep a shared cache its run renewed within the threshold", func(t *testing.T) {
		// given a shared cache whose heartbeat renewed it a minute ago (see
		// support.SharedCache: a live run keeps renewing it)
		t.Setenv("TMPDIR", t.TempDir())
		//nolint:usetesting // the name and the location are what is being tested
		cacheDir, err := os.MkdirTemp("", support.SharedCachePattern)
		require.NoError(t, err)
		renewed := time.Now().Add(-time.Minute)
		require.NoError(t, os.Chtimes(cacheDir, renewed, renewed))

		// when another run sweeps the same temporary directory
		gitlocal.CleanupStaleTempDirs()

		// then
		assert.DirExists(t, cacheDir)
	})

	t.Run("should remove stale autoupdate temp directories and changelog files", func(t *testing.T) {
		// given — scope os.TempDir() to a test-owned directory so we don't
		// interfere with other tests or processes on the same machine.
		tempBase := t.TempDir()
		t.Setenv("TMPDIR", tempBase)

		// t.TempDir() is not a substitute for any of these three: CleanupStaleTempDirs
		// scans os.TempDir() and matches on the `autoupdate-*` names, so a directory the
		// testing package names, or one nested a level deeper, is invisible to the code
		// under test.
		//nolint:usetesting // the name and the location are what is being tested
		batchDir, err := os.MkdirTemp("", "autoupdate-batch-*")
		require.NoError(t, err)
		//nolint:usetesting // the name and the location are what is being tested
		localDir, err := os.MkdirTemp("", "autoupdate-local-*")
		require.NoError(t, err)
		//nolint:usetesting // the name and the location are what is being tested
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

	t.Run("should remove a stale workspace even when it holds Go's read-only module cache", func(t *testing.T) {
		// given -- a workspace left by a run that was killed mid-repository, still
		// holding the module cache Go writes with read-only directories
		t.Setenv("TMPDIR", t.TempDir())
		//nolint:usetesting // the name and the location are what is being tested
		workspace, err := os.MkdirTemp("", "autoupdate-batch-*")
		require.NoError(t, err)
		module := filepath.Join(workspace, "tooling", "go", "mod", "example.com", "dependency@v1.0.0")
		// A directory needs the owner search bit, so 0o700 is the least-privilege mode.
		// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
		require.NoError(t, os.MkdirAll(module, 0o700))
		require.NoError(
			t,
			os.WriteFile(filepath.Join(module, "go.mod"), []byte("module example.com/dependency\n"), 0o400),
		)
		for dir := module; dir != workspace; dir = filepath.Dir(dir) {
			require.NoError(t, os.Chmod(dir, 0o555))
		}
		past := time.Now().Add(-time.Hour)
		require.NoError(t, os.Chtimes(workspace, past, past))

		// when
		gitlocal.CleanupStaleTempDirs()

		// then
		assert.NoDirExists(t, workspace)
	})

	t.Run("should keep a workspace a concurrent run may still be using", func(t *testing.T) {
		// given
		t.Setenv("TMPDIR", t.TempDir())
		//nolint:usetesting // the name and the location are what is being tested
		workspace, err := os.MkdirTemp("", "autoupdate-batch-*")
		require.NoError(t, err)

		// when
		gitlocal.CleanupStaleTempDirs()

		// then
		assert.DirExists(t, workspace)
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

func TestCloneRepository(t *testing.T) {
	t.Parallel()

	t.Run("should clone into the directory it is given and leave it to its owner", func(t *testing.T) {
		t.Parallel()

		// given
		remoteDir := createTestRepoWithCommit(t)
		repoDir := filepath.Join(t.TempDir(), "repo")
		registry := repositories.NewProviderRegistry()

		// when
		ctx, err := gitlocal.CloneRepository(
			gitops.NewGitOperations(registry), remoteDir, "refs/heads/main",
			[]transport.AuthMethod{&githttp.BasicAuth{Username: "unused", Password: "unused"}},
			registry, repoDir,
		)

		// then
		require.NoError(t, err)
		assert.Equal(t, repoDir, ctx.RepoDir())
		assert.FileExists(t, filepath.Join(repoDir, "README.md"))
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

// newBatchGitContext creates a BatchGitContext from a local repo for testing.
func newBatchGitContext(t *testing.T, repoDir string) *gitlocal.BatchGitContext {
	t.Helper()
	ctx, err := gitlocal.NewBatchGitContextFromLocal(repoDir, "main")
	require.NoError(t, err)
	return ctx
}
