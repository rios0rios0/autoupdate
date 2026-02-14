package python

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
)

const (
	updaterName      = "python"
	pyVersionTimeout = 15 * time.Second
	scriptFileMode   = 0o700

	// Branch name patterns for Python updates. One format is used when the
	// Python runtime version itself is being bumped; the other is used when
	// only pip dependencies are being refreshed.
	branchPyVersionFmt = "chore/upgrade-python-%s"
	branchPyDepsFmt    = "chore/upgrade-python-deps"
)

// Updater implements repositories.UpdaterRepository for Python dependencies.
// It clones the repository locally, runs pip commands to update
// dependencies, pushes the changes, and creates a PR via the provider API.
type PythonUpdaterRepository struct{}

// New creates a new Python updater.
func NewPythonUpdaterRepository() repositories.UpdaterRepository {
	return &PythonUpdaterRepository{}
}

func (u *PythonUpdaterRepository) Name() string { return updaterName }

// Detect returns true if the repository has a requirements.txt or pyproject.toml file.
func (u *PythonUpdaterRepository) Detect(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) bool {
	return provider.HasFile(ctx, repo, "requirements.txt") ||
		provider.HasFile(ctx, repo, "pyproject.toml")
}

// CreateUpdatePRs clones the repo, upgrades Python dependencies,
// and creates a PR.
func (u *PythonUpdaterRepository) CreateUpdatePRs(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
) ([]entities.PullRequest, error) {
	logger.Infof("[python] Processing %s/%s", repo.Organization, repo.Name)

	latestPyVersion, err := fetchLatestPythonVersion(ctx)
	if err != nil {
		logger.Warnf("[python] Failed to fetch latest Python version: %v (continuing without version upgrade)", err)
		latestPyVersion = ""
	} else {
		logger.Infof("[python] Latest stable Python version: %s", latestPyVersion)
	}

	vCtx := resolveVersionContext(ctx, provider, repo, latestPyVersion)

	// Check if PR already exists
	exists, prCheckErr := provider.PullRequestExists(ctx, repo, vCtx.BranchName)
	if prCheckErr != nil {
		logger.Warnf("[python] Failed to check existing PRs: %v", prCheckErr)
	}
	if exists {
		logger.Infof(
			"[python] PR already exists for branch %q, skipping",
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
		logger.Infof("[python] %s/%s: already up to date", repo.Organization, repo.Name)
		return []entities.PullRequest{}, nil
	}

	return openPullRequest(ctx, provider, repo, opts, vCtx, result)
}

// logDryRun logs what would happen without actually performing the upgrade.
func logDryRun(vCtx *versionContext, repo entities.Repository) {
	if vCtx.NeedsVersionUpgrade {
		logger.Infof(
			"[python] [DRY RUN] Would upgrade Python to %s and update deps for %s/%s",
			vCtx.LatestVersion, repo.Organization, repo.Name,
		)
	} else {
		logger.Infof(
			"[python] [DRY RUN] Would update Python dependencies for %s/%s",
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
	hasRequirements := provider.HasFile(ctx, repo, "requirements.txt")
	hasPyproject := provider.HasFile(ctx, repo, "pyproject.toml")

	cloneURL := provider.CloneURL(repo)
	defaultBranch := strings.TrimPrefix(repo.DefaultBranch, "refs/heads/")

	pythonBinary, err := findPythonBinary()
	if err != nil {
		return nil, fmt.Errorf("python binary not found: %w", err)
	}

	result, err := upgradeRepo(ctx, upgradeParams{
		CloneURL:        cloneURL,
		DefaultBranch:   defaultBranch,
		BranchName:      vCtx.BranchName,
		PythonVersion:   vCtx.LatestVersion,
		AuthToken:       provider.AuthToken(),
		ProviderName:    provider.Name(),
		ChangelogFile:   changelogFile,
		HasRequirements: hasRequirements,
		HasPyproject:    hasPyproject,
		PythonBinary:    pythonBinary,
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

	prTitle := "chore(deps): updated Python dependencies"
	if result.PythonVersionUpdated {
		prTitle = fmt.Sprintf(
			"chore(deps): upgraded Python to `%s` and updated all dependencies",
			vCtx.LatestVersion,
		)
	}
	prDesc := GeneratePRDescription(vCtx.LatestVersion, result.PythonVersionUpdated)

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
		"[python] Created PR #%d for %s/%s: %s",
		pr.ID, repo.Organization, repo.Name, pr.URL,
	)
	return []entities.PullRequest{*pr}, nil
}

// --- internal types ---

type versionContext struct {
	LatestVersion       string
	NeedsVersionUpgrade bool
	BranchName          string
}

type upgradeParams struct {
	CloneURL        string
	DefaultBranch   string
	BranchName      string
	PythonVersion   string
	AuthToken       string
	ProviderName    string
	ChangelogFile   string
	HasRequirements bool
	HasPyproject    bool
	PythonBinary    string
}

type upgradeResult struct {
	HasChanges           bool
	PythonVersionUpdated bool
	Output               string
}

// --- Python version fetching ---

type pythonRelease struct {
	Cycle  string `json:"cycle"`
	Latest string `json:"latest"`
	EOL    any    `json:"eol"` // bool (false) or string date
}

func fetchLatestPythonVersion(ctx context.Context) (string, error) {
	client := &http.Client{Timeout: pyVersionTimeout}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, "https://endoflife.date/api/python.json", nil,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Python versions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var releases []pythonRelease
	if decodeErr := json.NewDecoder(resp.Body).Decode(&releases); decodeErr != nil {
		return "", fmt.Errorf("failed to parse Python versions: %w", decodeErr)
	}

	for _, release := range releases {
		if isActiveRelease(release) {
			return release.Latest, nil
		}
	}

	return "", errors.New("no active Python release found")
}

// isActiveRelease returns true if the Python release cycle has not reached
// end-of-life. The EOL field is false when still active, or a date string
// when it has an EOL date — we check if that date is in the future.
func isActiveRelease(release pythonRelease) bool {
	switch v := release.EOL.(type) {
	case bool:
		return !v
	case string:
		eolDate, err := time.Parse("2006-01-02", v)
		if err != nil {
			return false
		}
		return eolDate.After(time.Now())
	default:
		return false
	}
}

// parsePythonVersionFile extracts the Python version from a .python-version
// file content. The file typically contains just a version string like "3.12.8".
func parsePythonVersionFile(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return ""
}

// --- version context ---

// resolveVersionContext reads the remote .python-version to find the current
// Python version and picks the right branch-name pattern.
func resolveVersionContext(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	latestPyVersion string,
) *versionContext {
	needsVersionUpgrade := false

	if latestPyVersion != "" && provider.HasFile(ctx, repo, ".python-version") {
		content, err := provider.GetFileContent(ctx, repo, ".python-version")
		if err == nil {
			currentVersion := parsePythonVersionFile(content)
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

// prepareChangelog reads the target repo's CHANGELOG.md (if it exists),
// inserts an entry describing the Python upgrade, and writes the modified
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
		logger.Warnf("[python] Failed to read CHANGELOG.md: %v", err)
		return ""
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

	modified := entities.InsertChangelogEntry(content, []string{entry})
	if modified == content {
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

// --- clone + upgrade ---

func upgradeRepo(
	ctx context.Context,
	params upgradeParams,
) (*upgradeResult, error) {
	result := &upgradeResult{}

	tmpDir, err := os.MkdirTemp("", "autoupdate-python-*")
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
	result.PythonVersionUpdated = strings.Contains(result.Output, "PYTHON_VERSION_UPDATED=true")
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

	// Python upgrade commands
	writePythonUpgradeCommands(&sb, params)

	// Update Dockerfile python image tags
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

func writePythonUpgradeCommands(sb *strings.Builder, params upgradeParams) {
	// Update .python-version if it exists and a new version is available
	sb.WriteString("# Check and update Python version\n")
	sb.WriteString("PYTHON_VERSION_CHANGED=false\n")
	sb.WriteString("if [ -n \"${PYTHON_VERSION:-}\" ] && [ -f \".python-version\" ]; then\n")
	sb.WriteString("    CURRENT_PY_VERSION=$(head -1 .python-version | tr -d '[:space:]')\n")
	sb.WriteString(
		"    if [ -n \"$CURRENT_PY_VERSION\" ] && [ \"$CURRENT_PY_VERSION\" != \"$PYTHON_VERSION\" ]; then\n",
	)
	sb.WriteString("        echo \"Updating .python-version from $CURRENT_PY_VERSION to $PYTHON_VERSION...\"\n")
	sb.WriteString("        echo \"$PYTHON_VERSION\" > .python-version\n")
	sb.WriteString("        PYTHON_VERSION_CHANGED=true\n")
	sb.WriteString("        echo \"PYTHON_VERSION_UPDATED=true\"\n")
	sb.WriteString("    else\n")
	sb.WriteString("        echo \"Python version already at $CURRENT_PY_VERSION, skipping version update\"\n")
	sb.WriteString("        echo \"PYTHON_VERSION_UPDATED=false\"\n")
	sb.WriteString("    fi\n")
	sb.WriteString("else\n")
	sb.WriteString("    echo \"PYTHON_VERSION_UPDATED=false\"\n")
	sb.WriteString("fi\n\n")

	// Create virtual environment and upgrade dependencies
	sb.WriteString("# Create virtual environment for dependency upgrade\n")
	sb.WriteString("VENV_DIR=$(mktemp -d)\n")
	sb.WriteString("\"$PYTHON_BINARY\" -m venv \"$VENV_DIR\"\n")
	sb.WriteString("# shellcheck disable=SC1091\n")
	sb.WriteString("source \"$VENV_DIR/bin/activate\"\n")
	sb.WriteString("pip install --upgrade pip 2>&1 || echo \"WARNING: pip upgrade had some errors\"\n\n")

	if params.HasRequirements {
		sb.WriteString("# Upgrade dependencies from requirements.txt\n")
		sb.WriteString("if [ -f \"requirements.txt\" ]; then\n")
		sb.WriteString("    echo \"Installing current requirements...\"\n")
		sb.WriteString("    pip install -r requirements.txt 2>&1 || echo \"WARNING: pip install had some errors\"\n\n")
		sb.WriteString("    echo \"Upgrading all packages...\"\n")
		sb.WriteString(
			"    pip install --upgrade -r requirements.txt 2>&1 || echo \"WARNING: pip upgrade had some errors\"\n\n",
		)
		sb.WriteString("    echo \"Freezing updated requirements...\"\n")
		sb.WriteString("    pip freeze > requirements.txt\n")
		sb.WriteString("fi\n\n")
	}

	if params.HasPyproject {
		sb.WriteString("# Upgrade dependencies from pyproject.toml\n")
		sb.WriteString("if [ -f \"pyproject.toml\" ]; then\n")
		sb.WriteString("    echo \"Upgrading pyproject.toml dependencies...\"\n")
		sb.WriteString(
			"    pip install --upgrade . 2>&1 || echo \"WARNING: pip install --upgrade . had some errors\"\n",
		)
		sb.WriteString("    if [ -f \"requirements.txt\" ]; then\n")
		sb.WriteString("        pip freeze > requirements.txt\n")
		sb.WriteString("    fi\n")
		sb.WriteString("fi\n\n")
	}

	sb.WriteString("deactivate 2>/dev/null || true\n")
	sb.WriteString("rm -rf \"$VENV_DIR\"\n\n")
}

func writeDockerfileUpdate(sb *strings.Builder) {
	sb.WriteString("# Update Dockerfile python image tags when the Python version was bumped.\n")
	sb.WriteString("if [ \"$PYTHON_VERSION_CHANGED\" = \"true\" ]; then\n")
	sb.WriteString("    echo \"Updating Dockerfile python image tags to $PYTHON_VERSION...\"\n")
	sb.WriteString(
		"    find . -type f -not -path './.git/*' " +
			"\\( -name 'Dockerfile' -o -name 'Dockerfile.*' -o -name '*.Dockerfile' \\) " +
			"-print0 | while IFS= read -r -d '' df; do\n",
	)
	sb.WriteString("        if grep -q 'python:[0-9]' \"$df\"; then\n")
	sb.WriteString(
		"            sed \"s|python:[0-9][0-9.]*|python:${PYTHON_VERSION}|g\" \"$df\" > \"$df.tmp\" && mv \"$df.tmp\" \"$df\"\n",
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
	sb.WriteString("    if [ \"$PYTHON_VERSION_CHANGED\" = \"true\" ]; then\n")
	sb.WriteString(
		"        git commit -m \"chore(deps): upgraded Python to `$PYTHON_VERSION` and updated all dependencies\"\n",
	)
	sb.WriteString("    else\n")
	sb.WriteString("        git commit -m \"chore(deps): updated Python dependencies\"\n")
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
		"PYTHON_BINARY="+params.PythonBinary,
	)
	if params.PythonVersion != "" {
		env = append(env, "PYTHON_VERSION="+params.PythonVersion)
	}
	if params.ChangelogFile != "" {
		env = append(env, "CHANGELOG_FILE="+params.ChangelogFile)
	}
	return env
}

func findPythonBinary() (string, error) {
	// Try python3 first, then python
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}

	commonPaths := []string{
		"/usr/bin/python3",
		"/usr/local/bin/python3",
		"/usr/bin/python",
		"/usr/local/bin/python",
	}

	home, _ := os.UserHomeDir()
	if home != "" {
		commonPaths = append(commonPaths,
			filepath.Join(home, ".pyenv", "shims", "python3"),
			filepath.Join(home, ".pyenv", "shims", "python"),
		)
	}

	for _, p := range commonPaths {
		if _, statErr := os.Stat(p); statErr == nil {
			return p, nil
		}
	}

	return "", errors.New("python binary not found in PATH or common locations")
}

// GeneratePRDescription builds a markdown PR description for a Python
// dependency upgrade. Exported so that the local-mode CLI handler can
// reuse the same description format.
func GeneratePRDescription(pyVersion string, pyVersionUpdated bool) string {
	var sb strings.Builder
	sb.WriteString("## Summary\n\n")
	if pyVersionUpdated {
		sb.WriteString(
			"This PR upgrades the Python version to **" + pyVersion + "** and updates all pip dependencies.\n\n",
		)
	} else {
		sb.WriteString(
			"This PR updates all Python pip dependencies to their latest versions.\n\n",
		)
	}
	sb.WriteString("### Changes\n\n")
	if pyVersionUpdated {
		sb.WriteString("- Updated `.python-version` to `" + pyVersion + "`\n")
	}
	sb.WriteString("- Ran `pip install --upgrade -r requirements.txt` to update all dependencies\n")
	sb.WriteString("- Ran `pip freeze` to capture updated versions\n")
	sb.WriteString("\n### Review Checklist\n\n")
	sb.WriteString("- [ ] Verify build passes\n")
	sb.WriteString("- [ ] Verify tests pass\n")
	sb.WriteString("- [ ] Review dependency changes in `requirements.txt`\n")
	sb.WriteString("\n---\n")
	sb.WriteString("*This PR was automatically created by [autoupdate](https://github.com/rios0rios0/autoupdate)*\n")
	return sb.String()
}
