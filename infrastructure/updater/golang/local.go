package golang

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/domain"
)

// LocalUpgradeOptions holds options for the local (standalone) upgrade mode.
type LocalUpgradeOptions struct {
	DryRun       bool
	Verbose      bool
	AuthToken    string // auth token for private module access (passed to config.sh)
	ProviderName string // git provider name (e.g. "azuredevops", "github", "gitlab")
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
	latestGoVersion, err := fetchLatestGoVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch latest Go version: %w", err)
	}
	logger.Infof("[golang] Latest stable Go version: %s", latestGoVersion)

	goModContent, err := os.ReadFile(filepath.Join(repoDir, "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("failed to read go.mod: %w", err)
	}

	currentGoVersion := parseGoDirective(string(goModContent))
	needsVersionUpgrade := currentGoVersion != latestGoVersion
	logger.Infof(
		"[golang] Current go directive: %s (upgrade needed: %v)",
		currentGoVersion, needsVersionUpgrade,
	)

	branchName := fmt.Sprintf(branchGoDepsFmt, latestGoVersion)
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

// executeLocalUpgrade performs the actual upgrade by running the
// generated bash script in the local repository.
func executeLocalUpgrade(
	ctx context.Context,
	repoDir string,
	vCtx *versionContext,
	opts LocalUpgradeOptions,
) (*LocalResult, error) {
	changelogFile := prepareLocalChangelog(repoDir, vCtx)

	goBinary, err := findGoBinary()
	if err != nil {
		return nil, fmt.Errorf("go binary not found: %w", err)
	}

	// Check whether the local repo contains a config.sh that should be
	// sourced before running Go commands (private module settings, etc.).
	hasConfigSH := false
	if _, statErr := os.Stat(filepath.Join(repoDir, "config.sh")); statErr == nil {
		hasConfigSH = true
	}

	params := localUpgradeParams{
		BranchName:    vCtx.BranchName,
		GoVersion:     vCtx.LatestVersion,
		ChangelogFile: changelogFile,
		AuthToken:     opts.AuthToken,
		ProviderName:  opts.ProviderName,
		HasConfigSH:   hasConfigSH,
	}

	script := buildLocalUpgradeScript(params)

	tmpDir, err := os.MkdirTemp("", "autoupdate-local-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	scriptPath := filepath.Join(tmpDir, "upgrade.sh")
	if writeErr := os.WriteFile(scriptPath, []byte(script), scriptFileMode); writeErr != nil {
		return nil, fmt.Errorf("failed to write script: %w", writeErr)
	}

	cmd := exec.CommandContext(ctx, "bash", scriptPath)
	cmd.Dir = repoDir
	cmd.Env = buildLocalEnv(params, goBinary)

	output, runErr := cmd.CombinedOutput()
	outputStr := string(output)

	if opts.Verbose {
		logger.Debugf("[golang] Script output:\n%s", outputStr)
	}

	if runErr != nil {
		return nil, fmt.Errorf(
			"upgrade script failed: %w\nOutput:\n%s", runErr, outputStr,
		)
	}

	return &LocalResult{
		HasChanges:       strings.Contains(outputStr, "CHANGES_PUSHED=true"),
		GoVersionUpdated: strings.Contains(outputStr, "GO_VERSION_UPDATED=true"),
		LatestVersion:    vCtx.LatestVersion,
		BranchName:       vCtx.BranchName,
		Output:           outputStr,
	}, nil
}

// --- local-mode internal types & helpers ---

type localUpgradeParams struct {
	BranchName    string
	GoVersion     string
	ChangelogFile string
	AuthToken     string // auth token for private module access
	ProviderName  string // git provider name (for credential setup)
	HasConfigSH   bool   // whether the repo contains config.sh
}

// buildLocalUpgradeScript builds a bash script that upgrades Go
// dependencies in an already-checked-out repository.  Unlike the
// remote-mode script it does not clone — but it does set up git
// credentials (when a token is provided) and sources config.sh
// (when present) so that private Go modules can be fetched.
func buildLocalUpgradeScript(params localUpgradeParams) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	// Set up git credentials when an auth token is available, so that
	// private Go modules (go get) and git push can authenticate.
	writeLocalAuth(&sb, params)

	// Verify working tree is clean
	sb.WriteString("# Verify working tree is clean\n")
	sb.WriteString("if [ -n \"$(git status --porcelain)\" ]; then\n")
	sb.WriteString("    echo \"ERROR: working tree has uncommitted changes, please commit or stash first\"\n")
	sb.WriteString("    exit 1\n")
	sb.WriteString("fi\n\n")

	// Create branch
	sb.WriteString("echo \"Creating branch $BRANCH_NAME...\"\n")
	sb.WriteString("git checkout -b \"$BRANCH_NAME\" 2>&1\n\n")

	// Source config.sh if present (sets GOPRIVATE, GONOSUMDB, etc.)
	if params.HasConfigSH {
		sb.WriteString("echo \"Running config.sh...\"\n")
		sb.WriteString("if [ -f \"./config.sh\" ]; then\n")
		sb.WriteString("    source ./config.sh\n")
		sb.WriteString("fi\n\n")
	}

	// Go upgrade commands (reuse existing)
	writeGoUpgradeCommands(&sb)

	// Changelog update (reuse existing)
	writeChangelogUpdate(&sb)

	// Commit and push (reuse existing)
	writeCommitAndPush(&sb)

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
	case "azuredevops":
		writeAzureDevOpsAuth(sb)
	case "github":
		writeGitHubAuth(sb)
	case "gitlab":
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
	if params.ChangelogFile != "" {
		env = append(env, "CHANGELOG_FILE="+params.ChangelogFile)
	}
	return env
}

// prepareLocalChangelog reads CHANGELOG.md from disk (if it exists),
// inserts an upgrade entry, and writes the result to a temp file.
// Returns the temp file path, or "" if no changelog update is needed.
func prepareLocalChangelog(repoDir string, vCtx *versionContext) string {
	content, err := os.ReadFile(filepath.Join(repoDir, "CHANGELOG.md"))
	if err != nil {
		return "" // no changelog present
	}

	var entry string
	if vCtx.NeedsVersionUpgrade {
		entry = fmt.Sprintf(
			"- changed the Go version to `%s` and updated all module dependencies",
			vCtx.LatestVersion,
		)
	} else {
		entry = "- changed the Go module dependencies to their latest versions"
	}

	modified := domain.InsertChangelogEntry(string(content), []string{entry})
	if modified == string(content) {
		return ""
	}

	tmpFile, writeErr := os.CreateTemp("", "changelog-*.md")
	if writeErr != nil {
		logger.Warnf("[golang] Failed to create temp changelog file: %v", writeErr)
		return ""
	}

	if _, writeErr = tmpFile.WriteString(modified); writeErr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		logger.Warnf("[golang] Failed to write temp changelog: %v", writeErr)
		return ""
	}
	_ = tmpFile.Close()

	return tmpFile.Name()
}
