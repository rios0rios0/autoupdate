package dart

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlocal"
	"github.com/rios0rios0/autoupdate/internal/support"
	langDart "github.com/rios0rios0/langforge/pkg/infrastructure/languages/dart"
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
	HasChanges    bool
	SDKUpdated    bool
	LatestVersion string
	Toolchain     string
	BranchName    string
	Output        string
}

// RunLocalUpgrade runs the pub dependency upgrade directly in a local repository
// directory. Unlike CreateUpdatePRs it does not clone the repository and does
// not set up git credentials — it relies on the user's existing checkout and
// credential configuration.
func RunLocalUpgrade(
	ctx context.Context,
	repoDir string,
	opts LocalUpgradeOptions,
) (*LocalResult, error) {
	if opts.Verbose {
		logger.SetLevel(logger.DebugLevel)
	}

	vCtx := newUpdaterRepository().resolveLocalVersionContext(ctx, repoDir)

	if opts.DryRun {
		return handleDryRun(vCtx, repoDir), nil
	}

	return executeLocalUpgrade(ctx, repoDir, vCtx, opts)
}

// resolveLocalVersionContext picks the toolchain from the local pubspec.yaml and
// compares the local .fvmrc pin against the latest stable SDK.
func (u *UpdaterRepository) resolveLocalVersionContext(ctx context.Context, repoDir string) *versionContext {
	toolchain := toolchainDart
	if langDart.IsFlutter(repoDir) {
		toolchain = toolchainFlutter
	}

	latest := u.fetchLatestSDK(ctx, toolchain)

	currentPin := ""
	if latest != "" {
		if content, err := os.ReadFile(filepath.Join(repoDir, FvmConfigFile)); err == nil {
			currentPin = ParseFvmVersion(string(content))
		}
	}

	return newVersionContext(toolchain, latest, currentPin)
}

// handleDryRun logs the planned action and returns a result without executing
// the upgrade.
func handleDryRun(vCtx *versionContext, repoDir string) *LocalResult {
	if vCtx.NeedsVersionUpgrade {
		logger.Infof(
			"[dart] [DRY RUN] Would upgrade Flutter to %s and update pub dependencies in %s",
			vCtx.LatestVersion, repoDir,
		)
	} else {
		logger.Infof(
			"[dart] [DRY RUN] Would update pub dependencies in %s with %s",
			repoDir, vCtx.Toolchain,
		)
	}
	return &LocalResult{
		LatestVersion: vCtx.LatestVersion,
		Toolchain:     vCtx.Toolchain,
		BranchName:    vCtx.BranchName,
		SDKUpdated:    vCtx.NeedsVersionUpgrade,
	}
}

// executeLocalUpgrade performs the upgrade using go-git for branch/commit/push
// and a bash script only for the pub commands.
func executeLocalUpgrade(
	ctx context.Context,
	repoDir string,
	vCtx *versionContext,
	opts LocalUpgradeOptions,
) (*LocalResult, error) {
	// --- Git Setup (go-git) ---
	gitCtx, restore, err := gitlocal.PrepareBranch(repoDir, vCtx.BranchName, opts.PushAuth)
	if err != nil {
		return nil, err
	}
	defer restore()

	// --- Language Operations ---
	sdkUpdated := applyFvmPin(repoDir, vCtx)

	outputStr, runErr := runLanguageUpgradeScript(ctx, repoDir, vCtx, opts)
	if runErr != nil {
		return nil, runErr
	}

	// --- Git Finalize (go-git) ---
	pushed, pushErr := gitCtx.StageCommitAndPush(
		vCtx.BranchName, commitMessage(vCtx, sdkUpdated), opts.AuthToken,
	)
	if pushErr != nil {
		return nil, pushErr
	}

	return &LocalResult{
		HasChanges:    pushed,
		SDKUpdated:    sdkUpdated,
		LatestVersion: vCtx.LatestVersion,
		Toolchain:     vCtx.Toolchain,
		BranchName:    vCtx.BranchName,
		Output:        outputStr,
	}, nil
}

// runLanguageUpgradeScript builds and executes the bash script that performs the
// pub upgrade and the changelog update.
func runLanguageUpgradeScript(
	ctx context.Context,
	repoDir string,
	vCtx *versionContext,
	opts LocalUpgradeOptions,
) (string, error) {
	changelog := support.StageLocalChangelog(repoDir, changelogEntries(vCtx, vCtx.NeedsVersionUpgrade))
	defer changelog.Remove()

	params := localUpgradeParams{
		BranchName:   vCtx.BranchName,
		Toolchain:    vCtx.Toolchain,
		Changelog:    changelog,
		AuthToken:    opts.AuthToken,
		ProviderName: opts.ProviderName,
	}

	return cmdrunner.RunScript(ctx, localCmdRunner, cmdrunner.ScriptRun{
		Body:        buildLocalUpgradeScript(params),
		TempPattern: "autoupdate-dart-local-*",
		Dir:         repoDir,
		Env:         buildLocalEnv(params),
		LogPrefix:   "dart",
		Verbose:     opts.Verbose,
	})
}

// --- local-mode internal types & helpers ---

type localUpgradeParams struct {
	BranchName string
	Toolchain  string
	// Changelog is the staged changelog payload the script copies into
	// the clone; an empty value leaves the repository's changelog untouched.
	Changelog    support.StagedChangelog
	AuthToken    string
	ProviderName string
}

// buildLocalUpgradeScript builds a bash script that performs only the pub
// operations and the changelog update. Git operations (branch creation,
// staging, committing, pushing) are handled by LocalGitContext, and the .fvmrc
// pin is rewritten in Go.
func buildLocalUpgradeScript(params localUpgradeParams) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	writeLocalAuth(&sb, params)
	writeDartUpgradeCommands(&sb)
	sb.WriteString(support.ChangelogUpdateScript())

	return sb.String()
}

// writeLocalAuth adds credential setup to the script when a token is provided.
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
		"PUB_EXECUTABLE="+params.Toolchain,
	)
	if params.AuthToken != "" {
		env = append(env,
			"AUTH_TOKEN="+params.AuthToken,
			"GIT_HTTPS_TOKEN="+params.AuthToken,
		)
	}
	env = append(env, params.Changelog.Env()...)
	return env
}
