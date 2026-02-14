package python

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// LocalUpgradeOptions holds options for the local (standalone) upgrade mode.
type LocalUpgradeOptions struct {
	DryRun       bool
	Verbose      bool
	AuthToken    string // auth token for private package access
	ProviderName string // git provider name (e.g. "azuredevops", "github", "gitlab")
}

// LocalResult holds the outcome of a local upgrade operation.
type LocalResult struct {
	HasChanges           bool
	PythonVersionUpdated bool
	LatestVersion        string
	BranchName           string
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

	if opts.DryRun {
		return handleDryRun(vCtx, repoDir), nil
	}

	return executeLocalUpgrade(ctx, repoDir, vCtx, opts)
}

// resolveLocalVersionContext fetches the latest Python version and compares
// it against the local .python-version to build a versionContext.
func resolveLocalVersionContext(ctx context.Context, repoDir string) *versionContext {
	latestPyVersion, err := fetchLatestPythonVersion(ctx)
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
func handleDryRun(vCtx *versionContext, repoDir string) *LocalResult {
	if vCtx.NeedsVersionUpgrade {
		logger.Infof(
			"[python] [DRY RUN] Would upgrade Python to %s and update deps in %s",
			vCtx.LatestVersion, repoDir,
		)
	} else {
		logger.Infof(
			"[python] [DRY RUN] Would update Python dependencies in %s",
			repoDir,
		)
	}
	return &LocalResult{
		LatestVersion:        vCtx.LatestVersion,
		BranchName:           vCtx.BranchName,
		PythonVersionUpdated: vCtx.NeedsVersionUpgrade,
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

	pythonBinary, err := findPythonBinary()
	if err != nil {
		return nil, fmt.Errorf("python binary not found: %w", err)
	}

	hasRequirements := false
	if _, statErr := os.Stat(filepath.Join(repoDir, "requirements.txt")); statErr == nil {
		hasRequirements = true
	}

	hasPyproject := false
	if _, statErr := os.Stat(filepath.Join(repoDir, "pyproject.toml")); statErr == nil {
		hasPyproject = true
	}

	params := localUpgradeParams{
		BranchName:      vCtx.BranchName,
		PythonVersion:   vCtx.LatestVersion,
		ChangelogFile:   changelogFile,
		AuthToken:       opts.AuthToken,
		ProviderName:    opts.ProviderName,
		HasRequirements: hasRequirements,
		HasPyproject:    hasPyproject,
		PythonBinary:    pythonBinary,
	}

	script := buildLocalUpgradeScript(params)

	tmpDir, err := os.MkdirTemp("", "autoupdate-python-local-*")
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
	cmd.Env = buildLocalEnv(params)

	output, runErr := cmd.CombinedOutput()
	outputStr := string(output)

	if opts.Verbose {
		logger.Debugf("[python] Script output:\n%s", outputStr)
	}

	if runErr != nil {
		return nil, fmt.Errorf(
			"upgrade script failed: %w\nOutput:\n%s", runErr, outputStr,
		)
	}

	return &LocalResult{
		HasChanges:           strings.Contains(outputStr, "CHANGES_PUSHED=true"),
		PythonVersionUpdated: strings.Contains(outputStr, "PYTHON_VERSION_UPDATED=true"),
		LatestVersion:        vCtx.LatestVersion,
		BranchName:           vCtx.BranchName,
		Output:               outputStr,
	}, nil
}

// --- local-mode internal types & helpers ---

type localUpgradeParams struct {
	BranchName      string
	PythonVersion   string
	ChangelogFile   string
	AuthToken       string
	ProviderName    string
	HasRequirements bool
	HasPyproject    bool
	PythonBinary    string
}

// buildLocalUpgradeScript builds a bash script that upgrades Python
// dependencies in an already-checked-out repository.
func buildLocalUpgradeScript(params localUpgradeParams) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	// Set up git credentials when an auth token is available
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

	// Python upgrade commands (reuse remote-mode helpers)
	writePythonUpgradeCommands(&sb, upgradeParams{
		HasRequirements: params.HasRequirements,
		HasPyproject:    params.HasPyproject,
	})

	// Update Dockerfile python image tags
	writeDockerfileUpdate(&sb)

	// Changelog update
	writeChangelogUpdate(&sb)

	// Commit and push
	writeCommitAndPush(&sb)

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
			"- changed the Python version to `%s` and updated all pip dependencies",
			vCtx.LatestVersion,
		)
	} else {
		entry = "- changed the Python dependencies to their latest versions"
	}

	modified := entities.InsertChangelogEntry(string(content), []string{entry})
	if modified == string(content) {
		return ""
	}

	tmpFile, writeErr := os.CreateTemp("", "changelog-*.md")
	if writeErr != nil {
		logger.Warnf("[python] Failed to create temp changelog file: %v", writeErr)
		return ""
	}

	if _, writeErr = tmpFile.WriteString(modified); writeErr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		logger.Warnf("[python] Failed to write temp changelog: %v", writeErr)
		return ""
	}
	_ = tmpFile.Close()

	return tmpFile.Name()
}
