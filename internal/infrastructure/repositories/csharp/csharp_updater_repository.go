package csharp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
	"github.com/rios0rios0/autoupdate/internal/support"
	langCSharp "github.com/rios0rios0/langforge/pkg/infrastructure/languages/csharp"
)

const (
	updaterName          = "csharp"
	dotnetVersionTimeout = 15 * time.Second
	scriptFileMode       = 0o700

	// Branch name patterns for C# updates. One format is used when the
	// .NET SDK version itself is being bumped; the other is used when
	// only NuGet dependencies are being refreshed.
	branchDotnetVersionFmt = "chore/upgrade-dotnet-%s"
	branchDotnetDepsFmt    = "chore/upgrade-dotnet-deps"

	// Commit/PR messages and changelog entries used across remote and local modes.
	dotnetCommitMsgDeps      = "chore(deps): updated NuGet dependencies"
	dotnetChangelogEntryDeps = "- changed the NuGet dependencies to their latest versions"

	// Git provider names for auth setup.
	providerAzureDevOps = "azuredevops"
	providerGitHub      = "github"
	providerGitLab      = "gitlab"
)

// defaultRunner is the package-level command runner for remote-mode functions.
var defaultRunner cmdrunner.Runner = cmdrunner.NewDefaultRunner() //nolint:gochecknoglobals // test override

// UpdaterRepository implements repositories.UpdaterRepository for C# / .NET dependencies.
// It clones the repository locally, runs dotnet commands to update
// dependencies, pushes the changes, and creates a PR via the provider API.
type UpdaterRepository struct {
	versionFetcher VersionFetcher
	cmdRunner      cmdrunner.Runner
}

// NewUpdaterRepository creates a new C# updater with default dependencies.
func NewUpdaterRepository() repositories.UpdaterRepository {
	return &UpdaterRepository{
		versionFetcher: NewHTTPDotnetVersionFetcher(&http.Client{Timeout: dotnetVersionTimeout}),
		cmdRunner:      cmdrunner.NewDefaultRunner(),
	}
}

// NewUpdaterRepositoryWithDeps creates a C# updater with injected dependencies (for testing).
func NewUpdaterRepositoryWithDeps(vf VersionFetcher) repositories.UpdaterRepository {
	return &UpdaterRepository{versionFetcher: vf, cmdRunner: cmdrunner.NewDefaultRunner()}
}

func (u *UpdaterRepository) Name() string { return updaterName }

// Detect returns true if the repository has C# marker files (e.g. *.csproj, *.sln).
func (u *UpdaterRepository) Detect(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) bool {
	found, err := support.DetectRemote(ctx, &langCSharp.Detector{}, provider, repo)
	if err != nil {
		logger.Warnf("[csharp] detection error for %s/%s: %v", repo.Organization, repo.Name, err)
		return false
	}
	return found
}

// CreateUpdatePRs clones the repo, upgrades .NET SDK and NuGet dependencies,
// and creates a PR.
func (u *UpdaterRepository) CreateUpdatePRs(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
) ([]entities.PullRequest, error) {
	logger.Infof("[csharp] Processing %s/%s", repo.Organization, repo.Name)

	latestDotnetVersion, err := u.versionFetcher.FetchLatestVersion(ctx)
	if err != nil {
		logger.Warnf("[csharp] Failed to fetch latest .NET version: %v (continuing without version upgrade)", err)
		latestDotnetVersion = ""
	} else {
		logger.Infof("[csharp] Latest stable .NET SDK version: %s", latestDotnetVersion)
	}

	vCtx := resolveVersionContext(ctx, provider, repo, latestDotnetVersion)

	// Check if PR already exists
	exists, prCheckErr := provider.PullRequestExists(ctx, repo, vCtx.BranchName)
	if prCheckErr != nil {
		logger.Warnf("[csharp] Failed to check existing PRs: %v", prCheckErr)
	}
	if exists {
		logger.Infof(
			"[csharp] PR already exists for branch %q, skipping",
			vCtx.BranchName,
		)
		return []entities.PullRequest{}, nil
	}

	if opts.DryRun {
		logDryRun(vCtx, repo)
		return []entities.PullRequest{}, nil
	}

	result, upgradeErr := cloneAndUpgrade(ctx, provider, repo, vCtx)
	if upgradeErr != nil {
		return nil, upgradeErr
	}

	if !result.HasChanges {
		logger.Infof("[csharp] %s/%s: already up to date", repo.Organization, repo.Name)
		return []entities.PullRequest{}, nil
	}

	return openPullRequest(ctx, provider, repo, opts, vCtx, result)
}

// logDryRun logs what would happen without actually performing the upgrade.
func logDryRun(vCtx *versionContext, repo entities.Repository) {
	if vCtx.NeedsVersionUpgrade {
		logger.Infof(
			"[csharp] [DRY RUN] Would upgrade .NET SDK to %s and update deps for %s/%s",
			vCtx.LatestVersion, repo.Organization, repo.Name,
		)
	} else {
		logger.Infof(
			"[csharp] [DRY RUN] Would update NuGet dependencies for %s/%s",
			repo.Organization, repo.Name,
		)
	}
}

// cloneAndUpgrade prepares the changelog, clones the repository, runs the
// upgrade script, and returns the result.
func cloneAndUpgrade(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	vCtx *versionContext,
) (*upgradeResult, error) {
	changelogFile := prepareChangelog(ctx, provider, repo, vCtx)
	if changelogFile != "" {
		defer os.Remove(changelogFile)
	}

	cloneURL := provider.CloneURL(repo)
	defaultBranch := strings.TrimPrefix(repo.DefaultBranch, "refs/heads/")

	dotnetBinary, err := findDotnetBinary()
	if err != nil {
		return nil, fmt.Errorf("dotnet binary not found: %w", err)
	}

	result, err := upgradeRepo(ctx, upgradeParams{
		CloneURL:      cloneURL,
		DefaultBranch: defaultBranch,
		BranchName:    vCtx.BranchName,
		DotnetVersion: vCtx.LatestVersion,
		AuthToken:     provider.AuthToken(),
		ProviderName:  provider.Name(),
		ChangelogFile: changelogFile,
		DotnetBinary:  dotnetBinary,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upgrade: %w", err)
	}

	return result, nil
}

// openPullRequest creates the PR on the hosting provider after a successful
// upgrade.
func openPullRequest(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
	vCtx *versionContext,
	result *upgradeResult,
) ([]entities.PullRequest, error) {
	targetBranch := repo.DefaultBranch
	if opts.TargetBranch != "" {
		targetBranch = "refs/heads/" + opts.TargetBranch
	}

	prTitle := dotnetCommitMsgDeps
	if result.DotnetVersionUpdated {
		prTitle = fmt.Sprintf(
			"chore(deps): upgraded .NET SDK to `%s` and updated all NuGet dependencies",
			vCtx.LatestVersion,
		)
	}
	prDesc := GeneratePRDescription(vCtx.LatestVersion, result.DotnetVersionUpdated)

	pr, createErr := provider.CreatePullRequest(ctx, repo, entities.PullRequestInput{
		SourceBranch: "refs/heads/" + vCtx.BranchName,
		TargetBranch: targetBranch,
		Title:        prTitle,
		Description:  prDesc,
		AutoComplete: opts.AutoComplete,
	})
	if createErr != nil {
		return nil, fmt.Errorf("failed to create PR: %w", createErr)
	}

	logger.Infof(
		"[csharp] Created PR #%d for %s/%s: %s",
		pr.ID, repo.Organization, repo.Name, pr.URL,
	)
	return []entities.PullRequest{*pr}, nil
}

// ApplyUpdates implements repositories.LocalUpdater. It runs language-specific
// .NET upgrade operations on a locally cloned repository, without performing
// any git clone, branch, commit, or push operations.
func (u *UpdaterRepository) ApplyUpdates(
	ctx context.Context,
	repoDir string,
	_ repositories.ProviderRepository,
	repo entities.Repository,
	_ entities.UpdateOptions,
) (*repositories.LocalUpdateResult, error) {
	logger.Infof("[csharp] Processing local clone of %s/%s", repo.Organization, repo.Name)

	// resolveLocalVersionContext handles fetching + comparison
	vCtx := resolveLocalVersionContext(ctx, repoDir)

	dotnetBinary, binErr := findDotnetBinary()
	if binErr != nil {
		return nil, fmt.Errorf("dotnet binary not found: %w", binErr)
	}

	script := buildBatchDotnetScript()
	scriptPath := filepath.Join(repoDir, ".autoupdate-upgrade.sh")
	if writeErr := os.WriteFile(scriptPath, []byte(script), scriptFileMode); writeErr != nil {
		return nil, fmt.Errorf("failed to write script: %w", writeErr)
	}
	defer func() { _ = os.Remove(scriptPath) }()

	cmd := exec.CommandContext(ctx, "bash", scriptPath)
	cmd.Dir = repoDir
	env := append(os.Environ(), "DOTNET_BINARY="+dotnetBinary)
	if vCtx.LatestVersion != "" {
		env = append(env, "DOTNET_VERSION="+vCtx.LatestVersion)
	}
	cmd.Env = env

	output, cmdErr := cmd.CombinedOutput()
	outputStr := string(output)
	logger.Debugf("[csharp] Upgrade script output:\n%s", outputStr)

	if cmdErr != nil {
		return nil, fmt.Errorf("upgrade script failed: %w\nOutput:\n%s", cmdErr, outputStr)
	}

	// Remove the script before checking worktree state so it does not
	// appear as an untracked file in the git status check below.
	_ = os.Remove(scriptPath)
	dotnetVersionUpdated := strings.Contains(outputStr, "DOTNET_VERSION_UPDATED=true")

	// Return early if the upgrade script made no filesystem changes
	if !support.HasUncommittedChanges(ctx, repoDir) {
		logger.Infof("[csharp] No filesystem changes detected after upgrade script")
		return nil, repositories.ErrNoUpdatesNeeded
	}

	// Update CHANGELOG locally
	var entry string
	if dotnetVersionUpdated {
		entry = fmt.Sprintf(
			"- changed the .NET SDK version to `%s` and updated all NuGet dependencies",
			vCtx.LatestVersion,
		)
	} else {
		entry = dotnetChangelogEntryDeps
	}
	support.LocalChangelogUpdate(repoDir, []string{entry})

	commitMsg := dotnetCommitMsgDeps
	prTitle := commitMsg
	if dotnetVersionUpdated {
		commitMsg = fmt.Sprintf(
			"chore(deps): upgraded .NET SDK to `%s` and updated all NuGet dependencies",
			vCtx.LatestVersion,
		)
		prTitle = commitMsg
	}

	return &repositories.LocalUpdateResult{
		BranchName:    vCtx.BranchName,
		CommitMessage: commitMsg,
		PRTitle:       prTitle,
		PRDescription: GeneratePRDescription(vCtx.LatestVersion, dotnetVersionUpdated),
	}, nil
}

// buildBatchDotnetScript generates a bash script with only language-specific
// operations (no git clone, branch, commit, or push) for the batch pipeline.
func buildBatchDotnetScript() string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	writeDotnetUpgradeCommands(&sb)
	writeDockerfileUpdate(&sb)

	return sb.String()
}

// --- internal types ---

type versionContext struct {
	LatestVersion       string
	NeedsVersionUpgrade bool
	BranchName          string
}

type upgradeParams struct {
	CloneURL      string
	DefaultBranch string
	BranchName    string
	DotnetVersion string
	AuthToken     string
	ProviderName  string
	ChangelogFile string
	DotnetBinary  string
}

type upgradeResult struct {
	HasChanges           bool
	DotnetVersionUpdated bool
	Output               string
}

// globalJSON represents the structure of a global.json file.
type globalJSON struct {
	SDK struct {
		Version string `json:"version"`
	} `json:"sdk"`
}

// parseGlobalJSON extracts the SDK version from a global.json file content.
func parseGlobalJSON(content string) string {
	var g globalJSON
	if err := json.Unmarshal([]byte(content), &g); err != nil {
		return ""
	}
	return g.SDK.Version
}

// --- version context ---

// resolveVersionContext reads the remote global.json to find the current
// .NET SDK version and picks the right branch-name pattern.
func resolveVersionContext(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	latestDotnetVersion string,
) *versionContext {
	needsVersionUpgrade := false

	if latestDotnetVersion != "" && provider.HasFile(ctx, repo, "global.json") {
		content, err := provider.GetFileContent(ctx, repo, "global.json")
		if err == nil {
			currentVersion := parseGlobalJSON(content)
			needsVersionUpgrade = currentVersion != "" && currentVersion != latestDotnetVersion
			logger.Infof(
				"[csharp] Current global.json SDK version: %s (upgrade needed: %v)",
				currentVersion, needsVersionUpgrade,
			)
		}
	}

	branchName := branchDotnetDepsFmt
	if needsVersionUpgrade {
		branchName = fmt.Sprintf(branchDotnetVersionFmt, latestDotnetVersion)
	}

	return &versionContext{
		LatestVersion:       latestDotnetVersion,
		NeedsVersionUpgrade: needsVersionUpgrade,
		BranchName:          branchName,
	}
}

// resolveLocalVersionContext fetches the latest .NET SDK version and compares
// it against the local global.json to build a versionContext.
func resolveLocalVersionContext(ctx context.Context, repoDir string) *versionContext {
	fetcher := NewHTTPDotnetVersionFetcher(&http.Client{Timeout: dotnetVersionTimeout})
	latestDotnetVersion, err := fetcher.FetchLatestVersion(ctx)
	if err != nil {
		logger.Warnf("[csharp] Failed to fetch latest .NET version: %v (continuing without version upgrade)", err)
		latestDotnetVersion = ""
	} else {
		logger.Infof("[csharp] Latest stable .NET SDK version: %s", latestDotnetVersion)
	}

	needsVersionUpgrade := false
	if latestDotnetVersion != "" {
		globalJSONContent, readErr := os.ReadFile(filepath.Join(repoDir, "global.json"))
		if readErr == nil {
			currentVersion := parseGlobalJSON(string(globalJSONContent))
			needsVersionUpgrade = currentVersion != "" && currentVersion != latestDotnetVersion
			logger.Infof(
				"[csharp] Current global.json SDK version: %s (upgrade needed: %v)",
				currentVersion, needsVersionUpgrade,
			)
		}
	}

	branchName := branchDotnetDepsFmt
	if needsVersionUpgrade {
		branchName = fmt.Sprintf(branchDotnetVersionFmt, latestDotnetVersion)
	}

	return &versionContext{
		LatestVersion:       latestDotnetVersion,
		NeedsVersionUpgrade: needsVersionUpgrade,
		BranchName:          branchName,
	}
}

// prepareChangelog reads the target repo's CHANGELOG.md (if it exists),
// inserts an entry describing the .NET upgrade, and writes the modified
// content to a temp file.
func prepareChangelog(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	vCtx *versionContext,
) string {
	if !provider.HasFile(ctx, repo, "CHANGELOG.md") {
		return ""
	}

	content, err := provider.GetFileContent(ctx, repo, "CHANGELOG.md")
	if err != nil {
		logger.Warnf("[csharp] Failed to read CHANGELOG.md: %v", err)
		return ""
	}

	var entry string
	if vCtx.NeedsVersionUpgrade {
		entry = fmt.Sprintf(
			"- changed the .NET SDK version to `%s` and updated all NuGet dependencies",
			vCtx.LatestVersion,
		)
	} else {
		entry = dotnetChangelogEntryDeps
	}

	modified := entities.InsertChangelogEntry(content, []string{entry})
	if modified == content {
		return ""
	}

	tmpFile, writeErr := os.CreateTemp("", "autoupdate-changelog-*.md")
	if writeErr != nil {
		logger.Warnf("[csharp] Failed to create temp changelog file: %v", writeErr)
		return ""
	}

	if _, writeErr = tmpFile.WriteString(modified); writeErr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		logger.Warnf("[csharp] Failed to write temp changelog: %v", writeErr)
		return ""
	}
	_ = tmpFile.Close()

	return tmpFile.Name()
}

// --- clone + upgrade ---

func upgradeRepo(
	ctx context.Context,
	params upgradeParams,
) (*upgradeResult, error) {
	result := &upgradeResult{}

	tmpDir, err := os.MkdirTemp("", "autoupdate-csharp-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "repo")

	script := buildUpgradeScript(params, repoDir)
	scriptPath := filepath.Join(tmpDir, "upgrade.sh")

	if writeErr := os.WriteFile(scriptPath, []byte(script), scriptFileMode); writeErr != nil {
		return nil, fmt.Errorf("failed to write script: %w", writeErr)
	}

	runResult, runErr := defaultRunner.Run(ctx, "bash", []string{scriptPath}, cmdrunner.RunOptions{
		Dir: tmpDir,
		Env: buildEnv(params, repoDir),
	})
	if runResult != nil {
		result.Output = runResult.Output
	}

	if runErr != nil {
		redactedOutput := support.RedactTokens(result.Output, params.AuthToken)
		return result, fmt.Errorf(
			"upgrade script failed: %w\nOutput:\n%s", runErr, redactedOutput,
		)
	}

	result.HasChanges = strings.Contains(result.Output, "CHANGES_PUSHED=true")
	result.DotnetVersionUpdated = strings.Contains(result.Output, "DOTNET_VERSION_UPDATED=true")
	return result, nil
}

func buildUpgradeScript(
	params upgradeParams,
	repoDir string,
) string {
	_ = repoDir // used via env vars in the script

	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	// Set up git credentials based on provider
	writeGitAuth(&sb, params)

	// Ensure git user identity is configured
	sb.WriteString("# Ensure git user identity is configured\n")
	sb.WriteString("if ! git config --global user.name > /dev/null 2>&1; then\n")
	sb.WriteString("    git config --global user.name \"autoupdate[bot]\"\n")
	sb.WriteString("fi\n")
	sb.WriteString("if ! git config --global user.email > /dev/null 2>&1; then\n")
	sb.WriteString("    git config --global user.email \"autoupdate[bot]@users.noreply.github.com\"\n")
	sb.WriteString("fi\n\n")

	// Clone
	sb.WriteString("echo \"Cloning repository...\"\n")
	sb.WriteString("git clone --depth=1 --branch \"$DEFAULT_BRANCH\" \"$CLONE_URL\" \"$REPO_DIR\" 2>&1\n")
	sb.WriteString("cd \"$REPO_DIR\"\n\n")

	// Create branch
	sb.WriteString("git checkout -b \"$BRANCH_NAME\" 2>&1\n\n")

	// .NET upgrade commands
	writeDotnetUpgradeCommands(&sb)

	// Update Dockerfile .NET image tags
	writeDockerfileUpdate(&sb)

	// Overwrite CHANGELOG.md with the pre-generated content (if provided)
	writeChangelogUpdate(&sb)

	// Check for changes and commit/push
	writeCommitAndPush(&sb)

	return sb.String()
}

func writeGitAuth(sb *strings.Builder, params upgradeParams) {
	sb.WriteString("# Set up isolated git config for auth\n")
	sb.WriteString("TEMP_GITCONFIG=$(mktemp)\n")
	sb.WriteString("cp ~/.gitconfig \"$TEMP_GITCONFIG\" 2>/dev/null || true\n")

	switch params.ProviderName {
	case providerAzureDevOps:
		sb.WriteString("echo '[url \"https://pat:'\"${AUTH_TOKEN}\"'@dev.azure.com/\"]' >> \"$TEMP_GITCONFIG\"\n")
		sb.WriteString("echo '    insteadOf = https://dev.azure.com/' >> \"$TEMP_GITCONFIG\"\n")
		sb.WriteString("echo '[url \"https://pat:'\"${AUTH_TOKEN}\"'@dev.azure.com/\"]' >> \"$TEMP_GITCONFIG\"\n")
		sb.WriteString("echo '    insteadOf = git@ssh.dev.azure.com:v3/' >> \"$TEMP_GITCONFIG\"\n")
	case providerGitHub:
		sb.WriteString(
			"echo '[url \"https://x-access-token:'\"${AUTH_TOKEN}\"'@github.com/\"]' >> \"$TEMP_GITCONFIG\"\n",
		)
		sb.WriteString("echo '    insteadOf = https://github.com/' >> \"$TEMP_GITCONFIG\"\n")
	case providerGitLab:
		sb.WriteString("echo '[url \"https://oauth2:'\"${AUTH_TOKEN}\"'@gitlab.com/\"]' >> \"$TEMP_GITCONFIG\"\n")
		sb.WriteString("echo '    insteadOf = https://gitlab.com/' >> \"$TEMP_GITCONFIG\"\n")
	}

	sb.WriteString("export GIT_CONFIG_GLOBAL=\"$TEMP_GITCONFIG\"\n")
	sb.WriteString("trap 'rm -f \"$TEMP_GITCONFIG\"' EXIT\n\n")
}

func writeDotnetUpgradeCommands(sb *strings.Builder) {
	// Update global.json SDK version if it exists and a new version is available
	sb.WriteString("# Check and update .NET SDK version in global.json\n")
	sb.WriteString("DOTNET_VERSION_CHANGED=false\n")
	sb.WriteString("if [ -n \"${DOTNET_VERSION:-}\" ] && [ -f \"global.json\" ]; then\n")
	sb.WriteString("    CURRENT_SDK_VERSION=$(\"$DOTNET_BINARY\" --version 2>/dev/null || true)\n")
	sb.WriteString("    # Parse current SDK version from global.json\n")
	sb.WriteString("    if command -v jq &>/dev/null; then\n")
	sb.WriteString("        CURRENT_GLOBAL_VERSION=$(jq -r '.sdk.version // empty' global.json)\n")
	sb.WriteString("    elif command -v python3 &>/dev/null; then\n")
	sb.WriteString("        CURRENT_GLOBAL_VERSION=$(python3 -c \"import json; " +
		"print(json.load(open('global.json')).get('sdk',{}).get('version',''))\")\n")
	sb.WriteString("    else\n")
	sb.WriteString("        CURRENT_GLOBAL_VERSION=$(grep -Eo " +
		"'\"version\"[[:space:]]*:[[:space:]]*\"[^\"]*\"' global.json | " +
		"head -1 | grep -o '[0-9][0-9.]*')\n")
	sb.WriteString("    fi\n")
	sb.WriteString(
		"    if [ -n \"$CURRENT_GLOBAL_VERSION\" ] && [ \"$CURRENT_GLOBAL_VERSION\" != \"$DOTNET_VERSION\" ]; then\n",
	)
	sb.WriteString("        echo \"Updating global.json SDK version " +
		"from $CURRENT_GLOBAL_VERSION to $DOTNET_VERSION...\"\n")
	sb.WriteString("        # Update global.json using JSON-aware approach via temp file\n")
	sb.WriteString("        if command -v python3 &>/dev/null; then\n")
	sb.WriteString("            python3 -c \"\n")
	sb.WriteString("import json, sys\n")
	sb.WriteString("with open('global.json', 'r') as f:\n")
	sb.WriteString("    data = json.load(f)\n")
	sb.WriteString("data['sdk']['version'] = sys.argv[1]\n")
	sb.WriteString("with open('global.json', 'w') as f:\n")
	sb.WriteString("    json.dump(data, f, indent=2)\n")
	sb.WriteString("    f.write('\\n')\n")
	sb.WriteString("\" \"$DOTNET_VERSION\"\n")
	sb.WriteString("        elif command -v jq &>/dev/null; then\n")
	sb.WriteString("            jq --arg v \"$DOTNET_VERSION\" '.sdk.version = $v' " +
		"global.json > global.json.tmp && mv global.json.tmp global.json\n")
	sb.WriteString("        else\n")
	sb.WriteString(
		"            echo \"WARNING: Neither python3 nor jq found, cannot update global.json SDK version\"\n",
	)
	sb.WriteString("        fi\n")
	sb.WriteString("        DOTNET_VERSION_CHANGED=true\n")
	sb.WriteString("        echo \"DOTNET_VERSION_UPDATED=true\"\n")
	sb.WriteString("    else\n")
	sb.WriteString("        echo \".NET SDK version already at $CURRENT_GLOBAL_VERSION, skipping version update\"\n")
	sb.WriteString("        echo \"DOTNET_VERSION_UPDATED=false\"\n")
	sb.WriteString("    fi\n")
	sb.WriteString("else\n")
	sb.WriteString("    echo \"DOTNET_VERSION_UPDATED=false\"\n")
	sb.WriteString("fi\n\n")

	writeNuGetUpdate(sb)
}

func writeNuGetUpdate(sb *strings.Builder) {
	sb.WriteString("# Restore NuGet packages\n")
	sb.WriteString("echo \"Restoring NuGet packages...\"\n")
	sb.WriteString("\"$DOTNET_BINARY\" restore 2>&1 || echo \"WARNING: dotnet restore had some errors\"\n\n")

	sb.WriteString("# Update NuGet packages in all .csproj files\n")
	sb.WriteString("echo \"Updating NuGet packages...\"\n")
	sb.WriteString("find . -type f -not -path './.git/*' -name '*.csproj' -print0 | ")
	sb.WriteString("while IFS= read -r -d '' csproj; do\n")
	sb.WriteString("    echo \"Processing $csproj...\"\n")
	sb.WriteString("    # Extract PackageReference Include names\n")
	sb.WriteString("    grep -o 'Include=\"[^\"]*\"' \"$csproj\" 2>/dev/null | sed 's/Include=\"//;s/\"//' | ")
	sb.WriteString("while IFS= read -r pkg; do\n")
	sb.WriteString("        echo \"  Updating package: $pkg\"\n")
	sb.WriteString("        \"$DOTNET_BINARY\" add \"$csproj\" package \"$pkg\" 2>&1 || ")
	sb.WriteString("echo \"WARNING: failed to update $pkg in $csproj\"\n")
	sb.WriteString("    done\n")
	sb.WriteString("done\n\n")
}

func writeDockerfileUpdate(sb *strings.Builder) {
	sb.WriteString("# Update Dockerfile .NET image tags when the SDK version was bumped.\n")
	sb.WriteString("if [ \"$DOTNET_VERSION_CHANGED\" = \"true\" ]; then\n")
	sb.WriteString("    # Extract major.minor from the full version (e.g. 8.0.11 -> 8.0)\n")
	sb.WriteString("    DOTNET_MAJOR_MINOR=$(echo \"$DOTNET_VERSION\" | cut -d '.' -f 1,2)\n")
	sb.WriteString("    echo \"Updating Dockerfile .NET image tags to $DOTNET_MAJOR_MINOR...\"\n")
	sb.WriteString(
		"    find . -type f -not -path './.git/*' " +
			"\\( -name 'Dockerfile' -o -name 'Dockerfile.*' -o -name '*.Dockerfile' \\) " +
			"-print0 | while IFS= read -r -d '' df; do\n",
	)
	sb.WriteString("        if grep -q 'mcr.microsoft.com/dotnet/sdk:[0-9]' \"$df\"; then\n")
	sb.WriteString(
		"            sed \"s|mcr.microsoft.com/dotnet/sdk:[0-9][0-9.]*|mcr.microsoft.com/dotnet/sdk:${DOTNET_MAJOR_MINOR}|g\" " +
			"\"$df\" > \"$df.tmp\" && mv \"$df.tmp\" \"$df\"\n",
	)
	sb.WriteString("            echo \"  Updated SDK image in $df\"\n")
	sb.WriteString("        fi\n")
	sb.WriteString("        if grep -q 'mcr.microsoft.com/dotnet/aspnet:[0-9]' \"$df\"; then\n")
	sb.WriteString(
		"            sed \"s|mcr.microsoft.com/dotnet/aspnet:[0-9][0-9.]*|mcr.microsoft.com/dotnet/aspnet:${DOTNET_MAJOR_MINOR}|g\" " +
			"\"$df\" > \"$df.tmp\" && mv \"$df.tmp\" \"$df\"\n",
	)
	sb.WriteString("            echo \"  Updated ASP.NET image in $df\"\n")
	sb.WriteString("        fi\n")
	sb.WriteString("        if grep -q 'mcr.microsoft.com/dotnet/runtime:[0-9]' \"$df\"; then\n")
	sb.WriteString(
		"            sed \"s|mcr.microsoft.com/dotnet/runtime:[0-9][0-9.]*|mcr.microsoft.com/dotnet/runtime:${DOTNET_MAJOR_MINOR}|g\" " +
			"\"$df\" > \"$df.tmp\" && mv \"$df.tmp\" \"$df\"\n",
	)
	sb.WriteString("            echo \"  Updated Runtime image in $df\"\n")
	sb.WriteString("        fi\n")
	sb.WriteString("    done\n")
	sb.WriteString("fi\n\n")
}

func writeChangelogUpdate(sb *strings.Builder) {
	sb.WriteString("# Update CHANGELOG.md only if the upgrade produced actual changes.\n")
	sb.WriteString("if [ -n \"${CHANGELOG_FILE:-}\" ] && [ -f \"$CHANGELOG_FILE\" ]; then\n")
	sb.WriteString("    if [ -n \"$(git status --porcelain)\" ]; then\n")
	sb.WriteString("        echo \"Updating CHANGELOG.md...\"\n")
	sb.WriteString("        cp \"$CHANGELOG_FILE\" CHANGELOG.md\n")
	sb.WriteString("    else\n")
	sb.WriteString("        echo \"No dependency changes detected, skipping CHANGELOG update.\"\n")
	sb.WriteString("    fi\n")
	sb.WriteString("fi\n\n")
}

func writeCommitAndPush(sb *strings.Builder) {
	sb.WriteString("if [ -n \"$(git status --porcelain)\" ]; then\n")
	sb.WriteString("    echo \"Changes detected, committing and pushing...\"\n")
	sb.WriteString("    git add -A\n")
	sb.WriteString("    if [ \"$DOTNET_VERSION_CHANGED\" = \"true\" ]; then\n")
	sb.WriteString(
		"        git commit -m \"chore(deps): upgraded .NET SDK to `$DOTNET_VERSION` and updated all NuGet dependencies\"\n",
	)
	sb.WriteString("    else\n")
	sb.WriteString("        git commit -m \"chore(deps): updated NuGet dependencies\"\n")
	sb.WriteString("    fi\n")
	sb.WriteString("    git push origin \"$BRANCH_NAME\" 2>&1\n")
	sb.WriteString("    echo \"CHANGES_PUSHED=true\"\n")
	sb.WriteString("else\n")
	sb.WriteString("    echo \"No changes detected.\"\n")
	sb.WriteString("    echo \"CHANGES_PUSHED=false\"\n")
	sb.WriteString("fi\n")
}

func buildEnv(params upgradeParams, repoDir string) []string {
	env := append(os.Environ(),
		"AUTH_TOKEN="+params.AuthToken,
		"GIT_HTTPS_TOKEN="+params.AuthToken,
		"CLONE_URL="+params.CloneURL,
		"BRANCH_NAME="+params.BranchName,
		"REPO_DIR="+repoDir,
		"DEFAULT_BRANCH="+params.DefaultBranch,
		"DOTNET_BINARY="+params.DotnetBinary,
	)
	if params.DotnetVersion != "" {
		env = append(env, "DOTNET_VERSION="+params.DotnetVersion)
	}
	if params.ChangelogFile != "" {
		env = append(env, "CHANGELOG_FILE="+params.ChangelogFile)
	}
	return env
}

func findDotnetBinary() (string, error) {
	if path, err := exec.LookPath("dotnet"); err == nil {
		return path, nil
	}

	commonPaths := []string{
		"/usr/bin/dotnet",
		"/usr/local/bin/dotnet",
		"/usr/share/dotnet/dotnet",
		"/snap/bin/dotnet",
	}

	home, _ := os.UserHomeDir()
	if home != "" {
		commonPaths = append(commonPaths,
			filepath.Join(home, ".dotnet", "dotnet"),
		)
	}

	for _, p := range commonPaths {
		if _, statErr := os.Stat(p); statErr == nil {
			return p, nil
		}
	}

	return "", errors.New("dotnet binary not found in PATH or common locations")
}

// GeneratePRDescription builds a markdown PR description for a .NET
// dependency upgrade. Exported so that the local-mode CLI handler can
// reuse the same description format.
func GeneratePRDescription(dotnetVersion string, dotnetVersionUpdated bool) string {
	var sb strings.Builder
	sb.WriteString("## Summary\n\n")
	if dotnetVersionUpdated {
		sb.WriteString(
			"This PR upgrades the .NET SDK version to **" + dotnetVersion + "** and updates all NuGet dependencies.\n\n",
		)
	} else {
		sb.WriteString(
			"This PR updates all NuGet dependencies to their latest versions.\n\n",
		)
	}
	sb.WriteString("### Changes\n\n")
	if dotnetVersionUpdated {
		sb.WriteString("- Updated `global.json` SDK version to `" + dotnetVersion + "`\n")
	}
	sb.WriteString("- Ran `dotnet restore` to restore packages\n")
	sb.WriteString("- Ran `dotnet add package` for each PackageReference to update to latest versions\n")
	sb.WriteString("\n### Review Checklist\n\n")
	sb.WriteString("- [ ] Verify build passes\n")
	sb.WriteString("- [ ] Verify tests pass\n")
	sb.WriteString("- [ ] Review dependency changes in `*.csproj` files\n")
	sb.WriteString("\n---\n")
	sb.WriteString("*This PR was automatically created by [autoupdate](https://github.com/rios0rios0/autoupdate)*\n")
	return sb.String()
}
