package ruby

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlocal"
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
	RubyVersionUpdated bool
	LatestVersion      string
	BranchName         string
	Output             string
}

// RunLocalUpgrade runs the Ruby dependency upgrade directly in a local
// repository directory. Unlike CreateUpdatePRs it does not clone the
// repository and does not set up git credentials -- it relies on the
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

	if opts.DryRun {
		return handleDryRun(vCtx, repoDir), nil
	}

	return executeLocalUpgrade(ctx, repoDir, vCtx, opts)
}

// resolveLocalVersionContext fetches the latest Ruby version and compares
// it against the local .ruby-version to build a versionContext.
func resolveLocalVersionContext(ctx context.Context, repoDir string) *versionContext {
	fetcher := NewHTTPRubyVersionFetcher(&http.Client{Timeout: rbVersionTimeout})
	latestRbVersion, err := fetcher.FetchLatestVersion(ctx)
	if err != nil {
		logger.Warnf("[ruby] Failed to fetch latest Ruby version: %v (continuing without version upgrade)", err)
		latestRbVersion = ""
	} else {
		logger.Infof("[ruby] Latest stable Ruby version: %s", latestRbVersion)
	}

	needsVersionUpgrade := false
	if latestRbVersion != "" {
		rbVersionContent, readErr := os.ReadFile(filepath.Join(repoDir, ".ruby-version"))
		if readErr == nil {
			currentVersion := parseRubyVersionFile(string(rbVersionContent))
			needsVersionUpgrade = currentVersion != "" && currentVersion != latestRbVersion
			logger.Infof(
				"[ruby] Current .ruby-version: %s (upgrade needed: %v)",
				currentVersion, needsVersionUpgrade,
			)
		}
	}

	branchName := branchRbDepsFmt
	if needsVersionUpgrade {
		branchName = fmt.Sprintf(branchRbVersionFmt, latestRbVersion)
	}

	return &versionContext{
		LatestVersion:       latestRbVersion,
		NeedsVersionUpgrade: needsVersionUpgrade,
		BranchName:          branchName,
	}
}

// handleDryRun logs the planned action and returns a result without
// executing the upgrade.
func handleDryRun(vCtx *versionContext, repoDir string) *LocalResult {
	if vCtx.NeedsVersionUpgrade {
		logger.Infof(
			"[ruby] [DRY RUN] Would upgrade Ruby to %s and update deps in %s",
			vCtx.LatestVersion, repoDir,
		)
	} else {
		logger.Infof(
			"[ruby] [DRY RUN] Would update Ruby gem dependencies in %s",
			repoDir,
		)
	}
	return &LocalResult{
		LatestVersion:      vCtx.LatestVersion,
		BranchName:         vCtx.BranchName,
		RubyVersionUpdated: vCtx.NeedsVersionUpgrade,
	}
}

// executeLocalUpgrade performs the actual upgrade using go-git for
// branch/commit/push operations and a bash script only for the
// language-specific upgrade commands (gem update, bundle update, etc.).
func executeLocalUpgrade(
	ctx context.Context,
	repoDir string,
	vCtx *versionContext,
	opts LocalUpgradeOptions,
) (*LocalResult, error) {
	// --- Git Setup (go-git) ---
	gitCtx, err := gitlocal.NewLocalGitContext(repoDir, opts.PushAuth)
	if err != nil {
		return nil, err
	}
	originalBranch, err := gitCtx.CurrentBranch()
	if err != nil {
		return nil, fmt.Errorf("failed to determine current branch: %w", err)
	}
	stashed, stashErr := gitCtx.StashIfDirty()
	if stashErr != nil {
		return nil, stashErr
	}
	if stashed {
		defer func() {
			if checkoutErr := gitCtx.CheckoutBranch(originalBranch); checkoutErr != nil {
				logger.Warnf("Failed to switch back to %s: %v", originalBranch, checkoutErr)
			}
			if restoreErr := gitCtx.RestoreStash(); restoreErr != nil {
				logger.Warnf("Failed to restore stash: %v", restoreErr)
			}
		}()
	}
	if err = gitCtx.CreateBranch(vCtx.BranchName); err != nil {
		return nil, fmt.Errorf("failed to create branch %s: %w", vCtx.BranchName, err)
	}

	// --- Language Operations (bash) ---
	outputStr, runErr := runLanguageUpgradeScript(ctx, repoDir, vCtx, opts)
	if runErr != nil {
		return nil, runErr
	}

	rubyVersionUpdated := strings.Contains(outputStr, "RUBY_VERSION_UPDATED=true")

	// --- Git Finalize (go-git) ---
	commitMsg := rbCommitMsgDeps
	if rubyVersionUpdated {
		commitMsg = fmt.Sprintf(
			"chore(deps): upgraded Ruby to `%s` and updated all gem dependencies",
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
		RubyVersionUpdated: rubyVersionUpdated,
		LatestVersion:      vCtx.LatestVersion,
		BranchName:         vCtx.BranchName,
		Output:             outputStr,
	}, nil
}

// runLanguageUpgradeScript builds and executes the bash script that
// performs Ruby-specific upgrade operations (gem update, bundle update,
// Dockerfile updates, changelog updates).
func runLanguageUpgradeScript(
	ctx context.Context,
	repoDir string,
	vCtx *versionContext,
	opts LocalUpgradeOptions,
) (string, error) {
	changelogFile := prepareLocalChangelog(repoDir, vCtx)
	if changelogFile != "" {
		defer os.Remove(changelogFile)
	}

	params := localUpgradeParams{
		BranchName:    vCtx.BranchName,
		RubyVersion:   vCtx.LatestVersion,
		ChangelogFile: changelogFile,
		AuthToken:     opts.AuthToken,
		ProviderName:  opts.ProviderName,
	}

	script := buildLocalUpgradeScript(params)

	tmpDir, err := os.MkdirTemp("", "autoupdate-ruby-local-*")
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
		Env: buildLocalEnv(params),
	})

	var outputStr string
	if runResult != nil {
		outputStr = runResult.Output
	}

	if opts.Verbose {
		logger.Debugf("[ruby] Script output:\n%s", outputStr)
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
	BranchName    string
	RubyVersion   string
	ChangelogFile string
	AuthToken     string
	ProviderName  string
}

// buildLocalUpgradeScript builds a bash script that performs only the
// language-specific upgrade operations (auth, gem update, bundle update,
// Dockerfile updates, changelog updates). Git operations
// (branch creation, staging, committing, pushing) are handled by
// LocalGitContext.
func buildLocalUpgradeScript(params localUpgradeParams) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	// Set up git credentials when an auth token is available
	writeLocalAuth(&sb, params)

	// Ruby upgrade commands (reuse remote-mode helpers)
	writeRubyUpgradeCommands(&sb)

	// Update Dockerfile ruby image tags
	writeDockerfileUpdate(&sb)

	// Changelog update
	writeChangelogUpdate(&sb)

	return sb.String()
}

// writeLocalAuth adds credential setup to the script when a token is
// provided.
func writeLocalAuth(sb *strings.Builder, params localUpgradeParams) {
	if params.AuthToken == "" {
		return
	}

	sb.WriteString("# Set up git credentials for private package access\n")
	sb.WriteString("TEMP_GITCONFIG=$(mktemp)\n")
	sb.WriteString("cp ~/.gitconfig \"$TEMP_GITCONFIG\" 2>/dev/null || true\n")

	switch params.ProviderName {
	case "azuredevops":
		sb.WriteString("echo '[url \"https://pat:'\"${AUTH_TOKEN}\"'@dev.azure.com/\"]' >> \"$TEMP_GITCONFIG\"\n")
		sb.WriteString("echo '    insteadOf = https://dev.azure.com/' >> \"$TEMP_GITCONFIG\"\n")
	case "github":
		sb.WriteString(
			"echo '[url \"https://x-access-token:'\"${AUTH_TOKEN}\"'@github.com/\"]' >> \"$TEMP_GITCONFIG\"\n",
		)
		sb.WriteString("echo '    insteadOf = https://github.com/' >> \"$TEMP_GITCONFIG\"\n")
	case "gitlab":
		sb.WriteString("echo '[url \"https://oauth2:'\"${AUTH_TOKEN}\"'@gitlab.com/\"]' >> \"$TEMP_GITCONFIG\"\n")
		sb.WriteString("echo '    insteadOf = https://gitlab.com/' >> \"$TEMP_GITCONFIG\"\n")
	}

	sb.WriteString("export GIT_CONFIG_GLOBAL=\"$TEMP_GITCONFIG\"\n")
	sb.WriteString("trap 'rm -f \"$TEMP_GITCONFIG\"' EXIT\n\n")
}

// buildLocalEnv returns the environment for the local upgrade script.
func buildLocalEnv(params localUpgradeParams) []string {
	env := append(os.Environ(),
		"BRANCH_NAME="+params.BranchName,
	)
	if params.RubyVersion != "" {
		env = append(env, "TARGET_RUBY_VERSION="+params.RubyVersion)
	}
	if params.AuthToken != "" {
		env = append(env,
			"AUTH_TOKEN="+params.AuthToken,
			"GIT_HTTPS_TOKEN="+params.AuthToken,
		)
	}
	if params.ChangelogFile != "" {
		env = append(env, "CHANGELOG_FILE="+params.ChangelogFile)
	}
	return env
}

// prepareLocalChangelog reads CHANGELOG.md from disk (if it exists),
// inserts an upgrade entry, and writes the result to a temp file.
func prepareLocalChangelog(repoDir string, vCtx *versionContext) string {
	content, err := os.ReadFile(filepath.Join(repoDir, "CHANGELOG.md"))
	if err != nil {
		return "" // no changelog present
	}

	var entry string
	if vCtx.NeedsVersionUpgrade {
		entry = fmt.Sprintf(
			"- changed the Ruby version to `%s` and updated all gem dependencies",
			vCtx.LatestVersion,
		)
	} else {
		entry = "- changed the Ruby gem dependencies to their latest versions"
	}

	modified := entities.InsertChangelogEntry(string(content), []string{entry})
	if modified == string(content) {
		return ""
	}

	tmpFile, writeErr := os.CreateTemp("", "autoupdate-changelog-*.md")
	if writeErr != nil {
		logger.Warnf("[ruby] Failed to create temp changelog file: %v", writeErr)
		return ""
	}

	if _, writeErr = tmpFile.WriteString(modified); writeErr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		logger.Warnf("[ruby] Failed to write temp changelog: %v", writeErr)
		return ""
	}
	_ = tmpFile.Close()

	return tmpFile.Name()
}
