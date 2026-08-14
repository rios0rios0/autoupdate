package golang

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
	HasChanges       bool
	GoVersionUpdated bool
	LatestVersion    string
	BranchName       string
	Output           string
}

// RunLocalUpgrade runs the Go dependency upgrade directly in a local
// repository directory.  Unlike CreateUpdatePRs it does not clone the
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

	vCtx, err := resolveLocalVersionContext(ctx, repoDir)
	if err != nil {
		return nil, err
	}

	if opts.DryRun {
		return handleDryRun(vCtx, repoDir), nil
	}

	return executeLocalUpgrade(ctx, repoDir, vCtx, opts)
}

// resolveLocalVersionContext fetches the latest Go version and compares
// it against the local go.mod to build a versionContext.
func resolveLocalVersionContext(ctx context.Context, repoDir string) (*versionContext, error) {
	fetcher := NewHTTPGoVersionFetcher(&http.Client{Timeout: goVersionTimeout})
	latestGoVersion, err := fetcher.FetchLatestVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch latest Go version: %w", err)
	}
	logger.Infof("[golang] Latest stable Go version: %s", latestGoVersion)

	// Every module counts, not just the root one: the repository may have no
	// root module at all, and a nested module may be the only stale one.
	needsVersionUpgrade, sourceDir, found := resolveVersionUpgradeNeed(
		localGoModReader(repoDir),
		func() []string { return discoverLocalModuleDirs(repoDir) },
		latestGoVersion,
	)
	if !found {
		return nil, fmt.Errorf("no readable %s found in %s", goModFileName, repoDir)
	}

	logger.Infof(
		"[golang] Go version upgrade needed: %v (decided by %s)",
		needsVersionUpgrade, goModPathFor(sourceDir),
	)

	branchName := branchGoDepsFmt
	if needsVersionUpgrade {
		branchName = fmt.Sprintf(branchGoVersionFmt, latestGoVersion)
	}

	return &versionContext{
		LatestVersion:       latestGoVersion,
		NeedsVersionUpgrade: needsVersionUpgrade,
		BranchName:          branchName,
	}, nil
}

// handleDryRun logs the planned action and returns a result without
// executing the upgrade.
func handleDryRun(vCtx *versionContext, repoDir string) *LocalResult {
	if vCtx.NeedsVersionUpgrade {
		logger.Infof(
			"[golang] [DRY RUN] Would upgrade Go to %s and update deps in %s",
			vCtx.LatestVersion, repoDir,
		)
	} else {
		logger.Infof(
			"[golang] [DRY RUN] Would update Go module deps in %s (already at Go %s)",
			repoDir, vCtx.LatestVersion,
		)
	}
	return &LocalResult{
		LatestVersion:    vCtx.LatestVersion,
		BranchName:       vCtx.BranchName,
		GoVersionUpdated: vCtx.NeedsVersionUpgrade,
	}
}

// executeLocalUpgrade performs the actual upgrade using go-git for
// branch/commit/push operations and a bash script only for the
// language-specific upgrade commands (go get, go mod tidy, etc.).
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

	// --- Language Operations (bash) ---
	outputStr, runErr := runLanguageUpgradeScript(ctx, repoDir, vCtx, opts)
	if runErr != nil {
		return nil, runErr
	}

	goVersionUpdated := strings.Contains(outputStr, "GO_VERSION_UPDATED=true")

	// Rewrite Dockerfile golang base-image tags in Go, verifying each target
	// tag exists on Docker Hub (falling back to the closest published one)
	// instead of blindly writing the go.dev version, which may not have a
	// published image yet or may lack the requested Alpine variant.
	if goVersionUpdated {
		dfChanged, dfErr := updateDockerfileGolangTags(
			ctx, repoDir, vCtx.LatestVersion, fetchGolangTags,
		)
		switch {
		case dfErr != nil:
			logger.Warnf("[golang] Failed to update Dockerfile golang image tags: %v", dfErr)
		case dfChanged:
			logger.Infof(
				"[golang] Updated Dockerfile golang base image tags to the closest published tags for Go %s",
				vCtx.LatestVersion,
			)
		}
	}

	// --- Git Finalize (go-git) ---
	commitMsg := goCommitMsgDeps
	if goVersionUpdated {
		commitMsg = fmt.Sprintf(
			"chore(deps): upgraded Go version to `%s` and updated all dependencies",
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
		HasChanges:       pushed,
		GoVersionUpdated: goVersionUpdated,
		LatestVersion:    vCtx.LatestVersion,
		BranchName:       vCtx.BranchName,
		Output:           outputStr,
	}, nil
}

// runLanguageUpgradeScript builds and executes the bash script that
// performs Go-specific upgrade operations (go get, go mod tidy,
// Dockerfile updates, changelog updates).
func runLanguageUpgradeScript(
	ctx context.Context,
	repoDir string,
	vCtx *versionContext,
	opts LocalUpgradeOptions,
) (string, error) {
	changelog := support.StageLocalChangelog(repoDir, changelogEntries(vCtx))
	defer changelog.Remove()

	goBinary, err := findGoBinary()
	if err != nil {
		return "", fmt.Errorf("go binary not found: %w", err)
	}

	hasConfigSH := false
	if _, statErr := os.Stat(filepath.Join(repoDir, "config.sh")); statErr == nil {
		hasConfigSH = true
	}

	params := localUpgradeParams{
		BranchName:   vCtx.BranchName,
		GoVersion:    vCtx.LatestVersion,
		Changelog:    changelog,
		AuthToken:    opts.AuthToken,
		ProviderName: opts.ProviderName,
		HasConfigSH:  hasConfigSH,
	}

	script := buildLocalUpgradeScript(params)

	tmpDir, err := os.MkdirTemp("", "autoupdate-local-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	scriptPath := filepath.Join(tmpDir, "upgrade.sh")
	if writeErr := os.WriteFile(scriptPath, []byte(script), scriptFileMode); writeErr != nil {
		return "", fmt.Errorf("failed to write script: %w", writeErr)
	}

	runResult, runErr := localCmdRunner.Run(ctx, "bash", []string{scriptPath}, cmdrunner.RunOptions{
		Dir: repoDir,
		Env: buildLocalEnv(params, goBinary),
	})

	var outputStr string
	if runResult != nil {
		outputStr = runResult.Output
	}

	if opts.Verbose {
		logger.Debugf("[golang] Script output:\n%s", outputStr)
	}

	if runErr != nil {
		return "", fmt.Errorf(
			"upgrade script failed: %w\nOutput:\n%s", runErr, outputStr,
		)
	}

	return outputStr, nil
}

// --- local-mode internal types & helpers ---

type localUpgradeParams struct {
	BranchName string
	GoVersion  string
	// Changelog is the staged changelog payload the script copies into
	// the clone; an empty value leaves the repository's changelog untouched.
	Changelog    support.StagedChangelog
	AuthToken    string
	ProviderName string // git provider name (for credential setup)
	HasConfigSH  bool   // whether the repo contains config.sh
}

// buildLocalUpgradeScript builds a bash script that performs only the
// language-specific upgrade operations (auth, go get, go mod tidy,
// Dockerfile updates, changelog updates).  Git operations (branch
// creation, staging, committing, pushing) are handled by LocalGitContext.
func buildLocalUpgradeScript(params localUpgradeParams) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	// Set up git credentials when an auth token is available, so that
	// private Go modules (go get) can authenticate.
	writeLocalAuth(&sb, params)

	// Source config.sh if present (sets GOPRIVATE, GONOSUMDB, etc.)
	if params.HasConfigSH {
		sb.WriteString("echo \"Running config.sh...\"\n")
		sb.WriteString("if [ -f \"./config.sh\" ]; then\n")
		sb.WriteString("    source ./config.sh\n")
		sb.WriteString("fi\n\n")
	}

	// Go upgrade commands (reuse existing)
	writeGoUpgradeCommands(&sb)

	// NOTE: Dockerfile golang image tags are updated in Go (registry-verified)
	// by updateDockerfileGolangTags after this script runs — never via a blind
	// tag rewrite here, which could point FROM at a non-existent image.

	// Changelog update (reuse existing)
	sb.WriteString(support.ChangelogUpdateScript())

	return sb.String()
}

// writeLocalAuth adds credential setup to the script when a token is
// provided.  It mirrors the remote-mode auth helpers but uses a
// temporary gitconfig so the user's real ~/.gitconfig is not modified.
func writeLocalAuth(sb *strings.Builder, params localUpgradeParams) {
	if params.AuthToken == "" {
		return
	}

	sb.WriteString("# Set up git credentials for private module access\n")
	sb.WriteString("TEMP_GITCONFIG=$(mktemp)\n")
	sb.WriteString("cp ~/.gitconfig \"$TEMP_GITCONFIG\" 2>/dev/null || true\n")

	switch params.ProviderName {
	case providerAzureDevOps:
		writeAzureDevOpsAuth(sb)
	case providerGitHub:
		writeGitHubAuth(sb)
	case providerGitLab:
		writeGitLabAuth(sb)
	}

	sb.WriteString("export GIT_CONFIG_GLOBAL=\"$TEMP_GITCONFIG\"\n")
	sb.WriteString("trap 'rm -f \"$TEMP_GITCONFIG\"' EXIT\n\n")
}

// buildLocalEnv returns the environment for the local upgrade script.
// It includes auth tokens when provided so that config.sh and git
// push can authenticate against the remote.
func buildLocalEnv(params localUpgradeParams, goBinary string) []string {
	env := append(os.Environ(),
		"BRANCH_NAME="+params.BranchName,
		"GO_VERSION="+params.GoVersion,
		"GO_BINARY="+goBinary,
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
