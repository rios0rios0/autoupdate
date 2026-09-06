package support

import (
	"os"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// refsHeadsPrefix is how the provider APIs spell a branch ref: a repository's
// DefaultBranch arrives carrying it and the pull request input expects it.
const refsHeadsPrefix = "refs/heads/"

// CloneTarget identifies the repository a clone-based upgrade script works on
// and how the script authenticates against it. The script-driven updaters
// embed it in their parameters, so the fields every one of them needs are
// declared -- and turned into environment variables -- exactly once.
type CloneTarget struct {
	CloneURL      string
	DefaultBranch string
	BranchName    string
	AuthToken     string
	ProviderName  string
	// Changelog is the staged changelog payload the script copies into the
	// clone; an empty value leaves the repository's changelog untouched.
	Changelog StagedChangelog
}

// CloneTargetFor describes the clone of repo on provider for a run that pushes
// branchName and copies changelog into it.
func CloneTargetFor(
	provider repositories.ProviderRepository,
	repo entities.Repository,
	branchName string,
	changelog StagedChangelog,
) CloneTarget {
	return CloneTarget{
		CloneURL:      provider.CloneURL(repo),
		DefaultBranch: strings.TrimPrefix(repo.DefaultBranch, refsHeadsPrefix),
		BranchName:    branchName,
		AuthToken:     provider.AuthToken(),
		ProviderName:  provider.Name(),
		Changelog:     changelog,
	}
}

// RemoteCloneScript returns the bash that turns a fresh working directory into
// a clone of the repository on its upgrade branch: a fallback identity for the
// commit, a shallow clone of the default branch and the checkout of the
// upgrade branch. It reads CLONE_URL, DEFAULT_BRANCH, REPO_DIR and BRANCH_NAME
// from the environment CloneEnv builds and leaves the shell inside the clone,
// which is where the language-specific commands that follow expect to run.
func RemoteCloneScript() string {
	return `# Ensure git user identity is configured
if ! git config --global user.name > /dev/null 2>&1; then
    git config --global user.name "autoupdate[bot]"
fi
if ! git config --global user.email > /dev/null 2>&1; then
    git config --global user.email "autoupdate[bot]@users.noreply.github.com"
fi

echo "Cloning repository..."
git clone --depth=1 --branch "$DEFAULT_BRANCH" "$CLONE_URL" "$REPO_DIR" 2>&1
cd "$REPO_DIR"

git checkout -b "$BRANCH_NAME" 2>&1

`
}

// CloneEnv returns the environment every clone-based upgrade script starts
// from: the process environment, the clone target, the token under both names
// the credential rewrite and repository-specific scripts read it by, and the
// staged changelog. The caller appends its language-specific variables.
func CloneEnv(target CloneTarget, repoDir string) []string {
	env := append(os.Environ(),
		"AUTH_TOKEN="+target.AuthToken,
		"GIT_HTTPS_TOKEN="+target.AuthToken,
		"CLONE_URL="+target.CloneURL,
		"BRANCH_NAME="+target.BranchName,
		"REPO_DIR="+repoDir,
		"DEFAULT_BRANCH="+target.DefaultBranch,
	)

	return append(env, target.Changelog.Env()...)
}
