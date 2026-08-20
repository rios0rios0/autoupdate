package javascript

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlocal"
	"github.com/rios0rios0/autoupdate/internal/support"
)

// localCmdRunner is the package-level command runner for local-mode upgrade scripts.
var localCmdRunner cmdrunner.Runner = cmdrunner.NewDefaultRunner() //nolint:gochecknoglobals // test override

// LocalUpgradeOptions holds options for the local (standalone) upgrade mode.
type LocalUpgradeOptions struct {
	DryRun       bool
	Verbose      bool
	AuthToken    string
	ProviderName string                    // git provider name (e.g. "azuredevops", "github", "gitlab")
	PushAuth     gitlocal.PushAuthResolver // resolves auth methods for git push
}

// LocalResult holds the outcome of a local upgrade operation.
type LocalResult struct {
	HasChanges         bool
	NodeVersionUpdated bool
	LatestVersion      string
	BranchName         string
	PackageManager     string
	Output             string
}

// RunLocalUpgrade runs the JavaScript dependency upgrade directly in a local
// repository directory. Unlike CreateUpdatePRs it does not clone the
// repository and does not set up git credentials — it relies on the
// user's existing checkout and credential configuration.
func RunLocalUpgrade(
	ctx context.Context,
	repoDir string,
	opts LocalUpgradeOptions,
) (*LocalResult, error) {
	if opts.Verbose {
		logger.SetLevel(logger.DebugLevel)
	}

	vCtx := resolveLocalVersionContext(ctx, repoDir)

	pkgMgr := detectLocalPackageManager(repoDir)

	if opts.DryRun {
		return handleDryRunLocal(vCtx, repoDir, pkgMgr), nil
	}

	return executeLocalUpgrade(ctx, repoDir, vCtx, pkgMgr, opts)
}

// resolveLocalVersionContext fetches the latest Node.js version and compares
// it against the local .nvmrc or .node-version to build a versionContext.
func resolveLocalVersionContext(ctx context.Context, repoDir string) *versionContext {
	fetcher := NewHTTPNodeVersionFetcher(&http.Client{Timeout: nodeVersionTimeout})
	latestNodeVersion, err := fetcher.FetchLatestVersion(ctx)
	if err != nil {
		logger.Warnf(
			"[javascript] Failed to fetch latest Node.js version: %v (continuing without version upgrade)",
			err,
		)
		latestNodeVersion = ""
	} else {
		logger.Infof("[javascript] Latest Node.js LTS version: %s", latestNodeVersion)
	}

	needsVersionUpgrade := false
	if latestNodeVersion != "" {
		currentVersion := readLocalNodeVersion(repoDir)
		if currentVersion != "" {
			needsVersionUpgrade = support.IsNewerVersion(currentVersion, latestNodeVersion)
			logger.Infof(
				"[javascript] Current Node.js version: %s (upgrade needed: %v)",
				currentVersion, needsVersionUpgrade,
			)
		}
	}

	branchName := branchJSDepsFmt
	if needsVersionUpgrade {
		branchName = fmt.Sprintf(branchNodeVersionFmt, latestNodeVersion)
	}

	return &versionContext{
		LatestVersion:       latestNodeVersion,
		NeedsVersionUpgrade: needsVersionUpgrade,
		BranchName:          branchName,
	}
}

// readLocalNodeVersion reads the Node.js version from .nvmrc or .node-version
// files in the local repository.
func readLocalNodeVersion(repoDir string) string {
	for _, versionFile := range []string{".nvmrc", ".node-version"} {
		content, err := os.ReadFile(filepath.Join(repoDir, versionFile))
		if err == nil {
			version := parseNodeVersionFile(string(content))
			if version != "" {
				return version
			}
		}
	}
	return ""
}

// detectLocalPackageManager determines which package manager the local
// repository uses by checking for lockfiles.
func detectLocalPackageManager(repoDir string) string {
	if _, err := os.Stat(filepath.Join(repoDir, "pnpm-lock.yaml")); err == nil {
		return pkgMgrPnpm
	}
	if _, err := os.Stat(filepath.Join(repoDir, "yarn.lock")); err == nil {
		return pkgMgrYarn
	}
	return pkgMgrNpm
}

// handleDryRunLocal logs the planned action and returns a result without
// executing the upgrade.
func handleDryRunLocal(vCtx *versionContext, repoDir, pkgMgr string) *LocalResult {
	if vCtx.NeedsVersionUpgrade {
		logger.Infof(
			"[javascript] [DRY RUN] Would upgrade Node.js to %s and update deps in %s (using %s)",
			vCtx.LatestVersion, repoDir, pkgMgr,
		)
	} else {
		logger.Infof(
			"[javascript] [DRY RUN] Would update JavaScript dependencies in %s (using %s)",
			repoDir, pkgMgr,
		)
	}
	return &LocalResult{
		LatestVersion:      vCtx.LatestVersion,
		BranchName:         vCtx.BranchName,
		NodeVersionUpdated: vCtx.NeedsVersionUpgrade,
		PackageManager:     pkgMgr,
	}
}

// executeLocalUpgrade performs the actual upgrade using go-git for
// branch/commit/push operations and a bash script only for the
// language-specific upgrade commands (npm/yarn/pnpm install, etc.).
func executeLocalUpgrade(
	ctx context.Context,
	repoDir string,
	vCtx *versionContext,
	pkgMgr string,
	opts LocalUpgradeOptions,
) (*LocalResult, error) {
	// --- Git Setup (go-git) ---
	gitCtx, restore, err := gitlocal.PrepareBranch(repoDir, vCtx.BranchName, opts.PushAuth)
	if err != nil {
		return nil, err
	}
	defer restore()

	// --- Language Operations (bash) ---
	// The changelog is staged here rather than inside the script runner so this
	// function, which decides whether to keep the run, can also undo it.
	changelog := support.StageLocalChangelog(repoDir, changelogEntries(vCtx))
	defer changelog.Remove()

	outputStr, runErr := runLanguageUpgradeScript(ctx, repoDir, vCtx, pkgMgr, opts, changelog)
	if runErr != nil {
		return nil, runErr
	}

	nodeVersionUpdated := strings.Contains(outputStr, "NODE_VERSION_UPDATED=true")

	// Skip when the only change is a cosmetic lockfile version sync.
	if hasOnlyLockfileVersionChanges(ctx, repoDir) {
		logger.Infof(
			"[javascript] Only cosmetic lockfile version changes detected (project version sync), skipping",
		)
		revertWorkingTreeChanges(ctx, repoDir, changelog)
		return &LocalResult{
			HasChanges:     false,
			LatestVersion:  vCtx.LatestVersion,
			BranchName:     vCtx.BranchName,
			PackageManager: pkgMgr,
		}, nil
	}

	// --- Git Finalize (go-git) ---
	commitMsg := jsCommitMsgDeps
	if nodeVersionUpdated {
		commitMsg = fmt.Sprintf(
			"chore(deps): upgraded Node.js to `%s` and updated all dependencies",
			vCtx.LatestVersion,
		)
	}

	pushed, pushErr := gitCtx.StageCommitAndPush(
		vCtx.BranchName, commitMsg, opts.AuthToken,
	)
	if pushErr != nil {
		return nil, pushErr
	}

	return &LocalResult{
		HasChanges:         pushed,
		NodeVersionUpdated: nodeVersionUpdated,
		LatestVersion:      vCtx.LatestVersion,
		BranchName:         vCtx.BranchName,
		PackageManager:     pkgMgr,
		Output:             outputStr,
	}, nil
}

// runLanguageUpgradeScript builds and executes the bash script that
// performs JavaScript-specific upgrade operations (npm/yarn/pnpm install,
// Dockerfile updates, changelog updates).
func runLanguageUpgradeScript(
	ctx context.Context,
	repoDir string,
	vCtx *versionContext,
	pkgMgr string,
	opts LocalUpgradeOptions,
	changelog support.StagedChangelog,
) (string, error) {
	params := localUpgradeParams{
		BranchName:     vCtx.BranchName,
		NodeVersion:    vCtx.LatestVersion,
		Changelog:      changelog,
		AuthToken:      opts.AuthToken,
		ProviderName:   opts.ProviderName,
		PackageManager: pkgMgr,
	}

	return cmdrunner.RunScript(ctx, localCmdRunner, cmdrunner.ScriptRun{
		Body:        buildLocalUpgradeScript(params),
		TempPattern: "autoupdate-js-local-*",
		Dir:         repoDir,
		Env:         buildLocalEnv(params),
		LogPrefix:   "javascript",
		Verbose:     opts.Verbose,
	})
}

// --- local-mode internal types & helpers ---

type localUpgradeParams struct {
	BranchName  string
	NodeVersion string
	// Changelog is the staged changelog payload the script copies into
	// the clone; an empty value leaves the repository's changelog untouched.
	Changelog      support.StagedChangelog
	AuthToken      string
	ProviderName   string
	PackageManager string
}

// buildLocalUpgradeScript builds a bash script that performs only the
// language-specific upgrade operations (auth, npm/yarn/pnpm install,
// Dockerfile updates, changelog updates). Git operations (branch
// creation, staging, committing, pushing) are handled by LocalGitContext.
func buildLocalUpgradeScript(params localUpgradeParams) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	// Set up git credentials when an auth token is available
	writeLocalAuth(&sb, params)

	// JavaScript upgrade commands (reuse remote-mode helpers)
	writeJSUpgradeCommands(&sb, upgradeParams{
		PackageManager: params.PackageManager,
	})

	// Update Dockerfile node image tags
	writeDockerfileUpdate(&sb)

	// Changelog update
	sb.WriteString(support.ChangelogUpdateScript())

	return sb.String()
}

// writeLocalAuth adds credential setup to the script when a token is
// provided.
func writeLocalAuth(sb *strings.Builder, params localUpgradeParams) {
	if params.AuthToken == "" {
		return
	}

	sb.WriteString(support.GitAuthScript(params.ProviderName))
}

// buildLocalEnv returns the environment for the local upgrade script.
func buildLocalEnv(params localUpgradeParams) []string {
	env := append(os.Environ(),
		"BRANCH_NAME="+params.BranchName,
		"PACKAGE_MANAGER="+params.PackageManager,
	)
	if params.NodeVersion != "" {
		env = append(env, "NODE_VERSION="+params.NodeVersion)
	}
	if params.AuthToken != "" {
		env = append(env,
			"AUTH_TOKEN="+params.AuthToken,
			"GIT_HTTPS_TOKEN="+params.AuthToken,
		)
	}
	env = append(env, params.Changelog.Env()...)
	return env
}
