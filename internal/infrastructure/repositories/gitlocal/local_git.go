package gitlocal

import (
	"errors"
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	logger "github.com/sirupsen/logrus"

	gitops "github.com/rios0rios0/gitforge/pkg/git/infrastructure"
)

// LocalGitContext wraps go-git repository and worktree objects, providing
// high-level git operations for the local upgrade workflow.  It replaces
// the bash-generated git setup/finalize steps (clean check, branch
// creation, staging, committing, and pushing) with pure Go equivalents
// backed by gitforge.
type LocalGitContext struct {
	repo     *git.Repository
	workTree *git.Worktree
	repoDir  string
}

// NewLocalGitContext opens the repository at the given path and returns
// a ready-to-use context.  The caller should use EnsureClean, then
// CreateBranch, run language-specific upgrades, and finally call
// StageCommitAndPush.
func NewLocalGitContext(repoDir string) (*LocalGitContext, error) {
	repo, err := gitops.OpenRepo(repoDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository at %s: %w", repoDir, err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	return &LocalGitContext{
		repo:     repo,
		workTree: wt,
		repoDir:  repoDir,
	}, nil
}

// EnsureClean verifies that the working tree has no uncommitted changes.
// Returns an error if the worktree is dirty.
func (c *LocalGitContext) EnsureClean() error {
	clean, err := gitops.WorktreeIsClean(c.workTree)
	if err != nil {
		return fmt.Errorf("failed to check worktree status: %w", err)
	}

	if !clean {
		return errors.New("working tree has uncommitted changes, please commit or stash first")
	}

	return nil
}

// CreateBranch creates a new branch from HEAD and switches to it.
func (c *LocalGitContext) CreateBranch(branchName string) error {
	head, err := c.repo.Head()
	if err != nil {
		return fmt.Errorf("failed to get HEAD: %w", err)
	}

	logger.Infof("Creating branch %s...", branchName)
	return gitops.CreateAndSwitchBranch(c.repo, c.workTree, branchName, head.Hash())
}

// HasChanges returns true when the working tree has unstaged or
// untracked modifications.
func (c *LocalGitContext) HasChanges() (bool, error) {
	clean, err := gitops.WorktreeIsClean(c.workTree)
	if err != nil {
		return false, err
	}
	return !clean, nil
}

// StageCommitAndPush stages all changes, commits with the given message,
// and pushes the branch to the remote using HTTPS basic auth.
//
// The providerName selects the correct HTTPS username for the token
// (e.g. "x-access-token" for GitHub, "oauth2" for GitLab, "pat" for
// Azure DevOps).
//
// Returns true when changes were committed and pushed, false when
// the worktree was clean (nothing to push).
func (c *LocalGitContext) StageCommitAndPush(
	branchName, commitMessage, providerName, authToken string,
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

	if err = gitops.StageAll(c.workTree); err != nil {
		return false, fmt.Errorf("failed to stage changes: %w", err)
	}

	_, err = gitops.CommitChanges(c.repo, c.workTree, commitMessage, nil, "autoupdate", "autoupdate@noreply")
	if err != nil {
		return false, fmt.Errorf("failed to commit changes: %w", err)
	}

	refSpec := config.RefSpec(
		fmt.Sprintf("refs/heads/%s:refs/heads/%s", branchName, branchName),
	)

	if err = c.pushHTTPS(providerName, authToken, refSpec); err != nil {
		return false, fmt.Errorf("failed to push branch %s: %w", branchName, err)
	}

	return true, nil
}

// pushHTTPS pushes changes using go-git's HTTP basic auth with
// a provider-specific username and the supplied token as password.
func (c *LocalGitContext) pushHTTPS(providerName, authToken string, refSpec config.RefSpec) error {
	// providerUsername maps a provider name to the HTTPS username used for
	// git push authentication.  Each Git hosting provider expects a
	// specific fixed username when authenticating via personal access
	// tokens over HTTPS.
	providerUsername := map[string]string{
		"github":      "x-access-token",
		"gitlab":      "oauth2",
		"azuredevops": "pat",
	}

	username, ok := providerUsername[providerName]
	if !ok {
		return fmt.Errorf("unsupported provider for HTTPS push: %s", providerName)
	}

	logger.Infof("Pushing to remote via HTTPS (provider: %s)", providerName)
	return c.repo.Push(&git.PushOptions{
		RefSpecs:   []config.RefSpec{refSpec},
		RemoteName: "origin",
		Auth: &http.BasicAuth{
			Username: username,
			Password: authToken,
		},
	})
}
