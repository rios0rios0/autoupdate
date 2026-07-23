//go:build unit

package commands_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	gitcfg "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlocal"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
	gitops "github.com/rios0rios0/gitforge/pkg/git/infrastructure"
)

// localAuthMethods satisfies the clone helper, which refuses an empty list. A remote on
// the local filesystem needs no transport authentication, so the value is never used.
func localAuthMethods() []transport.AuthMethod {
	return []transport.AuthMethod{&githttp.BasicAuth{Username: "unused", Password: "unused"}}
}

// newBareRemoteWithBranches creates a bare repository on disk carrying the given
// branches, and returns its path.
func newBareRemoteWithBranches(t *testing.T, branches ...string) string {
	t.Helper()

	bareDir := t.TempDir()
	_, err := git.PlainInit(bareDir, true)
	require.NoError(t, err)

	workDir := t.TempDir()
	repo, err := git.PlainInit(workDir, false)
	require.NoError(t, err)

	_, err = repo.CreateRemote(&gitcfg.RemoteConfig{Name: "origin", URLs: []string{bareDir}})
	require.NoError(t, err)

	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "README.md"), []byte("# Test\n"), 0o600))
	_, err = wt.Add("README.md")
	require.NoError(t, err)
	_, err = wt.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@test.com", When: time.Now()},
	})
	require.NoError(t, err)

	head, err := repo.Head()
	require.NoError(t, err)

	// publish the default branch under a stable name plus every requested branch
	refSpecs := []gitcfg.RefSpec{
		gitcfg.RefSpec("+" + head.Name().String() + ":refs/heads/main"),
	}
	for _, branch := range branches {
		require.NoError(t, repo.Storer.SetReference(
			plumbing.NewHashReference(plumbing.ReferenceName("refs/heads/"+branch), head.Hash()),
		))
		refSpecs = append(refSpecs,
			gitcfg.RefSpec("refs/heads/"+branch+":refs/heads/"+branch))
	}
	require.NoError(t, repo.Push(&git.PushOptions{RemoteName: "origin", RefSpecs: refSpecs}))

	// PlainInit leaves HEAD pointing at the init default branch, which was never
	// published here; a clone would fail to resolve it.
	bare, err := git.PlainOpen(bareDir)
	require.NoError(t, err)
	require.NoError(t, bare.Storer.SetReference(
		plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.ReferenceName("refs/heads/main")),
	))

	return bareDir
}

// cloneBatchContext clones the given bare remote into a BatchGitContext.
func cloneBatchContext(t *testing.T, bareDir string) *gitlocal.BatchGitContext {
	t.Helper()

	registry := repositories.NewProviderRegistry()
	batchCtx, err := gitlocal.CloneRepository(
		gitops.NewGitOperations(registry), bareDir, "main", localAuthMethods(), registry,
	)
	require.NoError(t, err)
	t.Cleanup(batchCtx.Close)

	return batchCtx
}

func TestCleanupStaleAggregateBranchesBehaviour(t *testing.T) {
	t.Parallel()

	t.Run("should close the pull requests and delete only the aggregate branches", func(t *testing.T) {
		t.Parallel()

		// given a remote carrying two abandoned dated branches next to unrelated ones
		bareDir := newBareRemoteWithBranches(t,
			"chore/autoupdate-2026-07-21",
			"chore/autoupdate-2026-07-22",
			"chore/upgrade-go-deps",
			"feat/human-work",
		)
		batchCtx := cloneBatchContext(t, bareDir)

		spy := repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy()
		spy.PRClosedResult = true

		// when
		commands.CleanupStaleAggregateBranches(
			context.Background(), batchCtx, spy, entities.Repository{Organization: "org", Name: "repo"},
			&entities.Settings{}, "main", localAuthMethods(),
		)

		// then a pull request was closed for each dated branch, and only those
		assert.Equal(t, []string{
			"chore/autoupdate-2026-07-21",
			"chore/autoupdate-2026-07-22",
		}, spy.PRClosedBranches)

		remaining, err := batchCtx.ListRemoteBranches(localAuthMethods())
		require.NoError(t, err)
		assert.NotContains(t, remaining, "chore/autoupdate-2026-07-21")
		assert.NotContains(t, remaining, "chore/autoupdate-2026-07-22")
		assert.Contains(t, remaining, "chore/upgrade-go-deps")
		assert.Contains(t, remaining, "feat/human-work")
		assert.Contains(t, remaining, "main")
	})

	t.Run("should keep the branch when closing its pull request fails", func(t *testing.T) {
		t.Parallel()

		// given a provider that cannot close the pull request
		bareDir := newBareRemoteWithBranches(t, "chore/autoupdate-2026-07-21")
		batchCtx := cloneBatchContext(t, bareDir)

		spy := repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy()
		spy.PRCloseErr = errors.New("provider unavailable")

		// when
		commands.CleanupStaleAggregateBranches(
			context.Background(), batchCtx, spy, entities.Repository{Organization: "org", Name: "repo"},
			&entities.Settings{}, "main", localAuthMethods(),
		)

		// then the branch survives, so the pair stays retryable rather than leaving an
		// open pull request whose source branch no longer exists
		remaining, err := batchCtx.ListRemoteBranches(localAuthMethods())
		require.NoError(t, err)
		assert.Contains(t, remaining, "chore/autoupdate-2026-07-21")
	})

	t.Run("should do nothing when there are no aggregate branches", func(t *testing.T) {
		t.Parallel()

		// given
		bareDir := newBareRemoteWithBranches(t, "feat/human-work")
		batchCtx := cloneBatchContext(t, bareDir)

		spy := repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy()

		// when
		commands.CleanupStaleAggregateBranches(
			context.Background(), batchCtx, spy, entities.Repository{Organization: "org", Name: "repo"},
			&entities.Settings{}, "main", localAuthMethods(),
		)

		// then no pull request is touched and every branch survives
		assert.Empty(t, spy.PRClosedBranches)
		remaining, err := batchCtx.ListRemoteBranches(localAuthMethods())
		require.NoError(t, err)
		assert.Contains(t, remaining, "feat/human-work")
	})

	t.Run("should honour a custom aggregate prefix from the settings", func(t *testing.T) {
		t.Parallel()

		// given
		bareDir := newBareRemoteWithBranches(t, "bot/refresh-2026-07-21", "chore/autoupdate-2026-07-21")
		batchCtx := cloneBatchContext(t, bareDir)

		spy := repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy()
		spy.PRClosedResult = true
		settings := &entities.Settings{AggregateBranchPrefix: "bot/refresh-"}

		// when
		commands.CleanupStaleAggregateBranches(
			context.Background(), batchCtx, spy, entities.Repository{Organization: "org", Name: "repo"},
			settings, "main", localAuthMethods(),
		)

		// then only the configured prefix is swept
		assert.Equal(t, []string{"bot/refresh-2026-07-21"}, spy.PRClosedBranches)
		remaining, err := batchCtx.ListRemoteBranches(localAuthMethods())
		require.NoError(t, err)
		assert.NotContains(t, remaining, "bot/refresh-2026-07-21")
		assert.Contains(t, remaining, "chore/autoupdate-2026-07-21")
	})
}
