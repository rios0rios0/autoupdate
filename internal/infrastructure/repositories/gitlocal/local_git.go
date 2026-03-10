package gitlocal

import (
	"errors"
	"fmt"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport"
	logger "github.com/sirupsen/logrus"

	gitops "github.com/rios0rios0/gitforge/pkg/git/infrastructure"
	gitHelpers "github.com/rios0rios0/gitforge/pkg/git/infrastructure/helpers"
	globalEntities "github.com/rios0rios0/gitforge/pkg/global/domain/entities"
	signingInfra "github.com/rios0rios0/gitforge/pkg/signing/infrastructure"
)

// PushAuthResolver resolves authentication for git push operations.
// It abstracts the ProviderRegistry to avoid import cycles between the
// gitlocal package and the parent repositories package.
type PushAuthResolver interface {
	// GetAdapterByURL returns a LocalGitAuthProvider matching the URL, or nil.
	GetAdapterByURL(url string) globalEntities.LocalGitAuthProvider
	// GetAuthProvider creates a token-enabled provider for the given service
	// type and returns it as a LocalGitAuthProvider for transport authentication.
	// The implementation is responsible for mapping ServiceType to the internal
	// provider name.
	GetAuthProvider(serviceType globalEntities.ServiceType, token string) (globalEntities.LocalGitAuthProvider, error)
}

// LocalGitContext wraps go-git repository and worktree objects, providing
// high-level git operations for the local upgrade workflow.  It replaces
// the bash-generated git setup/finalize steps (clean check, branch
// creation, staging, committing, and pushing) with pure Go equivalents
// backed by gitforge.
type LocalGitContext struct {
	repo     *git.Repository
	workTree *git.Worktree
	repoDir  string
	resolver PushAuthResolver
}

// NewLocalGitContext opens the repository at the given path and returns
// a ready-to-use context.  The caller should use EnsureClean, then
// CreateBranch, run language-specific upgrades, and finally call
// StageCommitAndPush.
//
// The resolver is used to resolve auth methods for pushing.  It may be
// nil when push is not needed (e.g. in tests that only exercise local
// git operations).
func NewLocalGitContext(repoDir string, resolver PushAuthResolver) (*LocalGitContext, error) {
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
		resolver: resolver,
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
// and pushes the branch to the remote.
//
// The transport (SSH or HTTPS) is auto-detected from the remote URL via
// gitforge's PushWithTransportDetection.  For SSH remotes, system SSH
// keys are used.  For HTTPS remotes, the authToken is used to create a
// token-enabled provider via the registry and collect auth methods.
//
// If the repository's git config has commit.gpgsign=true, the commit
// will be signed using GPG or SSH depending on gpg.format.
//
// Returns true when changes were committed and pushed, false when
// the worktree was clean (nothing to push).
func (c *LocalGitContext) StageCommitAndPush(
	branchName, commitMessage, authToken string,
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
		return false, fmt.Errorf("failed to read git user config: %w", err)
	}

	name := userConfig.Name
	email := userConfig.Email
	if name == "" {
		name = "autoupdate"
	}
	if email == "" {
		email = "autoupdate@noreply"
	}

	localCfg, err := c.repo.Config()
	if err != nil {
		return false, fmt.Errorf("failed to read repo config: %w", err)
	}

	globalCfg, err := gitHelpers.GetGlobalGitConfig()
	if err != nil {
		return false, fmt.Errorf("failed to read global git config: %w", err)
	}

	gpgSign := gitHelpers.GetOptionFromConfig(localCfg, globalCfg, "commit", "gpgsign")
	signer, err := signingInfra.ResolveSignerFromGitConfig(
		gpgSign,
		userConfig.SigningFormat,
		userConfig.SigningKey,
		"",
		os.Getenv("GPG_PASSPHRASE"),
		"autoupdate",
	)
	if err != nil {
		return false, fmt.Errorf("failed to resolve commit signer: %w", err)
	}

	_, err = gitops.CommitChanges(c.repo, c.workTree, commitMessage, signer, name, email)
	if err != nil {
		return false, fmt.Errorf("failed to commit changes: %w", err)
	}

	refSpec := config.RefSpec(
		fmt.Sprintf("refs/heads/%s:refs/heads/%s", branchName, branchName),
	)

	authMethods, err := c.collectAuthMethods(authToken)
	if err != nil {
		return false, fmt.Errorf("failed to collect auth methods: %w", err)
	}

	if err = gitops.PushWithTransportDetection(c.repo, refSpec, authMethods); err != nil {
		return false, fmt.Errorf("failed to push branch %s: %w", branchName, err)
	}

	return true, nil
}

// collectAuthMethods resolves the remote URL, finds the matching provider,
// and collects all available auth methods for push.  Returns nil (no auth
// methods) when the resolver is nil, which is fine for SSH push where auth
// methods are not needed.
func (c *LocalGitContext) collectAuthMethods(authToken string) ([]transport.AuthMethod, error) {
	if c.resolver == nil {
		return nil, nil
	}

	remoteCfg, err := c.repo.Remote("origin")
	if err != nil {
		return nil, fmt.Errorf("failed to get origin remote: %w", err)
	}

	urls := remoteCfg.Config().URLs
	if len(urls) == 0 {
		return nil, nil
	}

	adapter := c.resolver.GetAdapterByURL(urls[0])
	if adapter == nil {
		return nil, nil
	}

	serviceType := adapter.GetServiceType()
	lgap, err := c.resolver.GetAuthProvider(serviceType, authToken)
	if err != nil {
		logger.Warnf("Failed to create auth provider for %v: %v", serviceType, err)
		return nil, nil
	}

	lgap.ConfigureTransport()
	return lgap.GetAuthMethods(""), nil
}
