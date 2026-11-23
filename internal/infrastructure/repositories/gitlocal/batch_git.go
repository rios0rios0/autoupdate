package gitlocal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
	gitops "github.com/rios0rios0/gitforge/v4/pkg/git/infrastructure"
)

// BatchGitContext wraps a cloned go-git repository for batch mode operations.
// It provides branch creation, GPG/SSH-signed commits, and transport-detected
// push — the same capabilities as LocalGitContext but for remotely cloned repos.
type BatchGitContext struct {
	repo          *git.Repository
	workTree      *git.Worktree
	repoDir       string
	defaultBranch string
	resolver      PushAuthResolver
	// checkout reports a context opened on the operator's own checkout rather than on a
	// clone the run made; see OpenCheckout.
	checkout bool
}

// CloneRepository clones a remote repository into repoDir using the provided
// auth methods (multi-token retry).
//
// The context does not own repoDir and never removes it: the batch run clones
// into the repository's support.RepositoryWorkspace, whose removal takes the
// clone with it together with everything the updaters' package managers wrote
// beside it. A failed clone is left for that removal too.
func CloneRepository(
	gitOps *gitops.GitOperations,
	cloneURL string,
	defaultBranch string,
	authMethods []transport.AuthMethod,
	resolver PushAuthResolver,
	repoDir string,
) (*BatchGitContext, error) {
	repo, err := gitOps.CloneRepo(cloneURL, repoDir, authMethods)
	if err != nil {
		return nil, fmt.Errorf("failed to clone %s: %w", cloneURL, err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	cleanBranch := strings.TrimPrefix(defaultBranch, "refs/heads/")

	return &BatchGitContext{
		repo:          repo,
		workTree:      wt,
		repoDir:       repoDir,
		defaultBranch: cleanBranch,
		resolver:      resolver,
	}, nil
}

// NewBatchGitContextFromLocal creates a BatchGitContext from an existing local
// repository. This is intended for testing — production code uses CloneRepository,
// and OpenCheckout for the operator's own checkout.
func NewBatchGitContextFromLocal(repoDir, defaultBranch string) (*BatchGitContext, error) {
	repo, err := gitops.OpenRepo(repoDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository at %s: %w", repoDir, err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	return &BatchGitContext{
		repo:          repo,
		workTree:      wt,
		repoDir:       repoDir,
		defaultBranch: strings.TrimPrefix(defaultBranch, "refs/heads/"),
	}, nil
}

// OpenCheckout opens the operator's own checkout for the snapshots that keep one
// updater's failure from reaching the others' changes, which `autoupdate .` takes from
// the aggregate pipeline (HeadHash, AdvanceSnapshot, RestoreSnapshot and FlattenOnto); it
// commits and pushes through LocalGitContext.
//
// A checkout is not a clone. It holds what git does not track -- ignored build output and
// caches, an .env -- and go-git's hard reset deletes every file its index does not list,
// ignored ones included, which is harmless in a clone the run throws away and data loss
// in a checkout. RestoreSnapshot on a checkout therefore resets through git itself, which
// only rewrites what git tracks, and then removes the untracked files the failing updater
// wrote, never the ignored ones. The operator's own untracked work is not among them: the
// run stashes it before any updater runs.
func OpenCheckout(repoDir string) (*BatchGitContext, error) {
	ctx, err := NewBatchGitContextFromLocal(repoDir, "")
	if err != nil {
		return nil, err
	}
	ctx.checkout = true
	return ctx, nil
}

// RepoDir returns the filesystem path of the cloned repository.
func (c *BatchGitContext) RepoDir() string {
	return c.repoDir
}

// CreateBranchFromDefault creates a new branch from the default branch HEAD
// and switches to it.
func (c *BatchGitContext) CreateBranchFromDefault(branchName string) error {
	head, err := c.repo.Head()
	if err != nil {
		return fmt.Errorf("failed to get HEAD: %w", err)
	}

	logger.Infof("Creating branch %s...", branchName)
	return gitops.CreateAndSwitchBranch(c.repo, c.workTree, branchName, head.Hash())
}

// ListRemoteBranches returns the names of the branches published on the origin
// remote, queried from the remote itself rather than from the clone's refs.
func (c *BatchGitContext) ListRemoteBranches(authMethods []transport.AuthMethod) ([]string, error) {
	branches, err := gitops.ListRemoteBranches(c.repo, authMethods)
	if err != nil {
		return nil, fmt.Errorf("failed to list remote branches: %w", err)
	}
	return branches, nil
}

// DeleteBranch removes branchName from the origin remote and then locally.
//
// Both halves are needed: deleting the branch on the remote leaves the local one
// behind, and a leftover local branch makes the branch still look like it exists,
// which would stop it from being recreated afterwards.
func (c *BatchGitContext) DeleteBranch(branchName string, authMethods []transport.AuthMethod) error {
	if err := gitops.DeleteRemoteBranch(c.repo, branchName, authMethods); err != nil {
		return fmt.Errorf("failed to delete remote branch %s: %w", branchName, err)
	}

	if err := gitops.DeleteLocalBranch(c.repo, branchName); err != nil {
		return fmt.Errorf("failed to delete local branch %s: %w", branchName, err)
	}

	return nil
}

// SwitchToDefault switches back to the default branch. This is used after
// a successful commit+push where the worktree is already clean.
func (c *BatchGitContext) SwitchToDefault() error {
	return c.workTree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(c.defaultBranch),
	})
}

// ResetToDefault discards all uncommitted changes (hard reset) and switches
// back to the default branch. This ensures the next updater starts from a
// clean worktree, even if the previous updater failed mid-way through
// modifying files.
func (c *BatchGitContext) ResetToDefault() error {
	if err := c.workTree.Reset(&git.ResetOptions{Mode: git.HardReset}); err != nil {
		return fmt.Errorf("failed to hard-reset worktree: %w", err)
	}
	return c.SwitchToDefault()
}

// HasChanges returns true when the working tree has unstaged or untracked
// modifications.
func (c *BatchGitContext) HasChanges() (bool, error) {
	clean, err := gitops.WorktreeIsClean(c.workTree)
	if err != nil {
		return false, err
	}
	return !clean, nil
}

// CommitSignedAndPush stages all changes, commits with GPG/SSH signing
// (when configured), and pushes the branch to the remote with transport
// auto-detection.
//
// The signing configuration comes from the cloned repo's git config and the
// global Settings (GpgKeyPath, GpgKeyPassphrase). Multi-token auth retry
// is handled by CollectBatchAuthMethods.
//
// Returns true when changes were committed and pushed, false when the
// worktree was clean.
func (c *BatchGitContext) CommitSignedAndPush(
	branchName, commitMessage string,
	settings *entities.Settings,
	authMethods []transport.AuthMethod,
) (bool, error) {
	hasChanges, err := c.HasChanges()
	if err != nil {
		return false, err
	}

	if !hasChanges {
		logger.Info("No changes detected.")
		return false, nil
	}

	logger.Info("Changes detected, committing and pushing...")

	err = commitAllSigned(c.repo, c.workTree, commitMessage, signedCommit{
		DefaultName:   "autoupdate[bot]",
		DefaultEmail:  "autoupdate[bot]@users.noreply.github.com",
		GPGKeyPath:    settings.GpgKeyPath,
		GPGPassphrase: settings.GpgKeyPassphrase,
	})
	if err != nil {
		return false, err
	}

	if err = pushBranch(c.repo, branchName, authMethods); err != nil {
		return false, err
	}

	return true, nil
}

// snapshotCommitSubject is the sentinel subject line used by AdvanceSnapshot
// for the throw-away commits that materialize per-updater rollback points.
// These commits never reach the remote: FlattenToWorktree collapses them
// before CommitSignedAndPush builds the final aggregate commit.
const snapshotCommitSubject = "__autoupdate_snapshot__"

// HeadHash returns the current HEAD commit hash. The aggregate pipeline
// uses this to capture a baseline before running each updater so it can
// roll back only that updater's partial writes if it fails.
func (c *BatchGitContext) HeadHash() (plumbing.Hash, error) {
	head, err := c.repo.Head()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("failed to resolve HEAD: %w", err)
	}
	return head.Hash(), nil
}

// RestoreSnapshot hard-resets the worktree to the given commit without
// switching branches. This is the rollback primitive for per-updater
// failure isolation in the aggregate pipeline: after a failing updater
// leaves the worktree in a half-modified state, callers restore the last
// known-good snapshot so the next updater starts from the accumulated
// state of all prior successful updaters.
func (c *BatchGitContext) RestoreSnapshot(hash plumbing.Hash) error {
	if c.checkout {
		return c.restoreCheckoutSnapshot(hash)
	}
	if err := c.workTree.Reset(&git.ResetOptions{
		Mode:   git.HardReset,
		Commit: hash,
	}); err != nil {
		return fmt.Errorf("failed to restore snapshot %s: %w", hash.String()[:7], err)
	}
	return nil
}

// restoreCheckoutSnapshot is RestoreSnapshot for the operator's checkout: `git reset
// --hard` puts the tracked files back, and `git clean -d` (without -x) removes the
// untracked ones, leaving every ignored file where it is.
func (c *BatchGitContext) restoreCheckoutSnapshot(hash plumbing.Hash) error {
	for _, args := range [][]string{
		{"reset", "--hard", "--quiet", hash.String()},
		{"clean", "-d", "--force", "--quiet"},
	} {
		output, err := support.GitCommand(context.TODO(), c.repoDir, args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf(
				"failed to restore snapshot %s (git %s): %w\nOutput: %s",
				hash.String()[:7], args[0], err, string(output),
			)
		}
	}
	return nil
}

// AdvanceSnapshot stages all current worktree changes and commits them as
// a throw-away snapshot on the current branch, returning the new HEAD
// hash. The commit is intentionally unsigned and carries
// snapshotCommitSubject so reviewers can recognise it if it ever leaks.
// FlattenToWorktree collapses these commits before the aggregate pipeline
// produces the real signed commit, so they never reach the remote.
//
// The prev hash is passed in for traceability/logging only; the new
// commit is created on top of whatever HEAD currently points at.
func (c *BatchGitContext) AdvanceSnapshot(prev plumbing.Hash) (plumbing.Hash, error) {
	if err := gitops.StageAll(c.workTree); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("failed to stage snapshot: %w", err)
	}
	commit, err := c.workTree.Commit(snapshotCommitSubject, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "autoupdate[bot]",
			Email: "autoupdate[bot]@users.noreply.github.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf(
			"failed to create snapshot commit (prev=%s): %w",
			prev.String()[:7], err,
		)
	}
	return commit, nil
}

// FlattenToWorktree collapses any snapshot commits made via AdvanceSnapshot
// back into the worktree and index, so a subsequent CommitSignedAndPush
// produces one signed commit representing the full aggregate diff. It
// works by mixed-resetting the current branch back to the default branch
// head; nothing has been pushed yet, so rewriting the branch here is safe.
//
// MixedReset (rather than SoftReset) is used so the change set lands in
// both the index and the working tree — CommitSignedAndPush calls
// HasChanges() / WorktreeIsClean() which inspect the working tree rather
// than the index, so the diff must be visible there.
func (c *BatchGitContext) FlattenToWorktree() error {
	defaultRef, err := c.repo.Reference(
		plumbing.NewBranchReferenceName(c.defaultBranch), true,
	)
	if err != nil {
		return fmt.Errorf("failed to resolve default branch ref: %w", err)
	}
	return c.FlattenOnto(defaultRef.Hash())
}

// FlattenOnto collapses the snapshot commits made on top of base back into the
// worktree and index, as FlattenToWorktree does for the default branch. `autoupdate .`
// needs the base named: when a dedicated upgrade has already committed and pushed on
// the branch, the snapshots sit on that commit, and resetting past it would rewrite a
// commit the remote already has.
func (c *BatchGitContext) FlattenOnto(base plumbing.Hash) error {
	if err := c.workTree.Reset(&git.ResetOptions{
		Mode:   git.MixedReset,
		Commit: base,
	}); err != nil {
		return fmt.Errorf("failed to flatten worktree: %w", err)
	}
	return nil
}

// staleTempThreshold is the minimum age a temp path must have before it is
// considered stale. This prevents concurrent autoupdate runs from deleting
// each other's active temporary directories.
const staleTempThreshold = 30 * time.Minute

// CleanupStaleTempDirs removes leftover autoupdate temporary directories and
// files from the OS temp dir. These accumulate when the process is killed
// (SIGKILL, OOM) before deferred cleanup can run. Only paths older than
// staleTempThreshold are removed to avoid interfering with concurrent runs.
//
// A leftover repository workspace holds that repository's package-manager
// caches as well as its clone, and Go's module cache among them is read-only, so
// each path goes through support.RemoveTree: [os.RemoveAll] would give up on the
// cache and leave it behind for every later run to fail to remove too.
func CleanupStaleTempDirs() {
	tmpDir := os.TempDir()
	cutoff := time.Now().Add(-staleTempThreshold)

	for _, pattern := range []string{
		support.RepositoryWorkspacePattern,
		support.SharedCachePattern,
		"autoupdate-local-*",
		"autoupdate-go-*",
		"autoupdate-js-*",
		"autoupdate-python-*",
		"autoupdate-java-*",
		"autoupdate-csharp-*",
		"autoupdate-ruby-*",
		"autoupdate-dart-*",
		"autoupdate-migration-*",
		"autoupdate-gitconfig-*",
		"autoupdate-changelog-*.md",
	} {
		matches, _ := filepath.Glob(filepath.Join(tmpDir, pattern))
		for _, m := range matches {
			info, err := os.Lstat(m)
			if err != nil || info.ModTime().After(cutoff) {
				continue
			}
			logger.Debugf("Cleaning up stale temp path: %s", m)
			if removeErr := support.RemoveTree(m); removeErr != nil {
				logger.Warnf("Could not remove the stale temp path %s: %v", m, removeErr)
			}
		}
	}
}
