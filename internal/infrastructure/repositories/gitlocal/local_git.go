package gitlocal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport"
	logger "github.com/sirupsen/logrus"

	gitops "github.com/rios0rios0/gitforge/pkg/git/infrastructure"
	gitHelpers "github.com/rios0rios0/gitforge/pkg/git/infrastructure/helpers"
	globalEntities "github.com/rios0rios0/gitforge/pkg/global/domain/entities"
	signingInfra "github.com/rios0rios0/gitforge/pkg/signing/infrastructure"
	signingHelpers "github.com/rios0rios0/gitforge/pkg/signing/infrastructure/helpers"
)

// PushAuthResolver resolves authentication for git push operations.
// It abstracts the ProviderRegistry to avoid import cycles between the
// gitlocal package and the parent repositories package.
type PushAuthResolver interface {
	// GetAdapterByURL returns a LocalGitAuthProvider matching the URL, or nil.
	GetAdapterByURL(url string) globalEntities.LocalGitAuthProvider
	// GetAuthProvider creates a token-enabled provider instance and returns
	// it as a LocalGitAuthProvider for transport authentication.
	GetAuthProvider(name, token string) (globalEntities.LocalGitAuthProvider, error)
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
// The transport (SSH or HTTPS) is auto-detected from the remote URL.
// For SSH remotes, system SSH keys are used.  For HTTPS remotes, the
// authToken is used to create a token-enabled provider via the registry
// and collect auth methods.
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

	signer, err := resolveCommitSigner(c.repo, userConfig)
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

	if err = c.pushChanges(authToken, refSpec); err != nil {
		return false, fmt.Errorf("failed to push branch %s: %w", branchName, err)
	}

	return true, nil
}

// resolveCommitSigner reads git config to determine if and how commits
// should be signed. Returns nil if signing is not configured.
func resolveCommitSigner(
	repo *git.Repository,
	userConfig *gitops.UserConfig,
) (globalEntities.CommitSigner, error) {
	localCfg, err := repo.Config()
	if err != nil {
		return nil, fmt.Errorf("failed to read repo config: %w", err)
	}

	globalCfg, err := gitHelpers.GetGlobalGitConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to read global git config: %w", err)
	}

	gpgSign := gitHelpers.GetOptionFromConfig(localCfg, globalCfg, "commit", "gpgsign")
	if gpgSign != "true" {
		return nil, nil
	}

	switch {
	case userConfig.SigningFormat == "ssh":
		logger.Info("Signing commit with SSH key")
		sshKeyPath, sshErr := signingHelpers.ReadSSHSigningKey(userConfig.SigningKey)
		if sshErr != nil {
			return nil, sshErr
		}
		return signingInfra.NewSSHSigner(sshKeyPath), nil

	default:
		logger.Info("Signing commit with GPG key")
		gpgPassphrase := os.Getenv("GPG_PASSPHRASE")
		gpgKeyReader, gpgErr := signingHelpers.GetGpgKeyReader(
			context.Background(), userConfig.SigningKey, "", "autoupdate",
		)
		if gpgErr != nil {
			return nil, gpgErr
		}

		signKey, gpgErr := signingHelpers.GetGpgKey(gpgKeyReader, gpgPassphrase)
		if gpgErr != nil {
			return nil, gpgErr
		}
		return signingInfra.NewGPGSigner(signKey), nil
	}
}

// pushChanges detects the remote transport (SSH or HTTPS) from the
// origin URL and pushes using the appropriate method.
//
// For SSH remotes, it delegates to gitforge's PushChangesSSH which uses
// system SSH keys.  For HTTPS remotes, it creates a token-enabled
// provider via the registry, collects auth methods, and tries each
// until one succeeds.
func (c *LocalGitContext) pushChanges(authToken string, refSpec config.RefSpec) error {
	remoteCfg, err := c.repo.Remote("origin")
	if err != nil {
		return fmt.Errorf("failed to get origin remote: %w", err)
	}

	urls := remoteCfg.Config().URLs
	if len(urls) == 0 {
		return errors.New("origin remote has no URLs configured")
	}
	remoteURL := urls[0]

	switch {
	case strings.HasPrefix(remoteURL, "git@"):
		logger.Info("Pushing to remote via SSH")
		return gitops.PushChangesSSH(c.repo, refSpec)

	case strings.HasPrefix(remoteURL, "https://") || strings.HasPrefix(remoteURL, "http://"):
		return c.pushChangesHTTPS(remoteURL, authToken, refSpec)

	default:
		return fmt.Errorf("unsupported remote URL scheme: %s", remoteURL)
	}
}

// pushChangesHTTPS resolves the provider from the remote URL, creates a
// token-enabled provider instance, and pushes with auth method retry.
func (c *LocalGitContext) pushChangesHTTPS(remoteURL, authToken string, refSpec config.RefSpec) error {
	if c.resolver == nil {
		return errors.New("push auth resolver is required for HTTPS push")
	}

	adapter := c.resolver.GetAdapterByURL(remoteURL)
	if adapter == nil {
		return fmt.Errorf("no registered provider matches remote URL: %s", remoteURL)
	}

	serviceType := adapter.GetServiceType()
	logger.Infof("Pushing to remote via HTTPS (service: %v)", serviceType)

	authMethods := c.collectAuthMethods(serviceType, authToken)
	if len(authMethods) == 0 {
		return fmt.Errorf("no auth methods available for service type %v", serviceType)
	}

	var lastErr error
	for _, method := range authMethods {
		lastErr = c.repo.Push(&git.PushOptions{
			RefSpecs:   []config.RefSpec{refSpec},
			RemoteName: "origin",
			Auth:       method,
		})
		if lastErr == nil {
			return nil
		}
		logger.Debugf("Push attempt failed with auth method %T: %v", method, lastErr)
	}

	return fmt.Errorf("all push attempts failed, last error: %w", lastErr)
}

// collectAuthMethods creates a token-enabled provider for the given
// service type and collects all available auth methods from it.
func (c *LocalGitContext) collectAuthMethods(
	serviceType globalEntities.ServiceType,
	authToken string,
) []transport.AuthMethod {
	providerName := serviceTypeToProviderName(serviceType)
	if providerName == "" {
		return nil
	}

	lgap, err := c.resolver.GetAuthProvider(providerName, authToken)
	if err != nil {
		logger.Warnf("Failed to create auth provider %s: %v", providerName, err)
		return nil
	}

	lgap.ConfigureTransport()
	return lgap.GetAuthMethods("")
}

// serviceTypeToProviderName maps a gitforge ServiceType to the provider
// name string used for registry lookups.
func serviceTypeToProviderName(serviceType globalEntities.ServiceType) string {
	providerNames := map[globalEntities.ServiceType]string{
		globalEntities.GITHUB:      "github",
		globalEntities.GITLAB:      "gitlab",
		globalEntities.AZUREDEVOPS: "azuredevops",
	}
	return providerNames[serviceType]
}
