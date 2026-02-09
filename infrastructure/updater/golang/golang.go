package golang

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
	updaterName      = "golang"
	goVersionTimeout = 15 * time.Second
	scriptFileMode   = 0o700

	// Branch name patterns — mirroring the Terraform updater's dual-branch
	// approach.  One format is used when the Go version itself is being
	// bumped (version upgrade); the other when the go directive is already
	// at the latest and only module dependencies are being refreshed.
	branchGoVersionFmt = "chore/upgrade-go-%s"
	branchGoDepsFmt    = "chore/upgrade-deps-%s"
)

// Updater implements domain.Updater for Go module dependencies.
// It clones the repository locally, runs go commands to update
// dependencies, pushes the changes, and creates a PR via the provider API.
type Updater struct{}

// New creates a new Go updater.
func New() domain.Updater {
	return &Updater{}
}

func (u *Updater) Name() string { return updaterName }

// Detect returns true if the repository has a go.mod file.
func (u *Updater) Detect(
	ctx context.Context,
	provider domain.Provider,
	repo domain.Repository,
) bool {
	return provider.HasFile(ctx, repo, "go.mod")
}

// CreateUpdatePRs clones the repo, upgrades Go version and
// dependencies, and creates a PR.
func (u *Updater) CreateUpdatePRs(
	ctx context.Context,
	provider domain.Provider,
	repo domain.Repository,
	opts domain.UpdateOptions,
) ([]domain.PullRequest, error) {
	logger.Infof("[golang] Processing %s/%s", repo.Organization, repo.Name)

	vCtx, err := resolveVersionContext(ctx, provider, repo)
	if err != nil {
		return nil, err
	}

	// Check if PR already exists
	exists, prCheckErr := provider.PullRequestExists(ctx, repo, vCtx.BranchName)
	if prCheckErr != nil {
		logger.Warnf("[golang] Failed to check existing PRs: %v", prCheckErr)
	}
	if exists {
		logger.Infof(
			"[golang] PR already exists for branch %q, skipping",
			vCtx.BranchName,
		)
		return []domain.PullRequest{}, nil
	}

	if opts.DryRun {
		logger.Infof(
			"[golang] [DRY RUN] Would upgrade Go to %s and update deps for %s/%s",
			vCtx.LatestVersion, repo.Organization, repo.Name,
		)
		return []domain.PullRequest{}, nil
	}

	// Check if config.sh exists (for Azure DevOps private package setups)
	hasConfigSH := provider.HasFile(ctx, repo, "config.sh")

	// Clone, upgrade, push
	cloneURL := provider.CloneURL(repo)
	defaultBranch := strings.TrimPrefix(repo.DefaultBranch, "refs/heads/")

	result, upgradeErr := upgradeGoRepo(ctx, upgradeParams{
		CloneURL:      cloneURL,
		DefaultBranch: defaultBranch,
		BranchName:    vCtx.BranchName,
		GoVersion:     vCtx.LatestVersion,
		AuthToken:     provider.AuthToken(),
		HasConfigSH:   hasConfigSH,
		ProviderName:  provider.Name(),
	})
	if upgradeErr != nil {
		return nil, fmt.Errorf("failed to upgrade: %w", upgradeErr)
	}

	if !result.HasChanges {
		logger.Infof(
			"[golang] %s/%s: already up to date",
			repo.Organization, repo.Name,
		)
		return []domain.PullRequest{}, nil
	}

	// Create PR
	targetBranch := repo.DefaultBranch
	if opts.TargetBranch != "" {
		targetBranch = "refs/heads/" + opts.TargetBranch
	}

	prTitle := "chore(deps): update Go module dependencies"
	if vCtx.NeedsVersionUpgrade {
		prTitle = fmt.Sprintf(
			"chore(deps): upgrade Go to %s and update dependencies",
			vCtx.LatestVersion,
		)
	}
	prDesc := generateGoPRDescription(vCtx.LatestVersion, hasConfigSH, vCtx.NeedsVersionUpgrade)

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
		"[golang] Created PR #%d for %s/%s: %s",
		pr.ID, repo.Organization, repo.Name, pr.URL,
	)
	return []domain.PullRequest{*pr}, nil
}

// versionContext holds the pre-resolved Go version information and the
// branch name derived from it.  Extracted from CreateUpdatePRs to keep
// that method within the project's funlen limit.
type versionContext struct {
	LatestVersion       string
	NeedsVersionUpgrade bool
	BranchName          string
}

// resolveVersionContext fetches the latest stable Go version, reads the
// remote go.mod to find the current directive, and picks the right
// branch-name pattern (version-upgrade vs deps-only).
func resolveVersionContext(
	ctx context.Context,
	provider domain.Provider,
	repo domain.Repository,
) (*versionContext, error) {
	latestGoVersion, err := fetchLatestGoVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch latest Go version: %w", err)
	}
	logger.Infof("[golang] Latest stable Go version: %s", latestGoVersion)

	// Read the current go.mod from the remote to decide whether this is a
	// version upgrade or a deps-only refresh — before cloning.
	needsVersionUpgrade := true // safe default when go.mod cannot be read
	goModContent, goModErr := provider.GetFileContent(ctx, repo, "go.mod")
	if goModErr != nil {
		logger.Warnf("[golang] Could not read remote go.mod, assuming version upgrade: %v", goModErr)
	} else {
		currentGoVersion := parseGoDirective(goModContent)
		needsVersionUpgrade = currentGoVersion != latestGoVersion
		logger.Infof("[golang] Current go directive: %s (upgrade needed: %v)", currentGoVersion, needsVersionUpgrade)
	}

	// Choose the branch name pattern based on the kind of change, following
	// the same dual-branch idea used by the Terraform updater.
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

// --- internal types ---

type upgradeParams struct {
	CloneURL      string
	DefaultBranch string
	BranchName    string
	GoVersion     string
	AuthToken     string
	HasConfigSH   bool
	ProviderName  string
}

type upgradeResult struct {
	HasChanges       bool
	GoVersionUpdated bool
	Output           string
}

// --- Go version fetching ---

type goRelease struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

func fetchLatestGoVersion(ctx context.Context) (string, error) {
	client := &http.Client{Timeout: goVersionTimeout}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, "https://go.dev/dl/?mode=json", nil,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Go versions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var releases []goRelease
	if decodeErr := json.NewDecoder(resp.Body).Decode(&releases); decodeErr != nil {
		return "", fmt.Errorf("failed to parse Go versions: %w", decodeErr)
	}

	for _, release := range releases {
		if release.Stable {
			return strings.TrimPrefix(release.Version, "go"), nil
		}
	}

	return "", errors.New("no stable Go version found")
}

// parseGoDirective extracts the version from a go.mod's "go" directive.
// For example, given content containing "go 1.25.7", it returns "1.25.7".
func parseGoDirective(goModContent string) string {
	for _, line := range strings.Split(goModContent, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "go ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "go "))
		}
	}
	return ""
}

// --- clone + upgrade ---

func upgradeGoRepo(
	ctx context.Context,
	params upgradeParams,
) (*upgradeResult, error) {
	result := &upgradeResult{}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "autoupdate-go-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "repo")

	// Find the go binary
	goBinary, err := findGoBinary()
	if err != nil {
		return nil, fmt.Errorf("go binary not found: %w", err)
	}

	// Build and run the upgrade script
	script := buildUpgradeScript(params, repoDir, goBinary)
	scriptPath := filepath.Join(tmpDir, "upgrade.sh")

	if writeErr := os.WriteFile(scriptPath, []byte(script), scriptFileMode); writeErr != nil {
		return nil, fmt.Errorf("failed to write script: %w", writeErr)
	}

	cmd := exec.CommandContext(ctx, "bash", scriptPath)
	cmd.Dir = tmpDir
	cmd.Env = buildEnv(params, repoDir, goBinary)

	output, err := cmd.CombinedOutput()
	result.Output = string(output)

	if err != nil {
		return result, fmt.Errorf(
			"upgrade script failed: %w\nOutput:\n%s", err, result.Output,
		)
	}

	result.HasChanges = strings.Contains(result.Output, "CHANGES_PUSHED=true")
	result.GoVersionUpdated = strings.Contains(result.Output, "GO_VERSION_UPDATED=true")
	return result, nil
}

func buildUpgradeScript(
	params upgradeParams,
	repoDir, goBinary string,
) string {
	_ = repoDir  // used via env vars in the script
	_ = goBinary // used via env vars in the script

	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	// Set up git credentials based on provider
	sb.WriteString("# Set up isolated git config for auth\n")
	sb.WriteString("TEMP_GITCONFIG=$(mktemp)\n")
	sb.WriteString("cp ~/.gitconfig \"$TEMP_GITCONFIG\" 2>/dev/null || true\n")

	switch params.ProviderName {
	case "azuredevops":
		writeAzureDevOpsAuth(&sb)
	case "github":
		writeGitHubAuth(&sb)
	case "gitlab":
		writeGitLabAuth(&sb)
	}

	sb.WriteString("export GIT_CONFIG_GLOBAL=\"$TEMP_GITCONFIG\"\n")
	sb.WriteString("trap 'rm -f \"$TEMP_GITCONFIG\"' EXIT\n\n")

	// Ensure git user identity is configured for committing. Only set
	// defaults when the values are missing so that any user-provided
	// configuration (e.g. from ~/.gitconfig) is preserved.
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

	// Source config.sh if present
	if params.HasConfigSH {
		sb.WriteString("echo \"Running config.sh...\"\n")
		sb.WriteString("if [ -f \"./config.sh\" ]; then\n")
		sb.WriteString("    source ./config.sh\n")
		sb.WriteString("fi\n\n")
	}

	// Go upgrade commands
	writeGoUpgradeCommands(&sb)

	// Check for changes and commit/push
	writeCommitAndPush(&sb)

	return sb.String()
}

func writeAzureDevOpsAuth(sb *strings.Builder) {
	sb.WriteString("echo '[url \"https://pat:'\"${AUTH_TOKEN}\"'@dev.azure.com/\"]' >> \"$TEMP_GITCONFIG\"\n")
	sb.WriteString("echo '    insteadOf = https://dev.azure.com/' >> \"$TEMP_GITCONFIG\"\n")
	sb.WriteString("echo '[url \"https://pat:'\"${AUTH_TOKEN}\"'@dev.azure.com/\"]' >> \"$TEMP_GITCONFIG\"\n")
	sb.WriteString("echo '    insteadOf = git@ssh.dev.azure.com:v3/' >> \"$TEMP_GITCONFIG\"\n")
}

func writeGitHubAuth(sb *strings.Builder) {
	sb.WriteString("echo '[url \"https://x-access-token:'\"${AUTH_TOKEN}\"'@github.com/\"]' >> \"$TEMP_GITCONFIG\"\n")
	sb.WriteString("echo '    insteadOf = https://github.com/' >> \"$TEMP_GITCONFIG\"\n")
}

func writeGitLabAuth(sb *strings.Builder) {
	sb.WriteString("echo '[url \"https://oauth2:'\"${AUTH_TOKEN}\"'@gitlab.com/\"]' >> \"$TEMP_GITCONFIG\"\n")
	sb.WriteString("echo '    insteadOf = https://gitlab.com/' >> \"$TEMP_GITCONFIG\"\n")
}

func writeGoUpgradeCommands(sb *strings.Builder) {
	// Read the current go version from go.mod and compare with the target
	sb.WriteString("# Read current Go version from go.mod\n")
	sb.WriteString("CURRENT_GO_VERSION=$(grep -m1 '^go ' go.mod | awk '{print $2}')\n")
	sb.WriteString("echo \"Current Go version in go.mod: $CURRENT_GO_VERSION\"\n")
	sb.WriteString("GO_VERSION_CHANGED=false\n\n")

	// Only update the go directive if the versions differ.
	// Use sed + redirect-and-move instead of "go mod edit -go=" to preserve
	// the full three-part version (e.g. 1.25.7) regardless of the Go binary
	// version running the script — older Go binaries normalise three-part
	// versions to two-part (1.25.7 → 1.25) which is the root cause of the bug.
	// NOTE: we avoid "sed -i" because its syntax is incompatible between
	// GNU sed (-i'') and BSD/macOS sed (-i ''). The redirect-and-move
	// pattern works identically on all POSIX systems.
	sb.WriteString("if [ \"$CURRENT_GO_VERSION\" != \"$GO_VERSION\" ]; then\n")
	sb.WriteString("    echo \"Updating Go version from $CURRENT_GO_VERSION to $GO_VERSION...\"\n")
	sb.WriteString("    sed \"s/^go [0-9][0-9.]*$/go ${GO_VERSION}/\" go.mod > go.mod.tmp && mv go.mod.tmp go.mod\n")
	sb.WriteString("    GO_VERSION_CHANGED=true\n")
	sb.WriteString("    echo \"GO_VERSION_UPDATED=true\"\n")
	sb.WriteString("else\n")
	sb.WriteString("    echo \"Go version already at $GO_VERSION, skipping directive update\"\n")
	sb.WriteString("    echo \"GO_VERSION_UPDATED=false\"\n")
	sb.WriteString("fi\n\n")

	sb.WriteString("echo \"Running go get -u ./...\"\n")
	sb.WriteString(
		"\"$GO_BINARY\" get -u ./... 2>&1 || echo \"WARNING: go get -u had some errors (continuing anyway)\"\n\n",
	)

	sb.WriteString("echo \"Running go mod tidy...\"\n")
	sb.WriteString(
		"\"$GO_BINARY\" mod tidy 2>&1 || echo \"WARNING: go mod tidy had some errors (continuing anyway)\"\n\n",
	)

	// Re-apply the Go version after go mod tidy, because older Go binaries
	// may normalise the three-part version back to two-part during tidy.
	sb.WriteString("# Re-apply Go version if go mod tidy normalised it\n")
	sb.WriteString("if [ \"$GO_VERSION_CHANGED\" = \"true\" ]; then\n")
	sb.WriteString("    AFTER_TIDY_VERSION=$(grep -m1 '^go ' go.mod | awk '{print $2}')\n")
	sb.WriteString("    if [ \"$AFTER_TIDY_VERSION\" != \"$GO_VERSION\" ]; then\n")
	sb.WriteString("        echo \"Re-applying Go version (go mod tidy changed it to $AFTER_TIDY_VERSION)...\"\n")
	sb.WriteString(
		"        sed \"s/^go [0-9][0-9.]*$/go ${GO_VERSION}/\" go.mod > go.mod.tmp && mv go.mod.tmp go.mod\n",
	)
	sb.WriteString("    fi\n")
	sb.WriteString("fi\n\n")

	sb.WriteString("if [ -d \"vendor\" ]; then\n")
	sb.WriteString("    echo \"Running go mod vendor...\"\n")
	sb.WriteString("    \"$GO_BINARY\" mod vendor 2>&1 || echo \"WARNING: go mod vendor had some errors\"\n")
	sb.WriteString("fi\n\n")
}

func writeCommitAndPush(sb *strings.Builder) {
	sb.WriteString("if [ -n \"$(git status --porcelain)\" ]; then\n")
	sb.WriteString("    echo \"Changes detected, committing and pushing...\"\n")
	sb.WriteString("    git add -A\n")
	sb.WriteString("    if [ \"$GO_VERSION_CHANGED\" = \"true\" ]; then\n")
	sb.WriteString("        git commit -m \"chore(deps): upgrade Go to $GO_VERSION and update dependencies\"\n")
	sb.WriteString("    else\n")
	sb.WriteString("        git commit -m \"chore(deps): update Go module dependencies\"\n")
	sb.WriteString("    fi\n")
	sb.WriteString("    git push origin \"$BRANCH_NAME\" 2>&1\n")
	sb.WriteString("    echo \"CHANGES_PUSHED=true\"\n")
	sb.WriteString("else\n")
	sb.WriteString("    echo \"No changes detected.\"\n")
	sb.WriteString("    echo \"CHANGES_PUSHED=false\"\n")
	sb.WriteString("fi\n")
}

func buildEnv(params upgradeParams, repoDir, goBinary string) []string {
	return append(os.Environ(),
		"AUTH_TOKEN="+params.AuthToken,
		// Export the token under common aliases so that repository-specific
		// scripts (e.g. config.sh) can reference it by their expected name.
		"GIT_HTTPS_TOKEN="+params.AuthToken,
		"CLONE_URL="+params.CloneURL,
		"BRANCH_NAME="+params.BranchName,
		"GO_VERSION="+params.GoVersion,
		"REPO_DIR="+repoDir,
		"GO_BINARY="+goBinary,
		"DEFAULT_BRANCH="+params.DefaultBranch,
	)
}

func findGoBinary() (string, error) {
	if path, err := exec.LookPath("go"); err == nil {
		return path, nil
	}

	commonPaths := []string{
		"/usr/local/go/bin/go",
		"/usr/bin/go",
		"/snap/bin/go",
	}

	home, _ := os.UserHomeDir()
	if home != "" {
		if goBin, found := findGoBinaryInGVM(home); found {
			return goBin, nil
		}

		goenvBin := filepath.Join(home, ".goenv", "shims", "go")
		commonPaths = append(commonPaths, goenvBin)
	}

	for _, p := range commonPaths {
		if _, statErr := os.Stat(p); statErr == nil {
			return p, nil
		}
	}

	return "", errors.New("go binary not found in PATH or common locations")
}

func findGoBinaryInGVM(home string) (string, bool) {
	gvmDir := filepath.Join(home, ".gvm", "gos")

	entries, err := os.ReadDir(gvmDir)
	if err != nil {
		return "", false
	}

	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "go") {
			goBin := filepath.Join(gvmDir, entry.Name(), "bin", "go")
			if _, statErr := os.Stat(goBin); statErr == nil {
				return goBin, true
			}
		}
	}

	return "", false
}

func generateGoPRDescription(goVersion string, hasConfigSH, goVersionUpdated bool) string {
	var sb strings.Builder
	sb.WriteString("## Summary\n\n")
	if goVersionUpdated {
		sb.WriteString(
			"This PR upgrades the Go version to **" + goVersion + "** and updates all module dependencies.\n\n",
		)
	} else {
		sb.WriteString(
			"This PR updates all Go module dependencies (Go version is already at **" + goVersion + "**).\n\n",
		)
	}
	sb.WriteString("### Changes\n\n")
	if goVersionUpdated {
		sb.WriteString("- Updated `go.mod` Go directive to `" + goVersion + "`\n")
	}
	sb.WriteString("- Ran `go get -u ./...` to update all dependencies\n")
	sb.WriteString("- Ran `go mod tidy` to clean up\n")
	if hasConfigSH {
		sb.WriteString("- `config.sh` was sourced before running Go commands (private package settings)\n")
	}
	sb.WriteString("\n### Review Checklist\n\n")
	sb.WriteString("- [ ] Verify build passes\n")
	sb.WriteString("- [ ] Verify tests pass\n")
	sb.WriteString("- [ ] Review dependency changes in `go.sum`\n")
	sb.WriteString("\n---\n")
	sb.WriteString("*This PR was automatically created by [autoupdate](https://github.com/rios0rios0/autoupdate)*\n")
	return sb.String()
}
