package javascript

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

	"github.com/rios0rios0/autoupdate/domain"
)

const (
	updaterName        = "javascript"
	nodeVersionTimeout = 15 * time.Second
	scriptFileMode     = 0o700

	// Package manager identifiers.
	pkgMgrPnpm = "pnpm"
	pkgMgrYarn = "yarn"
	pkgMgrNpm  = "npm"

	// Branch name patterns for JavaScript/Node.js updates.
	branchNodeVersionFmt = "chore/upgrade-node-%s"
	branchJSDepsFmt      = "chore/upgrade-js-deps"
)

// Updater implements domain.Updater for JavaScript/Node.js dependencies.
// It clones the repository locally, runs the appropriate package manager
// to update dependencies, pushes the changes, and creates a PR via the
// provider API.
type Updater struct{}

// New creates a new JavaScript updater.
func New() domain.Updater {
	return &Updater{}
}

func (u *Updater) Name() string { return updaterName }

// Detect returns true if the repository has a package.json file.
func (u *Updater) Detect(
	ctx context.Context,
	provider domain.Provider,
	repo domain.Repository,
) bool {
	return provider.HasFile(ctx, repo, "package.json")
}

// CreateUpdatePRs clones the repo, upgrades Node.js dependencies,
// and creates a PR.
func (u *Updater) CreateUpdatePRs(
	ctx context.Context,
	provider domain.Provider,
	repo domain.Repository,
	opts domain.UpdateOptions,
) ([]domain.PullRequest, error) {
	logger.Infof("[javascript] Processing %s/%s", repo.Organization, repo.Name)

	latestNodeVersion, err := fetchLatestNodeVersion(ctx)
	if err != nil {
		logger.Warnf(
			"[javascript] Failed to fetch latest Node.js version: %v (continuing without version upgrade)",
			err,
		)
		latestNodeVersion = ""
	} else {
		logger.Infof("[javascript] Latest Node.js LTS version: %s", latestNodeVersion)
	}

	vCtx := resolveVersionContext(ctx, provider, repo, latestNodeVersion)

	// Check if PR already exists
	exists, prCheckErr := provider.PullRequestExists(ctx, repo, vCtx.BranchName)
	if prCheckErr != nil {
		logger.Warnf("[javascript] Failed to check existing PRs: %v", prCheckErr)
	}
	if exists {
		logger.Infof(
			"[javascript] PR already exists for branch %q, skipping",
			vCtx.BranchName,
		)
		return []domain.PullRequest{}, nil
	}

	if opts.DryRun {
		logDryRun(vCtx, repo)
		return []domain.PullRequest{}, nil
	}

	pkgMgr := detectPackageManager(ctx, provider, repo)
	result, upgradeErr := cloneAndUpgrade(ctx, provider, repo, vCtx, pkgMgr)
	if upgradeErr != nil {
		return nil, upgradeErr
	}

	if !result.HasChanges {
		logger.Infof("[javascript] %s/%s: already up to date", repo.Organization, repo.Name)
		return []domain.PullRequest{}, nil
	}

	return openPullRequest(ctx, provider, repo, opts, vCtx, result, pkgMgr)
}

// logDryRun logs what would happen without actually performing the upgrade.
func logDryRun(vCtx *versionContext, repo domain.Repository) {
	if vCtx.NeedsVersionUpgrade {
		logger.Infof(
			"[javascript] [DRY RUN] Would upgrade Node.js to %s and update deps for %s/%s",
			vCtx.LatestVersion, repo.Organization, repo.Name,
		)
	} else {
		logger.Infof(
			"[javascript] [DRY RUN] Would update JavaScript dependencies for %s/%s",
			repo.Organization, repo.Name,
		)
	}
}

// cloneAndUpgrade prepares the changelog, clones the repository, runs the
// upgrade script, and returns the result.
func cloneAndUpgrade(
	ctx context.Context,
	provider domain.Provider,
	repo domain.Repository,
	vCtx *versionContext,
	pkgMgr string,
) (*upgradeResult, error) {
	changelogFile := prepareChangelog(ctx, provider, repo, vCtx)

	cloneURL := provider.CloneURL(repo)
	defaultBranch := strings.TrimPrefix(repo.DefaultBranch, "refs/heads/")

	result, err := upgradeRepo(ctx, upgradeParams{
		CloneURL:       cloneURL,
		DefaultBranch:  defaultBranch,
		BranchName:     vCtx.BranchName,
		NodeVersion:    vCtx.LatestVersion,
		AuthToken:      provider.AuthToken(),
		ProviderName:   provider.Name(),
		ChangelogFile:  changelogFile,
		PackageManager: pkgMgr,
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
	provider domain.Provider,
	repo domain.Repository,
	opts domain.UpdateOptions,
	vCtx *versionContext,
	result *upgradeResult,
	pkgMgr string,
) ([]domain.PullRequest, error) {
	targetBranch := repo.DefaultBranch
	if opts.TargetBranch != "" {
		targetBranch = "refs/heads/" + opts.TargetBranch
	}

	prTitle := "chore(deps): updated JavaScript dependencies"
	if result.NodeVersionUpdated {
		prTitle = fmt.Sprintf(
			"chore(deps): upgraded Node.js to `%s` and updated all dependencies",
			vCtx.LatestVersion,
		)
	}
	prDesc := GeneratePRDescription(vCtx.LatestVersion, pkgMgr, result.NodeVersionUpdated)

	pr, createErr := provider.CreatePullRequest(ctx, repo, domain.PullRequestInput{
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
		"[javascript] Created PR #%d for %s/%s: %s",
		pr.ID, repo.Organization, repo.Name, pr.URL,
	)
	return []domain.PullRequest{*pr}, nil
}

// --- internal types ---

type versionContext struct {
	LatestVersion       string
	NeedsVersionUpgrade bool
	BranchName          string
}

type upgradeParams struct {
	CloneURL       string
	DefaultBranch  string
	BranchName     string
	NodeVersion    string
	AuthToken      string
	ProviderName   string
	ChangelogFile  string
	PackageManager string // "npm", "yarn", or "pnpm"
}

type upgradeResult struct {
	HasChanges         bool
	NodeVersionUpdated bool
	Output             string
}

// --- Node.js version fetching ---

type nodeRelease struct {
	Version string `json:"version"`
	LTS     any    `json:"lts"` // false or string like "Jod"
}

func fetchLatestNodeVersion(ctx context.Context) (string, error) {
	client := &http.Client{Timeout: nodeVersionTimeout}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, "https://nodejs.org/dist/index.json", nil,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Node.js versions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var releases []nodeRelease
	if decodeErr := json.NewDecoder(resp.Body).Decode(&releases); decodeErr != nil {
		return "", fmt.Errorf("failed to parse Node.js versions: %w", decodeErr)
	}

	for _, release := range releases {
		if isLTSRelease(release) {
			return strings.TrimPrefix(release.Version, "v"), nil
		}
	}

	return "", errors.New("no LTS Node.js version found")
}

// isLTSRelease returns true if the Node.js release is an LTS version.
// The LTS field is false for non-LTS releases and a string (codename)
// for LTS releases.
func isLTSRelease(release nodeRelease) bool {
	switch v := release.LTS.(type) {
	case string:
		return v != ""
	case bool:
		return v
	default:
		return false
	}
}

// parseNodeVersionFile extracts the Node.js version from a .nvmrc or
// .node-version file content.
func parseNodeVersionFile(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			// Strip leading "v" if present
			return strings.TrimPrefix(line, "v")
		}
	}
	return ""
}

// --- package manager detection ---

// detectPackageManager determines which package manager the repository uses
// by checking for lockfiles.
func detectPackageManager(
	ctx context.Context,
	provider domain.Provider,
	repo domain.Repository,
) string {
	if provider.HasFile(ctx, repo, "pnpm-lock.yaml") {
		return pkgMgrPnpm
	}
	if provider.HasFile(ctx, repo, "yarn.lock") {
		return pkgMgrYarn
	}
	return pkgMgrNpm // default
}

// --- version context ---

// resolveVersionContext reads the remote .nvmrc or .node-version to find
// the current Node.js version and picks the right branch-name pattern.
func resolveVersionContext(
	ctx context.Context,
	provider domain.Provider,
	repo domain.Repository,
	latestNodeVersion string,
) *versionContext {
	needsVersionUpgrade := false

	if latestNodeVersion != "" {
		currentVersion := readCurrentNodeVersion(ctx, provider, repo)
		if currentVersion != "" {
			needsVersionUpgrade = currentVersion != latestNodeVersion
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

// readCurrentNodeVersion tries to read the Node.js version from .nvmrc
// or .node-version files.
func readCurrentNodeVersion(
	ctx context.Context,
	provider domain.Provider,
	repo domain.Repository,
) string {
	for _, versionFile := range []string{".nvmrc", ".node-version"} {
		if provider.HasFile(ctx, repo, versionFile) {
			content, err := provider.GetFileContent(ctx, repo, versionFile)
			if err == nil {
				version := parseNodeVersionFile(content)
				if version != "" {
					return version
				}
			}
		}
	}
	return ""
}

// prepareChangelog reads the target repo's CHANGELOG.md (if it exists),
// inserts an entry describing the JavaScript upgrade, and writes the modified
// content to a temp file.
func prepareChangelog(
	ctx context.Context,
	provider domain.Provider,
	repo domain.Repository,
	vCtx *versionContext,
) string {
	if !provider.HasFile(ctx, repo, "CHANGELOG.md") {
		return ""
	}

	content, err := provider.GetFileContent(ctx, repo, "CHANGELOG.md")
	if err != nil {
		logger.Warnf("[javascript] Failed to read CHANGELOG.md: %v", err)
		return ""
	}

	var entry string
	if vCtx.NeedsVersionUpgrade {
		entry = fmt.Sprintf(
			"- changed the Node.js version to `%s` and updated all JavaScript dependencies",
			vCtx.LatestVersion,
		)
	} else {
		entry = "- changed the JavaScript dependencies to their latest versions"
	}

	modified := domain.InsertChangelogEntry(content, []string{entry})
	if modified == content {
		return ""
	}

	tmpFile, writeErr := os.CreateTemp("", "changelog-*.md")
	if writeErr != nil {
		logger.Warnf("[javascript] Failed to create temp changelog file: %v", writeErr)
		return ""
	}

	if _, writeErr = tmpFile.WriteString(modified); writeErr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		logger.Warnf("[javascript] Failed to write temp changelog: %v", writeErr)
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

	tmpDir, err := os.MkdirTemp("", "autoupdate-js-*")
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

	cmd := exec.CommandContext(ctx, "bash", scriptPath)
	cmd.Dir = tmpDir
	cmd.Env = buildEnv(params, repoDir)

	output, err := cmd.CombinedOutput()
	result.Output = string(output)

	if err != nil {
		return result, fmt.Errorf(
			"upgrade script failed: %w\nOutput:\n%s", err, result.Output,
		)
	}

	result.HasChanges = strings.Contains(result.Output, "CHANGES_PUSHED=true")
	result.NodeVersionUpdated = strings.Contains(result.Output, "NODE_VERSION_UPDATED=true")
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

	// JavaScript upgrade commands
	writeJSUpgradeCommands(&sb, params)

	// Update Dockerfile node image tags
	writeDockerfileUpdate(&sb)

	// Overwrite CHANGELOG.md with the pre-generated content
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
	case "azuredevops":
		sb.WriteString("echo '[url \"https://pat:'\"${AUTH_TOKEN}\"'@dev.azure.com/\"]' >> \"$TEMP_GITCONFIG\"\n")
		sb.WriteString("echo '    insteadOf = https://dev.azure.com/' >> \"$TEMP_GITCONFIG\"\n")
		sb.WriteString("echo '[url \"https://pat:'\"${AUTH_TOKEN}\"'@dev.azure.com/\"]' >> \"$TEMP_GITCONFIG\"\n")
		sb.WriteString("echo '    insteadOf = git@ssh.dev.azure.com:v3/' >> \"$TEMP_GITCONFIG\"\n")
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

func writeJSUpgradeCommands(sb *strings.Builder, _ upgradeParams) {
	// Update .nvmrc / .node-version if it exists and a new version is available
	sb.WriteString("# Check and update Node.js version\n")
	sb.WriteString("NODE_VERSION_CHANGED=false\n")
	sb.WriteString("if [ -n \"${NODE_VERSION:-}\" ]; then\n")
	sb.WriteString("    for VERSION_FILE in .nvmrc .node-version; do\n")
	sb.WriteString("        if [ -f \"$VERSION_FILE\" ]; then\n")
	sb.WriteString("            CURRENT_NODE_VERSION=$(head -1 \"$VERSION_FILE\" | tr -d '[:space:]' | sed 's/^v//')\n")
	sb.WriteString(
		"            if [ -n \"$CURRENT_NODE_VERSION\" ] && [ \"$CURRENT_NODE_VERSION\" != \"$NODE_VERSION\" ]; then\n",
	)
	sb.WriteString("                echo \"Updating $VERSION_FILE from $CURRENT_NODE_VERSION to $NODE_VERSION...\"\n")
	sb.WriteString("                echo \"$NODE_VERSION\" > \"$VERSION_FILE\"\n")
	sb.WriteString("                NODE_VERSION_CHANGED=true\n")
	sb.WriteString("                echo \"NODE_VERSION_UPDATED=true\"\n")
	sb.WriteString("            fi\n")
	sb.WriteString("        fi\n")
	sb.WriteString("    done\n")
	sb.WriteString("fi\n")
	sb.WriteString("if [ \"$NODE_VERSION_CHANGED\" = \"false\" ]; then\n")
	sb.WriteString("    echo \"NODE_VERSION_UPDATED=false\"\n")
	sb.WriteString("fi\n\n")

	// Run package manager update
	sb.WriteString("# Update dependencies using detected package manager\n")
	sb.WriteString("echo \"Using package manager: $PACKAGE_MANAGER\"\n")
	sb.WriteString("case \"$PACKAGE_MANAGER\" in\n")
	sb.WriteString("    pnpm)\n")
	sb.WriteString("        echo \"Running pnpm update...\"\n")
	sb.WriteString("        pnpm update 2>&1 || echo \"WARNING: pnpm update had some errors (continuing anyway)\"\n")
	sb.WriteString("        ;;\n")
	sb.WriteString("    yarn)\n")
	sb.WriteString("        echo \"Running yarn upgrade...\"\n")
	sb.WriteString("        yarn upgrade 2>&1 || echo \"WARNING: yarn upgrade had some errors (continuing anyway)\"\n")
	sb.WriteString("        ;;\n")
	sb.WriteString("    *)\n")
	sb.WriteString("        echo \"Running npm update...\"\n")
	sb.WriteString("        npm update 2>&1 || echo \"WARNING: npm update had some errors (continuing anyway)\"\n")
	sb.WriteString("        ;;\n")
	sb.WriteString("esac\n\n")
}

func writeDockerfileUpdate(sb *strings.Builder) {
	sb.WriteString("# Update Dockerfile node image tags when the Node.js version was bumped.\n")
	sb.WriteString("if [ \"$NODE_VERSION_CHANGED\" = \"true\" ]; then\n")
	sb.WriteString("    echo \"Updating Dockerfile node image tags to $NODE_VERSION...\"\n")
	sb.WriteString(
		"    find . -type f -not -path './.git/*' " +
			"\\( -name 'Dockerfile' -o -name 'Dockerfile.*' -o -name '*.Dockerfile' \\) " +
			"-print0 | while IFS= read -r -d '' df; do\n",
	)
	sb.WriteString("        if grep -q 'node:[0-9]' \"$df\"; then\n")
	sb.WriteString(
		"            sed \"s|node:[0-9][0-9.]*|node:${NODE_VERSION}|g\" \"$df\" > \"$df.tmp\" && mv \"$df.tmp\" \"$df\"\n",
	)
	sb.WriteString("            echo \"  Updated $df\"\n")
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
	sb.WriteString("    if [ \"$NODE_VERSION_CHANGED\" = \"true\" ]; then\n")
	sb.WriteString(
		"        git commit -m \"chore(deps): upgraded Node.js to `$NODE_VERSION` and updated all dependencies\"\n",
	)
	sb.WriteString("    else\n")
	sb.WriteString("        git commit -m \"chore(deps): updated JavaScript dependencies\"\n")
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
		"PACKAGE_MANAGER="+params.PackageManager,
	)
	if params.NodeVersion != "" {
		env = append(env, "NODE_VERSION="+params.NodeVersion)
	}
	if params.ChangelogFile != "" {
		env = append(env, "CHANGELOG_FILE="+params.ChangelogFile)
	}
	return env
}

// GeneratePRDescription builds a markdown PR description for a JavaScript
// dependency upgrade. Exported so that the local-mode CLI handler can
// reuse the same description format.
func GeneratePRDescription(nodeVersion, pkgMgr string, nodeVersionUpdated bool) string {
	var sb strings.Builder
	sb.WriteString("## Summary\n\n")
	if nodeVersionUpdated {
		sb.WriteString(
			"This PR upgrades the Node.js version to **" + nodeVersion + "** and updates all JavaScript dependencies.\n\n",
		)
	} else {
		sb.WriteString(
			"This PR updates all JavaScript dependencies to their latest versions.\n\n",
		)
	}
	sb.WriteString("### Changes\n\n")
	if nodeVersionUpdated {
		sb.WriteString("- Updated `.nvmrc` / `.node-version` to `" + nodeVersion + "`\n")
	}

	switch pkgMgr {
	case "pnpm":
		sb.WriteString("- Ran `pnpm update` to update all dependencies\n")
	case "yarn":
		sb.WriteString("- Ran `yarn upgrade` to update all dependencies\n")
	default:
		sb.WriteString("- Ran `npm update` to update all dependencies\n")
	}

	sb.WriteString("\n### Review Checklist\n\n")
	sb.WriteString("- [ ] Verify build passes\n")
	sb.WriteString("- [ ] Verify tests pass\n")
	sb.WriteString("- [ ] Review dependency changes in lockfile\n")
	sb.WriteString("\n---\n")
	sb.WriteString("*This PR was automatically created by [autoupdate](https://github.com/rios0rios0/autoupdate)*\n")
	return sb.String()
}
