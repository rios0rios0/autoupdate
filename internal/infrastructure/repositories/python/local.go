package python

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
	HasChanges           bool
	PythonVersionUpdated bool
	LatestVersion        string
	BranchName           string
	Toolchain            string // dependency manager used: "pdm" or "pip"
	Output               string
}

// RunLocalUpgrade runs the Python dependency upgrade directly in a local
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

	// The dependency manager is resolved once, from the manifests the
	// repository carries before anything is upgraded, and that single value
	// drives both the commands that run and what the run reports.
	project := detectLocalProject(repoDir)

	if opts.DryRun {
		return handleDryRun(vCtx, repoDir, project), nil
	}

	return executeLocalUpgrade(ctx, repoDir, vCtx, project, opts)
}

// resolveLocalVersionContext fetches the latest Python version and compares
// it against the local .python-version to build a versionContext.
func resolveLocalVersionContext(ctx context.Context, repoDir string) *versionContext {
	fetcher := NewHTTPPythonVersionFetcher(&http.Client{Timeout: pyVersionTimeout})
	latestPyVersion, err := fetcher.FetchLatestVersion(ctx)
	if err != nil {
		logger.Warnf("[python] Failed to fetch latest Python version: %v (continuing without version upgrade)", err)
		latestPyVersion = ""
	} else {
		logger.Infof("[python] Latest stable Python version: %s", latestPyVersion)
	}

	needsVersionUpgrade := false
	if latestPyVersion != "" {
		pyVersionContent, readErr := os.ReadFile(filepath.Join(repoDir, ".python-version"))
		if readErr == nil {
			currentVersion := parsePythonVersionFile(string(pyVersionContent))
			needsVersionUpgrade = currentVersion != "" && currentVersion != latestPyVersion
			logger.Infof(
				"[python] Current .python-version: %s (upgrade needed: %v)",
				currentVersion, needsVersionUpgrade,
			)
		}
	}

	branchName := branchPyDepsFmt
	if needsVersionUpgrade {
		branchName = fmt.Sprintf(branchPyVersionFmt, latestPyVersion)
	}

	return &versionContext{
		LatestVersion:       latestPyVersion,
		NeedsVersionUpgrade: needsVersionUpgrade,
		BranchName:          branchName,
	}
}

// handleDryRun logs the planned action and returns a result without
// executing the upgrade.
func handleDryRun(vCtx *versionContext, repoDir string, project pythonProject) *LocalResult {
	if vCtx.NeedsVersionUpgrade {
		logger.Infof(
			"[python] [DRY RUN] Would upgrade Python to %s and update deps in %s (using %s)",
			vCtx.LatestVersion, repoDir, project.Toolchain(),
		)
	} else {
		logger.Infof(
			"[python] [DRY RUN] Would update Python dependencies in %s (using %s)",
			repoDir, project.Toolchain(),
		)
	}
	return &LocalResult{
		LatestVersion:        vCtx.LatestVersion,
		BranchName:           vCtx.BranchName,
		PythonVersionUpdated: vCtx.NeedsVersionUpgrade,
		Toolchain:            project.Toolchain(),
	}
}

// executeLocalUpgrade performs the actual upgrade using go-git for
// branch/commit/push operations and a bash script only for the
// language-specific upgrade commands (pip install, pyproject updates, etc.).
func executeLocalUpgrade(
	ctx context.Context,
	repoDir string,
	vCtx *versionContext,
	project pythonProject,
	opts LocalUpgradeOptions,
) (*LocalResult, error) {
	// --- Git Setup (go-git) ---
	gitCtx, restore, err := gitlocal.PrepareBranch(repoDir, vCtx.BranchName, opts.PushAuth)
	if err != nil {
		return nil, err
	}
	defer restore()

	// --- Language Operations (bash) ---
	outputStr, runErr := runLanguageUpgradeScript(ctx, repoDir, vCtx, project, opts)
	if runErr != nil {
		return nil, runErr
	}

	pythonVersionUpdated := strings.Contains(outputStr, "PYTHON_VERSION_UPDATED=true")

	// --- Git Finalize (go-git) ---
	commitMsg := pyCommitMsgDeps
	if pythonVersionUpdated {
		commitMsg = fmt.Sprintf(
			"chore(deps): upgraded Python to `%s` and updated all dependencies",
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
		HasChanges:           pushed,
		PythonVersionUpdated: pythonVersionUpdated,
		LatestVersion:        vCtx.LatestVersion,
		BranchName:           vCtx.BranchName,
		Toolchain:            project.Toolchain(),
		Output:               outputStr,
	}, nil
}

// runLanguageUpgradeScript builds and executes the bash script that
// performs Python-specific upgrade operations (pip install, pyproject
// updates, Dockerfile updates, changelog updates).
func runLanguageUpgradeScript(
	ctx context.Context,
	repoDir string,
	vCtx *versionContext,
	project pythonProject,
	opts LocalUpgradeOptions,
) (string, error) {
	changelog := support.StageLocalChangelog(repoDir, changelogEntries(vCtx))
	defer changelog.Remove()

	pythonBinary, err := findPythonBinary()
	if err != nil {
		return "", fmt.Errorf("python binary not found: %w", err)
	}

	params := localUpgradeParams{
		BranchName:    vCtx.BranchName,
		PythonVersion: vCtx.LatestVersion,
		Changelog:     changelog,
		AuthToken:     opts.AuthToken,
		ProviderName:  opts.ProviderName,
		Project:       project,
		PythonBinary:  pythonBinary,
	}

	return cmdrunner.RunScript(ctx, localCmdRunner, cmdrunner.ScriptRun{
		Body:        buildLocalUpgradeScript(params),
		TempPattern: "autoupdate-python-local-*",
		Dir:         repoDir,
		Env:         buildLocalEnv(params),
		LogPrefix:   "python",
		Verbose:     opts.Verbose,
	})
}

// --- local-mode internal types & helpers ---

type localUpgradeParams struct {
	BranchName    string
	PythonVersion string
	// Changelog is the staged changelog payload the script copies into
	// the clone; an empty value leaves the repository's changelog untouched.
	Changelog    support.StagedChangelog
	AuthToken    string
	ProviderName string
	// Project carries the manifests the repository has and the dependency
	// manager selected from them.
	Project      pythonProject
	PythonBinary string
}

// buildLocalUpgradeScript builds a bash script that performs only the
// language-specific upgrade operations (auth, pip install, pyproject
// updates, Dockerfile updates, changelog updates). Git operations
// (branch creation, staging, committing, pushing) are handled by
// LocalGitContext.
func buildLocalUpgradeScript(params localUpgradeParams) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	// Set up git credentials when an auth token is available
	writeLocalAuth(&sb, params)

	// Python upgrade commands (reuse remote-mode helpers)
	writePythonUpgradeCommands(&sb, upgradeParams{Project: params.Project})

	// Keep generated build metadata out of the commit
	writeEggInfoGitignore(&sb)

	// Update Dockerfile python image tags
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
		"PYTHON_BINARY="+params.PythonBinary,
	)
	if params.PythonVersion != "" {
		env = append(env, "PYTHON_VERSION="+params.PythonVersion)
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
