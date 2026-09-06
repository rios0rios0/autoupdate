package gitlocal

import (
	"fmt"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport"
	logger "github.com/sirupsen/logrus"

	gitops "github.com/rios0rios0/gitforge/pkg/git/infrastructure"
	gitHelpers "github.com/rios0rios0/gitforge/pkg/git/infrastructure/helpers"
	signingInfra "github.com/rios0rios0/gitforge/pkg/signing/infrastructure"
)

// signingIdentity is the program name the signer reports itself under.
const signingIdentity = "autoupdate"

// signedCommit is what the two contexts -- the local clone and the batch one --
// disagree about when they stage, sign and commit a worktree. Everything else
// about the operation is the same, so it lives in [commitAllSigned] and only
// these four values are supplied by the caller.
type signedCommit struct {
	// DefaultName and DefaultEmail author the commit when the repository's own
	// git config names nobody. The two contexts sign as different identities.
	DefaultName  string
	DefaultEmail string

	// GPGKeyPath and GPGPassphrase reach the signer. The batch context takes
	// them from the run's settings; the local one only has the environment.
	GPGKeyPath    string
	GPGPassphrase string
}

// commitAllSigned stages every change in the worktree and commits it, signed
// when the repository's git config asks for a signature.
//
// The author falls back to spec's identity only for what the config leaves
// blank, so a repository that names a committer keeps it. The signing decision
// comes from commit.gpgsign read across the local and global configs, and a
// global config that cannot be read is treated as empty rather than fatal --
// an unsigned commit is better than an abandoned upgrade.
func commitAllSigned(
	repo *git.Repository,
	workTree *git.Worktree,
	commitMessage string,
	spec signedCommit,
) error {
	if err := gitops.StageAll(workTree); err != nil {
		return fmt.Errorf("failed to stage changes: %w", err)
	}

	userConfig, err := gitops.ReadUserConfig(repo)
	if err != nil {
		logger.Warnf("Could not read git user config, using defaults: %v", err)
		userConfig = &gitops.UserConfig{}
	}

	name := userConfig.Name
	if name == "" {
		name = spec.DefaultName
	}
	email := userConfig.Email
	if email == "" {
		email = spec.DefaultEmail
	}

	localCfg, err := repo.Config()
	if err != nil {
		return fmt.Errorf("failed to read repo config: %w", err)
	}

	globalCfg, err := gitHelpers.GetGlobalGitConfig()
	if err != nil {
		logger.Warnf("Could not read global git config, using local only: %v", err)
		globalCfg = gitconfig.NewConfig()
	}

	signer, err := signingInfra.ResolveSignerFromGitConfig(
		gitHelpers.GetOptionFromConfig(localCfg, globalCfg, "commit", "gpgsign"),
		userConfig.SigningFormat,
		userConfig.SigningKey,
		spec.GPGKeyPath,
		spec.GPGPassphrase,
		signingIdentity,
		userConfig.SSHProgram,
	)
	if err != nil {
		return fmt.Errorf("failed to resolve commit signer: %w", err)
	}

	if _, err = gitops.CommitChanges(repo, workTree, commitMessage, signer, name, email); err != nil {
		return fmt.Errorf("failed to commit changes: %w", err)
	}

	return nil
}

// pushBranch pushes branchName to origin under the same name, letting gitforge
// pick the transport from the remote URL.
func pushBranch(
	repo *git.Repository,
	branchName string,
	authMethods []transport.AuthMethod,
) error {
	refSpec := gitconfig.RefSpec(
		fmt.Sprintf("refs/heads/%s:refs/heads/%s", branchName, branchName),
	)

	if err := gitops.PushWithTransportDetection(repo, refSpec, authMethods); err != nil {
		return fmt.Errorf("failed to push branch %s: %w", branchName, err)
	}

	return nil
}
