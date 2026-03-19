package gitlocal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	gitops "github.com/rios0rios0/gitforge/pkg/git/infrastructure"
	gitHelpers "github.com/rios0rios0/gitforge/pkg/git/infrastructure/helpers"
	signingInfra "github.com/rios0rios0/gitforge/pkg/signing/infrastructure"
)

// BatchGitContext wraps a cloned go-git repository for batch mode operations.
// It provides branch creation, GPG/SSH-signed commits, and transport-detected
// push — the same capabilities as LocalGitContext but for remotely cloned repos.
type BatchGitContext struct {
	repo          *git.Repository
	workTree      *git.Worktree
	tmpDir        string
	defaultBranch string
	resolver      PushAuthResolver
	stashRef      string // set by StashChanges, verified by PopStash
}

// CloneRepository clones a remote repository into a temporary directory using
// the provided auth methods (multi-token retry). The caller must call Close()
// when done to remove the temp directory.
func CloneRepository(
	gitOps *gitops.GitOperations,
	cloneURL string,
	defaultBranch string,
	authMethods []transport.AuthMethod,
	resolver PushAuthResolver,
) (*BatchGitContext, error) {
	tmpDir, err := os.MkdirTemp("", "autoupdate-batch-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	repo, err := gitOps.CloneRepo(cloneURL, tmpDir, authMethods)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to clone %s: %w", cloneURL, err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	cleanBranch := strings.TrimPrefix(defaultBranch, "refs/heads/")

	return &BatchGitContext{
		repo:          repo,
		workTree:      wt,
		tmpDir:        tmpDir,
		defaultBranch: cleanBranch,
		resolver:      resolver,
	}, nil
}

// NewBatchGitContextFromLocal creates a BatchGitContext from an existing local
// repository. This is intended for testing — production code uses CloneRepository.
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
		tmpDir:        repoDir,
		defaultBranch: strings.TrimPrefix(defaultBranch, "refs/heads/"),
	}, nil
}

// RepoDir returns the filesystem path of the cloned repository.
func (c *BatchGitContext) RepoDir() string {
	return c.tmpDir
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

	if err = gitops.StageAll(c.workTree); err != nil {
		return false, fmt.Errorf("failed to stage changes: %w", err)
	}

	userConfig, err := gitops.ReadUserConfig(c.repo)
	if err != nil {
		logger.Warnf("Could not read git user config, using defaults: %v", err)
		userConfig = &gitops.UserConfig{}
	}

	name := userConfig.Name
	email := userConfig.Email
	if name == "" {
		name = "autoupdate[bot]"
	}
	if email == "" {
		email = "autoupdate[bot]@users.noreply.github.com"
	}

	localCfg, err := c.repo.Config()
	if err != nil {
		return false, fmt.Errorf("failed to read repo config: %w", err)
	}

	globalCfg, err := gitHelpers.GetGlobalGitConfig()
	if err != nil {
		logger.Warnf("Could not read global git config, using local only: %v", err)
		globalCfg = gitconfig.NewConfig()
	}

	gpgSign := gitHelpers.GetOptionFromConfig(localCfg, globalCfg, "commit", "gpgsign")
	signer, err := signingInfra.ResolveSignerFromGitConfig(
		gpgSign,
		userConfig.SigningFormat,
		userConfig.SigningKey,
		settings.GpgKeyPath,
		settings.GpgKeyPassphrase,
		"autoupdate",
		userConfig.SSHProgram,
	)
	if err != nil {
		return false, fmt.Errorf("failed to resolve commit signer: %w", err)
	}

	_, err = gitops.CommitChanges(c.repo, c.workTree, commitMessage, signer, name, email)
	if err != nil {
		return false, fmt.Errorf("failed to commit changes: %w", err)
	}

	refSpec := gitconfig.RefSpec(
		fmt.Sprintf("refs/heads/%s:refs/heads/%s", branchName, branchName),
	)

	if err = gitops.PushWithTransportDetection(c.repo, refSpec, authMethods); err != nil {
		return false, fmt.Errorf("failed to push branch %s: %w", branchName, err)
	}

	return true, nil
}

// StashChanges stashes all uncommitted changes (including untracked files)
// so that a force-checkout can switch branches without losing them.
// Returns true when a stash entry was actually created, false when the
// worktree was already clean. The caller must only call PopStash when
// this method returned true.
func (c *BatchGitContext) StashChanges() (bool, error) {
	cmd := exec.CommandContext(
		context.TODO(), "git", "stash", "push", "--include-untracked", "-m", "autoupdate-batch-stash",
	)
	cmd.Dir = c.tmpDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to stash changes: %w\nOutput: %s", err, string(output))
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Saved working directory") {
		return false, nil
	}

	// Record the stash ref so PopStash can verify it pops the right entry.
	refCmd := exec.CommandContext(context.TODO(), "git", "rev-parse", "stash@{0}")
	refCmd.Dir = c.tmpDir
	refOut, refErr := refCmd.CombinedOutput()
	if refErr != nil {
		return false, fmt.Errorf("failed to read stash ref: %w\nOutput: %s", refErr, string(refOut))
	}
	c.stashRef = strings.TrimSpace(string(refOut))

	return true, nil
}

// PopStash restores the stash entry created by StashChanges. It verifies
// that stash@{0} still matches the recorded ref before popping, to avoid
// restoring an unrelated stash entry.
func (c *BatchGitContext) PopStash() error {
	if c.stashRef != "" {
		refCmd := exec.CommandContext(context.TODO(), "git", "rev-parse", "stash@{0}")
		refCmd.Dir = c.tmpDir
		refOut, refErr := refCmd.CombinedOutput()
		if refErr != nil {
			return fmt.Errorf("failed to verify stash ref: %w\nOutput: %s", refErr, string(refOut))
		}
		currentRef := strings.TrimSpace(string(refOut))
		if currentRef != c.stashRef {
			return fmt.Errorf(
				"stash@{0} ref changed (expected %s, got %s); refusing to pop wrong stash",
				c.stashRef, currentRef,
			)
		}
	}

	cmd := exec.CommandContext(context.TODO(), "git", "stash", "pop")
	cmd.Dir = c.tmpDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to pop stash: %w\nOutput: %s", err, string(output))
	}
	c.stashRef = ""
	return nil
}

// DropStash drops the stash entry created by StashChanges without applying it.
// This is used on error paths to clean up leftover stash entries.
func (c *BatchGitContext) DropStash() {
	if c.stashRef == "" {
		return
	}
	cmd := exec.CommandContext(context.TODO(), "git", "stash", "drop")
	cmd.Dir = c.tmpDir
	_ = cmd.Run()
	c.stashRef = ""
}

// Close removes the temporary directory created during cloning.
func (c *BatchGitContext) Close() {
	if c.tmpDir != "" {
		_ = os.RemoveAll(c.tmpDir)
	}
}
