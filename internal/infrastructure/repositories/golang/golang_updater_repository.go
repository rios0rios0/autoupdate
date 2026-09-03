package golang

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
	"github.com/rios0rios0/autoupdate/internal/support"
	langGolang "github.com/rios0rios0/langforge/pkg/infrastructure/languages/golang"
)

const (
	updaterName       = "golang"
	goVersionTimeout  = 15 * time.Second
	scriptFileMode    = 0o700
	goDirectiveFields = 2 // expected number of fields in "go <version>"

	// Branch name patterns for Go updates. One format is used when the Go
	// version (go directive) itself is being bumped; the other is used when
	// the go directive is already at the desired version and only module
	// dependencies are being refreshed.
	branchGoVersionFmt = "chore/upgrade-go-%s"
	branchGoDepsFmt    = "chore/upgrade-go-deps"

	// Commit/PR messages and changelog entries used across remote and local modes.
	goCommitMsgDeps      = "chore(deps): update Go module dependencies"
	goChangelogEntryDeps = "- changed the Go module dependencies to their latest versions"

	// Git provider names for auth setup.
	providerAzureDevOps = "azuredevops"
	providerGitHub      = "github"
	providerGitLab      = "gitlab"
)

// defaultRunner is the package-level command runner for remote-mode functions.
var defaultRunner cmdrunner.Runner = cmdrunner.NewDefaultRunner() //nolint:gochecknoglobals // test override

// UpdaterRepository implements repositories.UpdaterRepository for Go module dependencies.
// It clones the repository locally, runs go commands to update
// dependencies, pushes the changes, and creates a PR via the provider API.
type UpdaterRepository struct {
	versionFetcher VersionFetcher
	cmdRunner      cmdrunner.Runner
}

// NewUpdaterRepository creates a new Go updater with default dependencies.
func NewUpdaterRepository() repositories.UpdaterRepository {
	return &UpdaterRepository{
		versionFetcher: NewHTTPGoVersionFetcher(&http.Client{Timeout: goVersionTimeout}),
		cmdRunner:      cmdrunner.NewDefaultRunner(),
	}
}

// NewUpdaterRepositoryWithDeps creates a Go updater with injected dependencies (for testing).
func NewUpdaterRepositoryWithDeps(vf VersionFetcher) repositories.UpdaterRepository {
	return &UpdaterRepository{versionFetcher: vf, cmdRunner: cmdrunner.NewDefaultRunner()}
}

func (u *UpdaterRepository) Name() string { return updaterName }

// Detect returns true if the repository has Go marker files (e.g. go.mod).
// The langforge detector only inspects the repository root, so a repository
// that keeps its module in a subdirectory — such as a Terraform or
// infrastructure repository with a Go test harness nested under it — is
// detected through a full listing of go.mod files instead.
func (u *UpdaterRepository) Detect(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) bool {
	found, err := support.DetectRemote(ctx, &langGolang.Detector{}, provider, repo)
	if err != nil {
		logger.Warnf("[golang] detection error for %s/%s: %v", repo.Organization, repo.Name, err)
	}
	if found {
		return true
	}

	moduleDirs := discoverRemoteModuleDirs(ctx, provider, repo)
	if len(moduleDirs) == 0 {
		return false
	}

	logger.Infof(
		"[golang] %s/%s has no root go.mod but %d nested module(s): %s",
		repo.Organization, repo.Name, len(moduleDirs), strings.Join(moduleDirs, ", "),
	)
	return true
}

// CreateUpdatePRs clones the repo, upgrades Go version and
// dependencies, and creates a PR.
func (u *UpdaterRepository) CreateUpdatePRs(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
) ([]entities.PullRequest, error) {
	logger.Infof("[golang] Processing %s/%s", repo.Organization, repo.Name)

	latestGoVersion, err := u.versionFetcher.FetchLatestVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch latest Go version: %w", err)
	}
	logger.Infof("[golang] Latest stable Go version: %s", latestGoVersion)

	vCtx := resolveVersionContext(ctx, provider, repo, latestGoVersion)

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
		return []entities.PullRequest{}, nil
	}

	if opts.DryRun {
		if vCtx.NeedsVersionUpgrade {
			logger.Infof(
				"[golang] [DRY RUN] Would upgrade Go to %s and update deps for %s/%s",
				vCtx.LatestVersion, repo.Organization, repo.Name,
			)
		} else {
			logger.Infof(
				"[golang] [DRY RUN] Would update Go module deps for %s/%s (already at Go %s)",
				repo.Organization, repo.Name, vCtx.LatestVersion,
			)
		}
		return []entities.PullRequest{}, nil
	}

	result, hasConfigSH, upgradeErr := cloneAndUpgrade(ctx, provider, repo, vCtx)
	if upgradeErr != nil {
		return nil, upgradeErr
	}

	if !result.HasChanges {
		logger.Infof("[golang] %s/%s: already up to date", repo.Organization, repo.Name)
		return []entities.PullRequest{}, nil
	}

	return openPullRequest(ctx, provider, repo, opts, vCtx, result, hasConfigSH)
}

// ApplyUpdates implements repositories.LocalUpdater for the clone-based pipeline.
// It runs Go upgrade commands on the already-cloned repository, updates
// Dockerfiles and CHANGELOG, and returns PR metadata.
func (u *UpdaterRepository) ApplyUpdates(
	ctx context.Context,
	repoDir string,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	_ entities.UpdateOptions,
) (*repositories.LocalUpdateResult, error) {
	logger.Infof("[golang] Processing local clone of %s/%s", repo.Organization, repo.Name)

	latestGoVersion, err := u.versionFetcher.FetchLatestVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch latest Go version: %w", err)
	}
	logger.Infof("[golang] Latest stable Go version: %s", latestGoVersion)

	vCtx := localResolveVersionContext(repoDir, latestGoVersion)

	hasConfigSH := fileExistsLocally(filepath.Join(repoDir, "config.sh"))

	goBinary, goErr := findGoBinary()
	if goErr != nil {
		return nil, fmt.Errorf("go binary not found: %w", goErr)
	}

	script := buildLocalGoScript(provider.Name(), hasConfigSH)
	scriptPath := filepath.Join(repoDir, ".autoupdate-upgrade.sh")
	if writeErr := os.WriteFile(scriptPath, []byte(script), scriptFileMode); writeErr != nil {
		return nil, fmt.Errorf("failed to write script: %w", writeErr)
	}
	defer func() { _ = os.Remove(scriptPath) }()

	runResult, cmdErr := u.cmdRunner.Run(ctx, "bash", []string{scriptPath}, cmdrunner.RunOptions{
		Dir: repoDir,
		Env: append(os.Environ(),
			"AUTH_TOKEN="+provider.AuthToken(),
			"GIT_HTTPS_TOKEN="+provider.AuthToken(),
			"GO_VERSION="+vCtx.LatestVersion,
			"GO_BINARY="+goBinary,
		),
	})
	outputStr := ""
	if runResult != nil {
		outputStr = support.RedactTokens(runResult.Output, provider.AuthToken())
	}
	logger.Debugf("[golang] Upgrade script output:\n%s", outputStr)

	// Remove the script before checking worktree state so it does not
	// appear as an untracked file in the git status check below.
	_ = os.Remove(scriptPath)
	if cmdErr != nil {
		return nil, fmt.Errorf("upgrade script failed: %w\nOutput:\n%s", cmdErr, outputStr)
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

	// Return early if the upgrade script made no filesystem changes
	if !support.HasUncommittedChanges(ctx, repoDir) {
		logger.Infof("[golang] No filesystem changes detected after upgrade script")
		return nil, repositories.ErrNoUpdatesNeeded
	}
	logger.Infof("[golang] Filesystem changes detected, proceeding with commit")

	// Record the upgrade in the repository's changelog.
	var entry string
	if goVersionUpdated {
		entry = fmt.Sprintf(
			"- changed the Go version to `%s` and updated all module dependencies",
			vCtx.LatestVersion,
		)
	} else {
		entry = goChangelogEntryDeps
	}
	support.LocalChangelogUpdate(repoDir, []string{entry})

	commitMsg := goCommitMsgDeps
	prTitle := commitMsg
	if goVersionUpdated {
		commitMsg = fmt.Sprintf(
			"chore(deps): upgraded Go version to `%s` and updated all dependencies",
			vCtx.LatestVersion,
		)
		prTitle = commitMsg
	}

	return &repositories.LocalUpdateResult{
		BranchName:    vCtx.BranchName,
		CommitMessage: commitMsg,
		PRTitle:       prTitle,
		PRDescription: GenerateGoPRDescription(vCtx.LatestVersion, hasConfigSH, goVersionUpdated),
	}, nil
}

// localResolveVersionContext reads the local go.mod to determine the version
// context instead of using the provider API.
func localResolveVersionContext(repoDir, latestGoVersion string) *versionContext {
	needsVersionUpgrade := true
	needsUpgrade, sourceDir, found := resolveVersionUpgradeNeed(
		localGoModReader(repoDir),
		func() []string { return discoverLocalModuleDirs(repoDir) },
		latestGoVersion,
	)
	if !found {
		logger.Warnf("[golang] Could not read any local go.mod, assuming version upgrade")
	} else {
		needsVersionUpgrade = needsUpgrade
		logger.Infof(
			"[golang] Go version upgrade needed: %v (decided by %s)",
			needsVersionUpgrade, goModPathFor(sourceDir),
		)
	}

	branchName := branchGoDepsFmt
	if needsVersionUpgrade {
		branchName = fmt.Sprintf(branchGoVersionFmt, latestGoVersion)
	}

	return &versionContext{
		LatestVersion:       latestGoVersion,
		NeedsVersionUpgrade: needsVersionUpgrade,
		BranchName:          branchName,
	}
}

// buildLocalGoScript generates a bash script with only language-specific
// operations (no git clone, branch, commit, or push).
func buildLocalGoScript(providerName string, hasConfigSH bool) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	// Set up git credentials for `go get` with private modules
	sb.WriteString("# Set up isolated git config for auth (needed for private modules)\n")
	sb.WriteString("TEMP_GITCONFIG=$(mktemp)\n")
	sb.WriteString("cp ~/.gitconfig \"$TEMP_GITCONFIG\" 2>/dev/null || true\n")

	switch providerName {
	case providerAzureDevOps:
		writeAzureDevOpsAuth(&sb)
	case providerGitHub:
		writeGitHubAuth(&sb)
	case providerGitLab:
		writeGitLabAuth(&sb)
	}

	sb.WriteString("export GIT_CONFIG_GLOBAL=\"$TEMP_GITCONFIG\"\n")
	sb.WriteString("trap 'rm -f \"$TEMP_GITCONFIG\"' EXIT\n\n")

	if hasConfigSH {
		sb.WriteString("echo \"Running config.sh...\"\n")
		sb.WriteString("if [ -f \"./config.sh\" ]; then\n")
		sb.WriteString("    source ./config.sh\n")
		sb.WriteString("fi\n\n")
	}

	writeGoUpgradeCommands(&sb)
	// Dockerfile golang image tags are rewritten in Go (registry-verified) by
	// updateDockerfileGolangTags after this script runs — see ApplyUpdates.

	return sb.String()
}

// fileExistsLocally returns true if the given path exists on the local filesystem.
func fileExistsLocally(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// cloneAndUpgrade prepares the changelog, clones the repository, runs the
// upgrade script, and returns the result.
func cloneAndUpgrade(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	vCtx *versionContext,
) (*upgradeResult, bool, error) {
	hasConfigSH := provider.HasFile(ctx, repo, "config.sh")
	changelog := support.StageRemoteChangelog(ctx, provider, repo, changelogEntries(vCtx))
	defer changelog.Remove()

	cloneURL := provider.CloneURL(repo)
	defaultBranch := strings.TrimPrefix(repo.DefaultBranch, "refs/heads/")

	result, err := upgradeGoRepo(ctx, upgradeParams{
		CloneURL:      cloneURL,
		DefaultBranch: defaultBranch,
		BranchName:    vCtx.BranchName,
		GoVersion:     vCtx.LatestVersion,
		AuthToken:     provider.AuthToken(),
		HasConfigSH:   hasConfigSH,
		ProviderName:  provider.Name(),
		Changelog:     changelog,
	})
	if err != nil {
		return nil, false, fmt.Errorf("failed to upgrade: %w", err)
	}

	return result, hasConfigSH, nil
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
	hasConfigSH bool,
) ([]entities.PullRequest, error) {
	targetBranch := repo.DefaultBranch
	if opts.TargetBranch != "" {
		targetBranch = "refs/heads/" + opts.TargetBranch
	}

	prTitle := goCommitMsgDeps
	if result.GoVersionUpdated {
		prTitle = fmt.Sprintf(
			"chore(deps): upgraded Go version to `%s` and updated all dependencies",
			vCtx.LatestVersion,
		)
	}
	prDesc := GenerateGoPRDescription(vCtx.LatestVersion, hasConfigSH, result.GoVersionUpdated)

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
		"[golang] Created PR #%d for %s/%s: %s",
		pr.ID, repo.Organization, repo.Name, pr.URL,
	)
	return []entities.PullRequest{*pr}, nil
}

// versionContext holds the pre-resolved Go version information and the
// branch name derived from it.  Extracted from CreateUpdatePRs to keep
// that method within the project's funlen limit.
type versionContext struct {
	LatestVersion       string
	NeedsVersionUpgrade bool
	BranchName          string
}

// resolveVersionContext reads the remote go.mod to find the current go
// directive and picks the right branch-name pattern (version-upgrade vs
// deps-only).  The latest Go version must be provided by the caller so
// that this function stays free of HTTP calls and is fully testable with
// provider test doubles.
func resolveVersionContext(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	latestGoVersion string,
) *versionContext {
	// Read the current go.mod from the remote to decide whether this is a
	// version upgrade or a deps-only refresh — before cloning. When the root
	// holds no go.mod the first nested module answers the same question.
	needsVersionUpgrade := true // safe default when go.mod cannot be read
	needsUpgrade, sourceDir, found := resolveVersionUpgradeNeed(
		func(dir string) (string, error) {
			return provider.GetFileContent(ctx, repo, goModPathFor(dir))
		},
		func() []string { return discoverRemoteModuleDirs(ctx, provider, repo) },
		latestGoVersion,
	)
	if !found {
		logger.Warnf("[golang] Could not read any remote go.mod, assuming version upgrade")
	} else {
		needsVersionUpgrade = needsUpgrade
		logger.Infof(
			"[golang] Go version upgrade needed: %v (decided by %s)",
			needsVersionUpgrade, goModPathFor(sourceDir),
		)
	}

	// Choose the branch name pattern based on the kind of change, following
	// the same dual-branch idea used by the Terraform updater.
	branchName := branchGoDepsFmt
	if needsVersionUpgrade {
		branchName = fmt.Sprintf(branchGoVersionFmt, latestGoVersion)
	}

	return &versionContext{
		LatestVersion:       latestGoVersion,
		NeedsVersionUpgrade: needsVersionUpgrade,
		BranchName:          branchName,
	}
}

// changelogEntries renders the Keep a Changelog bullet describing the upgrade.
// The staging helpers turn it into a chlog fragment when the target repository
// uses that format instead.
func changelogEntries(vCtx *versionContext) []string {
	if vCtx.NeedsVersionUpgrade {
		return []string{fmt.Sprintf(
			"- changed the Go version to `%s` and updated all module dependencies",
			vCtx.LatestVersion,
		)}
	}
	return []string{goChangelogEntryDeps}
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
	// Changelog is the staged changelog payload the script copies into the
	// clone; an empty value leaves the repository's changelog untouched.
	Changelog support.StagedChangelog
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

// parseGoDirective extracts the version from a go.mod's "go" directive.
// For example, given content containing "go 1.25.7", it returns "1.25.7".
func parseGoDirective(goModContent string) string {
	for line := range strings.SplitSeq(goModContent, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "go ") {
			fields := strings.Fields(line)
			if len(fields) >= goDirectiveFields {
				return fields[1]
			}
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

	runResult, runErr := defaultRunner.Run(ctx, "bash", []string{scriptPath}, cmdrunner.RunOptions{
		Dir: tmpDir,
		Env: buildEnv(params, repoDir, goBinary),
	})
	if runResult != nil {
		result.Output = runResult.Output
	}

	if runErr != nil {
		return result, fmt.Errorf(
			"upgrade script failed: %w\nOutput:\n%s", runErr, result.Output,
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
	case providerAzureDevOps:
		writeAzureDevOpsAuth(&sb)
	case providerGitHub:
		writeGitHubAuth(&sb)
	case providerGitLab:
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

	// NOTE: Dockerfile golang image tags are no longer rewritten from bash.
	// This monolithic clone-and-push flow is legacy (the golang updater
	// implements LocalUpdater, so ApplyUpdates handles live runs and performs
	// the registry-verified Dockerfile rewrite in Go).

	// Copy in the staged changelog: an edited CHANGELOG.md, or a chlog
	// fragment when the target repository uses that format.
	sb.WriteString(support.ChangelogUpdateScript())

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

// writeGoUpgradeCommands emits the Go upgrade commands for every module in
// the repository. A repository may declare more than one module — for example
// an infrastructure repository whose integration-test harness lives in its own
// module under a subdirectory — and `go get ./...` never crosses a module
// boundary, so each module has to be upgraded in its own directory.
func writeGoUpgradeCommands(sb *strings.Builder) {
	// Defined once, outside the per-module loop that uses them.
	sb.WriteString(support.VersionGuardScript())
	sb.WriteString(support.GoMajorGuardScript())

	writeGoModuleDiscovery(sb)

	sb.WriteString("GO_VERSION_CHANGED=false\n\n")

	// The module list is fed through a heredoc rather than a pipe so the loop
	// body runs in the current shell and GO_VERSION_CHANGED survives it.
	sb.WriteString("while IFS= read -r MODULE_DIR; do\n")
	sb.WriteString("    [ -n \"$MODULE_DIR\" ] || continue\n")
	sb.WriteString("    echo \"=== Upgrading Go module in ${MODULE_DIR} ===\"\n")
	// "./" guards against a directory whose name starts with "-", which bash
	// would otherwise parse as pushd's stack-index option.
	sb.WriteString("    pushd \"./$MODULE_DIR\" > /dev/null\n\n")

	writeGoModuleUpgradeCommands(sb)

	sb.WriteString("    popd > /dev/null\n")
	// The delimiter is deliberately unquoted so that GO_MODULE_DIRS expands.
	sb.WriteString("done <<GO_MODULE_LIST_END\n")
	sb.WriteString("${GO_MODULE_DIRS}\n")
	sb.WriteString("GO_MODULE_LIST_END\n\n")
}

// writeGoModuleDiscovery emits the discovery of every go.mod in the checkout,
// skipping vendored trees, test fixtures and hidden directories.
func writeGoModuleDiscovery(sb *strings.Builder) {
	sb.WriteString("# Discover every Go module in the repository (root and nested).\n")
	sb.WriteString("# Unreadable subtrees make find exit non-zero; under `set -e` that would\n")
	sb.WriteString("# abort the whole upgrade, so the pipeline result is deliberately ignored\n")
	sb.WriteString("# and the modules that were found are still upgraded.\n")
	sb.WriteString("GO_MODULE_DIRS=$(find . -type f -name go.mod \\\n")
	sb.WriteString("    -not -path '*/vendor/*' \\\n")
	sb.WriteString("    -not -path '*/testdata/*' \\\n")
	sb.WriteString("    -not -path '*/node_modules/*' \\\n")
	sb.WriteString("    -not -path '*/.*/*' \\\n")
	sb.WriteString("    -exec dirname {} \\; 2>/dev/null | sed 's|^\\./||' | sort -u || true)\n")
	sb.WriteString("if [ -z \"$GO_MODULE_DIRS\" ]; then\n")
	sb.WriteString("    echo \"WARNING: no go.mod found in the repository, nothing to upgrade\"\n")
	sb.WriteString("    echo \"GO_VERSION_UPDATED=false\"\n")
	sb.WriteString("    exit 0\n")
	sb.WriteString("fi\n")
	sb.WriteString("echo \"Go modules to upgrade:\"\n")
	sb.WriteString("echo \"$GO_MODULE_DIRS\" | sed 's/^/  - /'\n\n")
}

// writeGoModuleUpgradeCommands emits the upgrade of a single module. It runs
// with the working directory already set to that module's directory, so every
// path stays module-relative.
func writeGoModuleUpgradeCommands(sb *strings.Builder) {
	// Read the current go version from go.mod and compare with the target.
	// "|| true" is required: a go.mod without a go directive makes grep exit 1,
	// which under `set -o pipefail` would abort the script and leave the
	// modules already rewritten in this run only half-upgraded. It also keeps
	// the "no directive" branch below reachable.
	sb.WriteString("    # Read current Go version from go.mod\n")
	sb.WriteString("    CURRENT_GO_VERSION=$(grep -m1 '^go ' go.mod | awk '{print $2}' || true)\n")
	sb.WriteString("    echo \"Current Go version in go.mod: ${CURRENT_GO_VERSION:-<not found>}\"\n")

	// Only update the go directive if the versions differ.
	// Use sed + redirect-and-move instead of "go mod edit -go=" to preserve
	// the full three-part version (e.g. 1.25.7) regardless of the Go binary
	// version running the script — older Go binaries normalise three-part
	// versions to two-part (1.25.7 → 1.25) which is the root cause of the bug.
	// NOTE: we avoid "sed -i" because its syntax is incompatible between
	// GNU sed (-i'') and BSD/macOS sed (-i ''). The redirect-and-move
	// pattern works identically on all POSIX systems.
	//
	// Edge cases handled:
	//   • Missing go directive — warn and let "go mod tidy" insert it later.
	//   • sed no-op (pattern didn't match) — verify the file was actually
	//     modified before setting GO_VERSION_CHANGED.
	sb.WriteString("    if [ -z \"$CURRENT_GO_VERSION\" ]; then\n")
	sb.WriteString("        echo \"WARNING: no go directive found in go.mod, skipping version update\"\n")
	sb.WriteString("        echo \"GO_VERSION_UPDATED=false\"\n")
	sb.WriteString("    elif autoupdate_version_is_newer \"$GO_VERSION\" \"$CURRENT_GO_VERSION\"; then\n")
	sb.WriteString("        echo \"Updating Go version from $CURRENT_GO_VERSION to $GO_VERSION...\"\n")
	sb.WriteString(
		"        sed \"s/^go [0-9][0-9.]*$/go ${GO_VERSION}/\" go.mod > go.mod.tmp && mv go.mod.tmp go.mod\n",
	)
	sb.WriteString("        # Verify the substitution actually took effect\n")
	sb.WriteString("        UPDATED_VERSION=$(grep -m1 '^go ' go.mod | awk '{print $2}' || true)\n")
	sb.WriteString("        if [ \"$UPDATED_VERSION\" = \"$GO_VERSION\" ]; then\n")
	sb.WriteString("            GO_VERSION_CHANGED=true\n")
	sb.WriteString("            echo \"GO_VERSION_UPDATED=true\"\n")
	sb.WriteString("        else\n")
	sb.WriteString("            echo \"WARNING: failed to update go directive (sed pattern did not match)\"\n")
	sb.WriteString("            echo \"GO_VERSION_UPDATED=false\"\n")
	sb.WriteString("        fi\n")
	sb.WriteString("    else\n")
	sb.WriteString(
		"        echo \"Keeping the go directive at $CURRENT_GO_VERSION (not older than $GO_VERSION)\"\n",
	)
	sb.WriteString("        echo \"GO_VERSION_UPDATED=false\"\n")
	sb.WriteString("    fi\n\n")

	// Snapshot the requirements before the upgrade so the major-version guard
	// below has something to compare against. See GoMajorGuardScript: `-u` will
	// not reach for a `/v2` path, but v0 and v1 share the unsuffixed one, so
	// the boundary Go itself calls compatibility-free is the one it crosses.
	sb.WriteString("    MODULE_VERSIONS_BEFORE=\"$(mktemp)\"\n")
	sb.WriteString(
		"    autoupdate_go_module_versions go.mod > \"$MODULE_VERSIONS_BEFORE\"\n\n",
	)

	sb.WriteString("    echo \"Running go get -u -t ./...\"\n")
	sb.WriteString(
		"    \"$GO_BINARY\" get -u -t ./... 2>&1 || " +
			"echo \"WARNING: go get -u -t had some errors (continuing anyway)\"\n\n",
	)

	sb.WriteString("    echo \"Running go mod tidy...\"\n")
	sb.WriteString(
		"    \"$GO_BINARY\" mod tidy 2>&1 || " +
			"echo \"WARNING: go mod tidy had some errors (continuing anyway)\"\n\n",
	)

	// After tidy, not before: tidy resolves the graph and can settle a
	// requirement on a different version than `go get` first wrote, so
	// comparing any earlier would read a version that is not the one shipping.
	// The guard returns non-zero when it could not resolve something, and the
	// script runs under `set -e`, so the call is branched rather than bare: a
	// guard that found a problem must report it, not abort the run before the
	// safe part of the upgrade is committed.
	sb.WriteString("    echo \"Checking for major version jumps...\"\n")
	sb.WriteString("    if ! autoupdate_go_hold_major_jumps " +
		"\"$GO_BINARY\" \"$MODULE_VERSIONS_BEFORE\" go.mod; then\n")
	sb.WriteString(
		"        echo \"  WARNING: some major version jumps are unresolved (see above); " +
			"review this module before merging\"\n",
	)
	sb.WriteString("    fi\n")
	sb.WriteString("    rm -f \"$MODULE_VERSIONS_BEFORE\"\n\n")

	// Re-apply the Go version after go mod tidy, because older Go binaries
	// may normalise the three-part version back to two-part during tidy.
	sb.WriteString("    # Re-apply Go version if go mod tidy normalised it\n")
	sb.WriteString("    AFTER_TIDY_VERSION=$(grep -m1 '^go ' go.mod | awk '{print $2}' || true)\n")
	sb.WriteString(
		"    if autoupdate_version_is_newer \"$GO_VERSION\" \"$AFTER_TIDY_VERSION\"; then\n",
	)
	sb.WriteString("        echo \"Re-applying Go version (go mod tidy changed it to $AFTER_TIDY_VERSION)...\"\n")
	sb.WriteString(
		"        sed \"s/^go [0-9][0-9.]*$/go ${GO_VERSION}/\" go.mod > go.mod.tmp && mv go.mod.tmp go.mod\n",
	)
	sb.WriteString("    fi\n\n")

	sb.WriteString("    if [ -d \"vendor\" ]; then\n")
	sb.WriteString("        echo \"Running go mod vendor...\"\n")
	sb.WriteString("        \"$GO_BINARY\" mod vendor 2>&1 || echo \"WARNING: go mod vendor had some errors\"\n")
	sb.WriteString("    fi\n\n")
}

func writeCommitAndPush(sb *strings.Builder) {
	sb.WriteString("if [ -n \"$(git status --porcelain)\" ]; then\n")
	sb.WriteString("    echo \"Changes detected, committing and pushing...\"\n")
	sb.WriteString("    git add -A\n")
	sb.WriteString("    if [ \"$GO_VERSION_CHANGED\" = \"true\" ]; then\n")
	// The backticks must be escaped: unescaped, bash treats them as command
	// substitution and the version silently vanishes from the commit subject.
	sb.WriteString(
		"        git commit -m \"chore(deps): upgraded Go version to \\`$GO_VERSION\\` " +
			"and updated all dependencies\"\n",
	)
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
	env := append(os.Environ(),
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
	env = append(env, params.Changelog.Env()...)
	return env
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

	for _, entry := range slices.Backward(entries) {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "go") {
			goBin := filepath.Join(gvmDir, entry.Name(), "bin", "go")
			if _, statErr := os.Stat(goBin); statErr == nil {
				return goBin, true
			}
		}
	}

	return "", false
}

// GenerateGoPRDescription builds a markdown PR description for a Go
// dependency upgrade.  Exported so that the local-mode CLI handler can
// reuse the same description format.
func GenerateGoPRDescription(goVersion string, hasConfigSH, goVersionUpdated bool) string {
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
		sb.WriteString("- Updated the `go` directive to `" + goVersion + "` in every `go.mod`\n")
	}
	sb.WriteString("- Ran `go get -u -t ./...` in each module directory to update all dependencies\n")
	sb.WriteString(
		"- Checked every dependency whose major version moved and held it at its " +
			"previous version, because v0 and v1 share an unsuffixed module path and " +
			"`-u` crosses the one boundary Go makes no compatibility promise across. " +
			"A hold can fail when an upgraded dependency requires the new major; the " +
			"run log names anything left unresolved, so check it before merging\n",
	)
	sb.WriteString("- Ran `go mod tidy` in each module directory to clean up\n")
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
