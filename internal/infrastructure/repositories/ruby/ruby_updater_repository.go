package ruby

import (
	"context"
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
	langRuby "github.com/rios0rios0/langforge/pkg/infrastructure/languages/ruby"
)

const (
	updaterName      = "ruby"
	rbVersionTimeout = 15 * time.Second
	scriptFileMode   = 0o700

	// Branch name patterns for Ruby updates. One format is used when the
	// Ruby runtime version itself is being bumped; the other is used when
	// only gem dependencies are being refreshed.
	branchRbVersionFmt = "chore/upgrade-ruby-%s"
	branchRbDepsFmt    = "chore/upgrade-ruby-deps"

	// Commit/PR messages and changelog entries used across remote and local modes.
	rbCommitMsgDeps      = "chore(deps): updated Ruby gem dependencies"
	rbChangelogEntryDeps = "- changed the Ruby gem dependencies to their latest versions"
)

// UpdaterRepository implements repositories.UpdaterRepository for Ruby dependencies.
// It clones the repository locally, runs bundler commands to update
// dependencies, pushes the changes, and creates a PR via the provider API.
type UpdaterRepository struct {
	versionFetcher VersionFetcher
	cmdRunner      cmdrunner.Runner
}

// NewUpdaterRepository creates a new Ruby updater with default dependencies.
func NewUpdaterRepository() repositories.UpdaterRepository {
	return &UpdaterRepository{
		versionFetcher: NewHTTPRubyVersionFetcher(&http.Client{Timeout: rbVersionTimeout}),
		cmdRunner:      cmdrunner.NewDefaultRunner(),
	}
}

// NewUpdaterRepositoryWithDeps creates a Ruby updater with injected dependencies (for testing).
func NewUpdaterRepositoryWithDeps(vf VersionFetcher) repositories.UpdaterRepository {
	return &UpdaterRepository{versionFetcher: vf, cmdRunner: cmdrunner.NewDefaultRunner()}
}

func (u *UpdaterRepository) Name() string { return updaterName }

// Detect returns true if the repository has Ruby marker files (e.g. Gemfile, .ruby-version).
func (u *UpdaterRepository) Detect(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) bool {
	found, err := support.DetectRemote(ctx, &langRuby.Detector{}, provider, repo)
	if err != nil {
		logger.Warnf("[ruby] detection error for %s/%s: %v", repo.Organization, repo.Name, err)
		return false
	}
	return found
}

// CreateUpdatePRs clones the repo, upgrades Ruby dependencies,
// and creates a PR.
func (u *UpdaterRepository) CreateUpdatePRs(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
) ([]entities.PullRequest, error) {
	logger.Infof("[ruby] Processing %s/%s", repo.Organization, repo.Name)

	latestRbVersion, err := u.versionFetcher.FetchLatestVersion(ctx)
	if err != nil {
		logger.Warnf("[ruby] Failed to fetch latest Ruby version: %v (continuing without version upgrade)", err)
		latestRbVersion = ""
	} else {
		logger.Infof("[ruby] Latest stable Ruby version: %s", latestRbVersion)
	}

	vCtx := resolveVersionContext(ctx, provider, repo, latestRbVersion)

	// Check if PR already exists
	exists, prCheckErr := provider.PullRequestExists(ctx, repo, vCtx.BranchName)
	if prCheckErr != nil {
		logger.Warnf("[ruby] Failed to check existing PRs: %v", prCheckErr)
	}
	if exists {
		logger.Infof(
			"[ruby] PR already exists for branch %q, skipping",
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
		logger.Infof("[ruby] %s/%s: already up to date", repo.Organization, repo.Name)
		return []entities.PullRequest{}, nil
	}

	return openPullRequest(ctx, provider, repo, opts, vCtx, result)
}

// logDryRun logs what would happen without actually performing the upgrade.
func logDryRun(vCtx *versionContext, repo entities.Repository) {
	if vCtx.NeedsVersionUpgrade {
		logger.Infof(
			"[ruby] [DRY RUN] Would upgrade Ruby to %s and update deps for %s/%s",
			vCtx.LatestVersion, repo.Organization, repo.Name,
		)
	} else {
		logger.Infof(
			"[ruby] [DRY RUN] Would update Ruby gem dependencies for %s/%s",
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

	result, err := upgradeRepo(ctx, upgradeParams{
		CloneURL:      cloneURL,
		DefaultBranch: defaultBranch,
		BranchName:    vCtx.BranchName,
		RubyVersion:   vCtx.LatestVersion,
		AuthToken:     provider.AuthToken(),
		ProviderName:  provider.Name(),
		ChangelogFile: changelogFile,
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

	prTitle := rbCommitMsgDeps
	if result.RubyVersionUpdated {
		prTitle = fmt.Sprintf(
			"chore(deps): upgraded Ruby to `%s` and updated all gem dependencies",
			vCtx.LatestVersion,
		)
	}
	prDesc := GeneratePRDescription(vCtx.LatestVersion, result.RubyVersionUpdated)

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
		"[ruby] Created PR #%d for %s/%s: %s",
		pr.ID, repo.Organization, repo.Name, pr.URL,
	)
	return []entities.PullRequest{*pr}, nil
}

// ApplyUpdates implements repositories.LocalUpdater. It runs language-specific
// Ruby upgrade operations on a locally cloned repository, without performing
// any git clone, branch, commit, or push operations.
func (u *UpdaterRepository) ApplyUpdates(
	ctx context.Context,
	repoDir string,
	_ repositories.ProviderRepository,
	repo entities.Repository,
	_ entities.UpdateOptions,
) (*repositories.LocalUpdateResult, error) {
	logger.Infof("[ruby] Processing local clone of %s/%s", repo.Organization, repo.Name)

	// resolveLocalVersionContext (from local.go) handles fetching + comparison
	vCtx := resolveLocalVersionContext(ctx, repoDir)

	script := buildBatchRubyScript()
	scriptPath := filepath.Join(repoDir, ".autoupdate-upgrade.sh")
	if writeErr := os.WriteFile(scriptPath, []byte(script), scriptFileMode); writeErr != nil {
		return nil, fmt.Errorf("failed to write script: %w", writeErr)
	}
	defer func() { _ = os.Remove(scriptPath) }()

	cmd := exec.CommandContext(ctx, "bash", scriptPath)
	cmd.Dir = repoDir
	env := os.Environ()
	if vCtx.LatestVersion != "" {
		env = append(env, "TARGET_RUBY_VERSION="+vCtx.LatestVersion)
	}
	cmd.Env = env

	output, cmdErr := cmd.CombinedOutput()
	outputStr := string(output)
	logger.Debugf("[ruby] Upgrade script output:\n%s", outputStr)

	if cmdErr != nil {
		return nil, fmt.Errorf("upgrade script failed: %w\nOutput:\n%s", cmdErr, outputStr)
	}

	// Remove the script before checking worktree state so it does not
	// appear as an untracked file in the git status check below.
	_ = os.Remove(scriptPath)
	rbVersionUpdated := strings.Contains(outputStr, "RUBY_VERSION_UPDATED=true")

	// Return early if the upgrade script made no filesystem changes
	if !support.HasUncommittedChanges(ctx, repoDir) {
		logger.Infof("[ruby] No filesystem changes detected after upgrade script")
		return nil, repositories.ErrNoUpdatesNeeded
	}

	// Update CHANGELOG locally
	var entry string
	if rbVersionUpdated {
		entry = fmt.Sprintf(
			"- changed the Ruby version to `%s` and updated all gem dependencies",
			vCtx.LatestVersion,
		)
	} else {
		entry = rbChangelogEntryDeps
	}
	support.LocalChangelogUpdate(repoDir, []string{entry})

	commitMsg := rbCommitMsgDeps
	prTitle := commitMsg
	if rbVersionUpdated {
		commitMsg = fmt.Sprintf(
			"chore(deps): upgraded Ruby to `%s` and updated all gem dependencies",
			vCtx.LatestVersion,
		)
		prTitle = commitMsg
	}

	return &repositories.LocalUpdateResult{
		BranchName:    vCtx.BranchName,
		CommitMessage: commitMsg,
		PRTitle:       prTitle,
		PRDescription: GeneratePRDescription(vCtx.LatestVersion, rbVersionUpdated),
	}, nil
}

// buildBatchRubyScript generates a bash script with only language-specific
// operations (no git clone, branch, commit, or push) for the batch pipeline.
func buildBatchRubyScript() string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	writeRubyUpgradeCommands(&sb)
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
	RubyVersion   string
	AuthToken     string
	ProviderName  string
	ChangelogFile string
}

type upgradeResult struct {
	HasChanges         bool
	RubyVersionUpdated bool
	Output             string
}

// parseRubyVersionFile extracts the Ruby version from a .ruby-version
// file content. The file typically contains just a version string like "3.3.6".
func parseRubyVersionFile(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return ""
}

// --- version context ---

// resolveVersionContext reads the remote .ruby-version to find the current
// Ruby version and picks the right branch-name pattern.
func resolveVersionContext(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	latestRbVersion string,
) *versionContext {
	needsVersionUpgrade := false

	if latestRbVersion != "" && provider.HasFile(ctx, repo, ".ruby-version") {
		content, err := provider.GetFileContent(ctx, repo, ".ruby-version")
		if err == nil {
			currentVersion := parseRubyVersionFile(content)
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

// prepareChangelog reads the target repo's CHANGELOG.md (if it exists),
// inserts an entry describing the Ruby upgrade, and writes the modified
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
		logger.Warnf("[ruby] Failed to read CHANGELOG.md: %v", err)
		return ""
	}

	var entry string
	if vCtx.NeedsVersionUpgrade {
		entry = fmt.Sprintf(
			"- changed the Ruby version to `%s` and updated all gem dependencies",
			vCtx.LatestVersion,
		)
	} else {
		entry = rbChangelogEntryDeps
	}

	modified := entities.InsertChangelogEntry(content, []string{entry})
	if modified == content {
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

// --- clone + upgrade ---

func upgradeRepo(
	ctx context.Context,
	params upgradeParams,
) (*upgradeResult, error) {
	result := &upgradeResult{}

	tmpDir, err := os.MkdirTemp("", "autoupdate-ruby-*")
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
		redactedOutput := support.RedactTokens(result.Output, params.AuthToken)
		return result, fmt.Errorf(
			"upgrade script failed: %w\nOutput:\n%s", err, redactedOutput,
		)
	}

	result.HasChanges = strings.Contains(result.Output, "CHANGES_PUSHED=true")
	result.RubyVersionUpdated = strings.Contains(result.Output, "RUBY_VERSION_UPDATED=true")
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

	// Ruby upgrade commands
	writeRubyUpgradeCommands(&sb)

	// Update Dockerfile ruby image tags
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

func writeRubyUpgradeCommands(sb *strings.Builder) {
	// Update .ruby-version if it exists and a new version is available
	sb.WriteString("# Check and update Ruby version\n")
	sb.WriteString("RUBY_VERSION_CHANGED=false\n")
	sb.WriteString("if [ -n \"${TARGET_RUBY_VERSION:-}\" ] && [ -f \".ruby-version\" ]; then\n")
	sb.WriteString("    CURRENT_RB_VERSION=$(head -1 .ruby-version | tr -d '[:space:]')\n")
	sb.WriteString(
		"    if [ -n \"$CURRENT_RB_VERSION\" ] && [ \"$CURRENT_RB_VERSION\" != \"$TARGET_RUBY_VERSION\" ]; then\n",
	)
	sb.WriteString("        echo \"Updating .ruby-version from $CURRENT_RB_VERSION to $TARGET_RUBY_VERSION...\"\n")
	sb.WriteString("        echo \"$TARGET_RUBY_VERSION\" > .ruby-version\n")
	sb.WriteString("        RUBY_VERSION_CHANGED=true\n")
	sb.WriteString("        echo \"RUBY_VERSION_UPDATED=true\"\n")
	sb.WriteString("    else\n")
	sb.WriteString("        echo \"Ruby version already at $CURRENT_RB_VERSION, skipping version update\"\n")
	sb.WriteString("        echo \"RUBY_VERSION_UPDATED=false\"\n")
	sb.WriteString("    fi\n")
	sb.WriteString("else\n")
	sb.WriteString("    echo \"RUBY_VERSION_UPDATED=false\"\n")
	sb.WriteString("fi\n\n")

	// Update bundler and bundle update
	sb.WriteString("# Update bundler and gem dependencies\n")
	sb.WriteString("if [ -f \"Gemfile\" ]; then\n")
	sb.WriteString("    echo \"Updating bundler...\"\n")
	sb.WriteString("    gem update bundler 2>&1 || echo \"WARNING: gem update bundler had some errors\"\n\n")
	sb.WriteString("    echo \"Running bundle update...\"\n")
	sb.WriteString("    bundle update 2>&1 || echo \"WARNING: bundle update had some errors\"\n")
	sb.WriteString("fi\n\n")
}

func writeDockerfileUpdate(sb *strings.Builder) {
	sb.WriteString("# Update Dockerfile ruby image tags when the Ruby version was bumped.\n")
	sb.WriteString("if [ \"$RUBY_VERSION_CHANGED\" = \"true\" ]; then\n")
	sb.WriteString("    echo \"Updating Dockerfile ruby image tags to $TARGET_RUBY_VERSION...\"\n")
	sb.WriteString(
		"    find . -type f -not -path './.git/*' " +
			"\\( -name 'Dockerfile' -o -name 'Dockerfile.*' -o -name '*.Dockerfile' \\) " +
			"-print0 | while IFS= read -r -d '' df; do\n",
	)
	sb.WriteString("        if grep -q 'ruby:[0-9]' \"$df\"; then\n")
	sb.WriteString(
		"            sed \"s|ruby:[0-9][0-9.]*|ruby:${TARGET_RUBY_VERSION}|g\" \"$df\" > \"$df.tmp\" && mv \"$df.tmp\" \"$df\"\n",
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
	sb.WriteString("    if [ \"$RUBY_VERSION_CHANGED\" = \"true\" ]; then\n")
	sb.WriteString(
		"        git commit -m \"chore(deps): upgraded Ruby to `$TARGET_RUBY_VERSION` and updated all gem dependencies\"\n",
	)
	sb.WriteString("    else\n")
	sb.WriteString("        git commit -m \"chore(deps): updated Ruby gem dependencies\"\n")
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
	)
	if params.RubyVersion != "" {
		env = append(env, "TARGET_RUBY_VERSION="+params.RubyVersion)
	}
	if params.ChangelogFile != "" {
		env = append(env, "CHANGELOG_FILE="+params.ChangelogFile)
	}
	return env
}

// GeneratePRDescription builds a markdown PR description for a Ruby
// dependency upgrade. Exported so that the local-mode CLI handler can
// reuse the same description format.
func GeneratePRDescription(rbVersion string, rbVersionUpdated bool) string {
	var sb strings.Builder
	sb.WriteString("## Summary\n\n")
	if rbVersionUpdated {
		sb.WriteString(
			"This PR upgrades the Ruby version to **" + rbVersion + "** and updates all gem dependencies.\n\n",
		)
	} else {
		sb.WriteString(
			"This PR updates all Ruby gem dependencies to their latest versions.\n\n",
		)
	}
	sb.WriteString("### Changes\n\n")
	if rbVersionUpdated {
		sb.WriteString("- Updated `.ruby-version` to `" + rbVersion + "`\n")
	}
	sb.WriteString("- Ran `gem update bundler` to ensure bundler is current\n")
	sb.WriteString("- Ran `bundle update` to update all gem dependencies\n")
	sb.WriteString("\n### Review Checklist\n\n")
	sb.WriteString("- [ ] Verify build passes\n")
	sb.WriteString("- [ ] Verify tests pass\n")
	sb.WriteString("- [ ] Review dependency changes in `Gemfile.lock`\n")
	sb.WriteString("\n---\n")
	sb.WriteString("*This PR was automatically created by [autoupdate](https://github.com/rios0rios0/autoupdate)*\n")
	return sb.String()
}
