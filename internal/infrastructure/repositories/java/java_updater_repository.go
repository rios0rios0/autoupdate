package java

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
	langJavaGradle "github.com/rios0rios0/langforge/pkg/infrastructure/languages/javagradle"
	langJavaMaven "github.com/rios0rios0/langforge/pkg/infrastructure/languages/javamaven"
)

const (
	updaterName        = "java"
	javaVersionTimeout = 15 * time.Second
	scriptFileMode     = 0o700

	// Build system identifiers.
	buildSystemGradle = "gradle"
	buildSystemMaven  = "maven"

	// Branch name patterns for Java updates. One format is used when the
	// Java runtime version itself is being bumped; the other is used when
	// only dependencies are being refreshed.
	branchJavaVersionFmt = "chore/upgrade-java-%s"
	branchJavaDepsFmt    = "chore/upgrade-java-deps"

	// Commit/PR messages and changelog entries used across remote and local modes.
	javaCommitMsgDeps      = "chore(deps): updated Java dependencies"
	javaChangelogEntryDeps = "- changed the Java dependencies to their latest versions"
)

// UpdaterRepository implements repositories.UpdaterRepository for Java dependencies.
// It supports both Gradle and Maven build systems. It clones the repository
// locally, runs the appropriate build tool commands to update dependencies,
// pushes the changes, and creates a PR via the provider API.
type UpdaterRepository struct {
	versionFetcher VersionFetcher
	cmdRunner      cmdrunner.Runner
}

// NewUpdaterRepository creates a new Java updater with default dependencies.
func NewUpdaterRepository() repositories.UpdaterRepository {
	return &UpdaterRepository{
		versionFetcher: NewHTTPJavaVersionFetcher(&http.Client{Timeout: javaVersionTimeout}),
		cmdRunner:      cmdrunner.NewDefaultRunner(),
	}
}

// NewUpdaterRepositoryWithDeps creates a Java updater with injected dependencies (for testing).
func NewUpdaterRepositoryWithDeps(vf VersionFetcher) repositories.UpdaterRepository {
	return &UpdaterRepository{versionFetcher: vf, cmdRunner: cmdrunner.NewDefaultRunner()}
}

func (u *UpdaterRepository) Name() string { return updaterName }

// Detect returns true if the repository has Java marker files (Gradle or Maven).
func (u *UpdaterRepository) Detect(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) bool {
	foundGradle, errGradle := support.DetectRemote(ctx, &langJavaGradle.Detector{}, provider, repo)
	if errGradle != nil {
		logger.Warnf("[java] Gradle detection error for %s/%s: %v", repo.Organization, repo.Name, errGradle)
	}
	if foundGradle {
		return true
	}

	foundMaven, errMaven := support.DetectRemote(ctx, &langJavaMaven.Detector{}, provider, repo)
	if errMaven != nil {
		logger.Warnf("[java] Maven detection error for %s/%s: %v", repo.Organization, repo.Name, errMaven)
	}
	return foundMaven
}

// CreateUpdatePRs clones the repo, upgrades Java dependencies,
// and creates a PR.
func (u *UpdaterRepository) CreateUpdatePRs(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
) ([]entities.PullRequest, error) {
	logger.Infof("[java] Processing %s/%s", repo.Organization, repo.Name)

	latestJavaVersion, err := u.versionFetcher.FetchLatestVersion(ctx)
	if err != nil {
		logger.Warnf("[java] Failed to fetch latest Java version: %v (continuing without version upgrade)", err)
		latestJavaVersion = ""
	} else {
		logger.Infof("[java] Latest LTS Java version: %s", latestJavaVersion)
	}

	vCtx := resolveVersionContext(ctx, provider, repo, latestJavaVersion)

	// Check if PR already exists
	exists, prCheckErr := provider.PullRequestExists(ctx, repo, vCtx.BranchName)
	if prCheckErr != nil {
		logger.Warnf("[java] Failed to check existing PRs: %v", prCheckErr)
	}
	if exists {
		logger.Infof(
			"[java] PR already exists for branch %q, skipping",
			vCtx.BranchName,
		)
		return []entities.PullRequest{}, nil
	}

	if opts.DryRun {
		logDryRun(vCtx, repo)
		return []entities.PullRequest{}, nil
	}

	buildSys := detectRemoteBuildSystem(ctx, provider, repo)
	result, upgradeErr := cloneAndUpgrade(ctx, provider, repo, vCtx, buildSys)
	if upgradeErr != nil {
		return nil, upgradeErr
	}

	if !result.HasChanges {
		logger.Infof("[java] %s/%s: already up to date", repo.Organization, repo.Name)
		return []entities.PullRequest{}, nil
	}

	return openPullRequest(ctx, provider, repo, opts, vCtx, result, buildSys)
}

// logDryRun logs what would happen without actually performing the upgrade.
func logDryRun(vCtx *versionContext, repo entities.Repository) {
	if vCtx.NeedsVersionUpgrade {
		logger.Infof(
			"[java] [DRY RUN] Would upgrade Java to %s and update deps for %s/%s",
			vCtx.LatestVersion, repo.Organization, repo.Name,
		)
	} else {
		logger.Infof(
			"[java] [DRY RUN] Would update Java dependencies for %s/%s",
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
	buildSys string,
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
		JavaVersion:   vCtx.LatestVersion,
		AuthToken:     provider.AuthToken(),
		ProviderName:  provider.Name(),
		ChangelogFile: changelogFile,
		BuildSystem:   buildSys,
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
	buildSys string,
) ([]entities.PullRequest, error) {
	targetBranch := repo.DefaultBranch
	if opts.TargetBranch != "" {
		targetBranch = "refs/heads/" + opts.TargetBranch
	}

	prTitle := javaCommitMsgDeps
	if result.JavaVersionUpdated {
		prTitle = fmt.Sprintf(
			"chore(deps): upgraded Java to `%s` and updated all dependencies",
			vCtx.LatestVersion,
		)
	}
	prDesc := GeneratePRDescription(vCtx.LatestVersion, buildSys, result.JavaVersionUpdated)

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
		"[java] Created PR #%d for %s/%s: %s",
		pr.ID, repo.Organization, repo.Name, pr.URL,
	)
	return []entities.PullRequest{*pr}, nil
}

// ApplyUpdates implements repositories.LocalUpdater. It runs language-specific
// Java upgrade operations on a locally cloned repository, without performing
// any git clone, branch, commit, or push operations.
func (u *UpdaterRepository) ApplyUpdates(
	ctx context.Context,
	repoDir string,
	_ repositories.ProviderRepository,
	repo entities.Repository,
	_ entities.UpdateOptions,
) (*repositories.LocalUpdateResult, error) {
	logger.Infof("[java] Processing local clone of %s/%s", repo.Organization, repo.Name)

	// resolveLocalVersionContext handles fetching + comparison
	vCtx := resolveLocalVersionContext(ctx, repoDir)

	buildSys := detectLocalBuildSystem(repoDir)

	script := buildBatchJavaScript(buildSys)
	scriptPath := filepath.Join(repoDir, ".autoupdate-upgrade.sh")
	if writeErr := os.WriteFile(scriptPath, []byte(script), scriptFileMode); writeErr != nil {
		return nil, fmt.Errorf("failed to write script: %w", writeErr)
	}
	defer func() { _ = os.Remove(scriptPath) }()

	cmd := exec.CommandContext(ctx, "bash", scriptPath)
	cmd.Dir = repoDir
	env := append(os.Environ(), "BUILD_SYSTEM="+buildSys)
	if vCtx.LatestVersion != "" {
		env = append(env, "JAVA_VERSION="+vCtx.LatestVersion)
	}
	cmd.Env = env

	output, cmdErr := cmd.CombinedOutput()
	outputStr := string(output)
	logger.Debugf("[java] Upgrade script output:\n%s", outputStr)

	// Remove the script before checking worktree state so it does not
	// appear as an untracked file in the git status check below.
	_ = os.Remove(scriptPath)
	if cmdErr != nil {
		return nil, fmt.Errorf("upgrade script failed: %w\nOutput:\n%s", cmdErr, outputStr)
	}

	javaVersionUpdated := strings.Contains(outputStr, "JAVA_VERSION_UPDATED=true")

	// Return early if the upgrade script made no filesystem changes
	if !support.HasUncommittedChanges(ctx, repoDir) {
		logger.Infof("[java] No filesystem changes detected after upgrade script")
		return nil, repositories.ErrNoUpdatesNeeded
	}

	// Update CHANGELOG locally
	var entry string
	if javaVersionUpdated {
		entry = fmt.Sprintf(
			"- changed the Java version to `%s` and updated all dependencies",
			vCtx.LatestVersion,
		)
	} else {
		entry = javaChangelogEntryDeps
	}
	support.LocalChangelogUpdate(repoDir, []string{entry})

	commitMsg := javaCommitMsgDeps
	prTitle := commitMsg
	if javaVersionUpdated {
		commitMsg = fmt.Sprintf(
			"chore(deps): upgraded Java to `%s` and updated all dependencies",
			vCtx.LatestVersion,
		)
		prTitle = commitMsg
	}

	return &repositories.LocalUpdateResult{
		BranchName:    vCtx.BranchName,
		CommitMessage: commitMsg,
		PRTitle:       prTitle,
		PRDescription: GeneratePRDescription(vCtx.LatestVersion, buildSys, javaVersionUpdated),
	}, nil
}

// buildBatchJavaScript generates a bash script with only language-specific
// operations (no git clone, branch, commit, or push) for the batch pipeline.
func buildBatchJavaScript(buildSys string) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")
	sb.WriteString("BUILD_SYSTEM=\"" + buildSys + "\"\n\n")

	writeJavaUpgradeCommands(&sb, upgradeParams{BuildSystem: buildSys})
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
	JavaVersion   string
	AuthToken     string
	ProviderName  string
	ChangelogFile string
	BuildSystem   string // "gradle" or "maven"
}

type upgradeResult struct {
	HasChanges         bool
	JavaVersionUpdated bool
	Output             string
}

// parseJavaVersionFile extracts the Java version from a .java-version
// file content. The file typically contains just a version string like "21.0.5".
func parseJavaVersionFile(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return ""
}

// extractMajorVersion extracts the major version number from a full Java
// version string (e.g. "21.0.5" -> "21", "21" -> "21").
func extractMajorVersion(version string) string {
	if idx := strings.IndexByte(version, '.'); idx > 0 {
		return version[:idx]
	}
	return version
}

// --- build system detection ---

// detectRemoteBuildSystem determines which build system the remote repository
// uses by checking for Gradle or Maven marker files.
func detectRemoteBuildSystem(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) string {
	if provider.HasFile(ctx, repo, "build.gradle") || provider.HasFile(ctx, repo, "build.gradle.kts") {
		return buildSystemGradle
	}
	if provider.HasFile(ctx, repo, "pom.xml") {
		return buildSystemMaven
	}
	return buildSystemGradle // default
}

// detectLocalBuildSystem determines which build system the local repository
// uses by checking for Gradle or Maven marker files.
func detectLocalBuildSystem(repoDir string) string {
	if _, err := os.Stat(filepath.Join(repoDir, "build.gradle")); err == nil {
		return buildSystemGradle
	}
	if _, err := os.Stat(filepath.Join(repoDir, "build.gradle.kts")); err == nil {
		return buildSystemGradle
	}
	if _, err := os.Stat(filepath.Join(repoDir, "pom.xml")); err == nil {
		return buildSystemMaven
	}
	return buildSystemGradle // default
}

// --- version context ---

// resolveVersionContext reads the remote .java-version to find the current
// Java version and picks the right branch-name pattern.
func resolveVersionContext(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	latestJavaVersion string,
) *versionContext {
	needsVersionUpgrade := false

	if latestJavaVersion != "" && provider.HasFile(ctx, repo, ".java-version") {
		content, err := provider.GetFileContent(ctx, repo, ".java-version")
		if err == nil {
			currentVersion := parseJavaVersionFile(content)
			needsVersionUpgrade = currentVersion != "" && currentVersion != latestJavaVersion
			logger.Infof(
				"[java] Current .java-version: %s (upgrade needed: %v)",
				currentVersion, needsVersionUpgrade,
			)
		}
	}

	branchName := branchJavaDepsFmt
	if needsVersionUpgrade {
		major := extractMajorVersion(latestJavaVersion)
		branchName = fmt.Sprintf(branchJavaVersionFmt, major)
	}

	return &versionContext{
		LatestVersion:       latestJavaVersion,
		NeedsVersionUpgrade: needsVersionUpgrade,
		BranchName:          branchName,
	}
}

// resolveLocalVersionContext fetches the latest Java version and compares
// it against the local .java-version to build a versionContext.
func resolveLocalVersionContext(ctx context.Context, repoDir string) *versionContext {
	fetcher := NewHTTPJavaVersionFetcher(&http.Client{Timeout: javaVersionTimeout})
	latestJavaVersion, err := fetcher.FetchLatestVersion(ctx)
	if err != nil {
		logger.Warnf("[java] Failed to fetch latest Java version: %v (continuing without version upgrade)", err)
		latestJavaVersion = ""
	} else {
		logger.Infof("[java] Latest LTS Java version: %s", latestJavaVersion)
	}

	needsVersionUpgrade := false
	if latestJavaVersion != "" {
		javaVersionContent, readErr := os.ReadFile(filepath.Join(repoDir, ".java-version"))
		if readErr == nil {
			currentVersion := parseJavaVersionFile(string(javaVersionContent))
			needsVersionUpgrade = currentVersion != "" && currentVersion != latestJavaVersion
			logger.Infof(
				"[java] Current .java-version: %s (upgrade needed: %v)",
				currentVersion, needsVersionUpgrade,
			)
		}
	}

	branchName := branchJavaDepsFmt
	if needsVersionUpgrade {
		major := extractMajorVersion(latestJavaVersion)
		branchName = fmt.Sprintf(branchJavaVersionFmt, major)
	}

	return &versionContext{
		LatestVersion:       latestJavaVersion,
		NeedsVersionUpgrade: needsVersionUpgrade,
		BranchName:          branchName,
	}
}

// prepareChangelog reads the target repo's CHANGELOG.md (if it exists),
// inserts an entry describing the Java upgrade, and writes the modified
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
		logger.Warnf("[java] Failed to read CHANGELOG.md: %v", err)
		return ""
	}

	var entry string
	if vCtx.NeedsVersionUpgrade {
		entry = fmt.Sprintf(
			"- changed the Java version to `%s` and updated all dependencies",
			vCtx.LatestVersion,
		)
	} else {
		entry = javaChangelogEntryDeps
	}

	modified := entities.InsertChangelogEntry(content, []string{entry})
	if modified == content {
		return ""
	}

	tmpFile, writeErr := os.CreateTemp("", "autoupdate-changelog-*.md")
	if writeErr != nil {
		logger.Warnf("[java] Failed to create temp changelog file: %v", writeErr)
		return ""
	}

	if _, writeErr = tmpFile.WriteString(modified); writeErr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		logger.Warnf("[java] Failed to write temp changelog: %v", writeErr)
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

	tmpDir, err := os.MkdirTemp("", "autoupdate-java-*")
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
	result.JavaVersionUpdated = strings.Contains(result.Output, "JAVA_VERSION_UPDATED=true")
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

	// Java upgrade commands
	writeJavaUpgradeCommands(&sb, params)

	// Update Dockerfile Java image tags
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

func writeJavaUpgradeCommands(sb *strings.Builder, _ upgradeParams) {
	// Update .java-version if it exists and a new version is available
	sb.WriteString("# Check and update Java version\n")
	sb.WriteString("JAVA_VERSION_CHANGED=false\n")
	sb.WriteString("if [ -n \"${JAVA_VERSION:-}\" ] && [ -f \".java-version\" ]; then\n")
	sb.WriteString("    CURRENT_JAVA_VERSION=$(head -1 .java-version | tr -d '[:space:]')\n")
	sb.WriteString(
		"    if [ -n \"$CURRENT_JAVA_VERSION\" ] && [ \"$CURRENT_JAVA_VERSION\" != \"$JAVA_VERSION\" ]; then\n",
	)
	sb.WriteString("        echo \"Updating .java-version from $CURRENT_JAVA_VERSION to $JAVA_VERSION...\"\n")
	sb.WriteString("        echo \"$JAVA_VERSION\" > .java-version\n")
	sb.WriteString("        JAVA_VERSION_CHANGED=true\n")
	sb.WriteString("        echo \"JAVA_VERSION_UPDATED=true\"\n")
	sb.WriteString("    else\n")
	sb.WriteString("        echo \"Java version already at $CURRENT_JAVA_VERSION, skipping version update\"\n")
	sb.WriteString("        echo \"JAVA_VERSION_UPDATED=false\"\n")
	sb.WriteString("    fi\n")
	sb.WriteString("else\n")
	sb.WriteString("    echo \"JAVA_VERSION_UPDATED=false\"\n")
	sb.WriteString("fi\n\n")

	// Build system specific commands
	sb.WriteString("# Update dependencies using detected build system\n")
	sb.WriteString("echo \"Using build system: $BUILD_SYSTEM\"\n")
	sb.WriteString("case \"$BUILD_SYSTEM\" in\n")

	// Gradle commands
	sb.WriteString("    gradle)\n")
	sb.WriteString("        # Refresh Gradle wrapper if gradlew exists\n")
	sb.WriteString("        if [ -f \"./gradlew\" ]; then\n")
	sb.WriteString("            echo \"Refreshing Gradle wrapper...\"\n")
	sb.WriteString(
		"            chmod +x ./gradlew\n",
	)
	sb.WriteString(
		"            ./gradlew wrapper 2>&1 || " +
			"echo \"WARNING: Gradle wrapper refresh had some errors (continuing anyway)\"\n",
	)
	sb.WriteString("        fi\n\n")
	sb.WriteString("        # Update dependency lockfiles if they exist\n")
	sb.WriteString(
		"        if ls *.lockfile gradle.lockfile 2>/dev/null | grep -q .; then\n",
	)
	sb.WriteString("            echo \"Updating Gradle dependency locks...\"\n")
	sb.WriteString("            if [ -f \"./gradlew\" ]; then\n")
	sb.WriteString(
		"                ./gradlew dependencies --write-locks 2>&1 || " +
			"echo \"WARNING: Gradle lock update had some errors (continuing anyway)\"\n",
	)
	sb.WriteString("            fi\n")
	sb.WriteString("        fi\n")
	sb.WriteString("        ;;\n")

	// Maven commands
	sb.WriteString("    maven)\n")
	sb.WriteString("        # Determine Maven command\n")
	sb.WriteString("        if [ -f \"./mvnw\" ]; then\n")
	sb.WriteString("            MVN_CMD=\"./mvnw\"\n")
	sb.WriteString("            chmod +x ./mvnw\n")
	sb.WriteString("        else\n")
	sb.WriteString("            MVN_CMD=\"mvn\"\n")
	sb.WriteString("        fi\n\n")
	sb.WriteString("        echo \"Updating Maven properties...\"\n")
	sb.WriteString(
		"        $MVN_CMD versions:update-properties -DgenerateBackupPoms=false 2>&1 || " +
			"echo \"WARNING: Maven properties update had some errors (continuing anyway)\"\n",
	)
	sb.WriteString("\n        echo \"Updating Maven dependencies to latest releases...\"\n")
	sb.WriteString(
		"        $MVN_CMD versions:use-latest-releases -DgenerateBackupPoms=false 2>&1 || " +
			"echo \"WARNING: Maven dependency update had some errors (continuing anyway)\"\n",
	)
	sb.WriteString("        ;;\n")

	sb.WriteString("esac\n\n")
}

func writeDockerfileUpdate(sb *strings.Builder) {
	sb.WriteString("# Update Dockerfile Java image tags when the Java version was bumped.\n")
	sb.WriteString("if [ \"$JAVA_VERSION_CHANGED\" = \"true\" ]; then\n")
	sb.WriteString("    JAVA_MAJOR=$(echo \"$JAVA_VERSION\" | cut -d. -f1)\n")
	sb.WriteString("    echo \"Updating Dockerfile Java image tags to major version $JAVA_MAJOR...\"\n")
	sb.WriteString(
		"    find . -type f -not -path './.git/*' " +
			"\\( -name 'Dockerfile' -o -name 'Dockerfile.*' -o -name '*.Dockerfile' \\) " +
			"-print0 | while IFS= read -r -d '' df; do\n",
	)
	sb.WriteString("        UPDATED=false\n")
	sb.WriteString("        for IMAGE in eclipse-temurin openjdk amazoncorretto; do\n")
	sb.WriteString("            if grep -q \"${IMAGE}:[0-9]\" \"$df\"; then\n")
	sb.WriteString(
		"                sed \"s|${IMAGE}:[0-9][0-9]*|${IMAGE}:${JAVA_MAJOR}|g\" \"$df\" > \"$df.tmp\" && mv \"$df.tmp\" \"$df\"\n",
	)
	sb.WriteString("                UPDATED=true\n")
	sb.WriteString("            fi\n")
	sb.WriteString("        done\n")
	sb.WriteString("        if [ \"$UPDATED\" = \"true\" ]; then\n")
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
	sb.WriteString("    if [ \"$JAVA_VERSION_CHANGED\" = \"true\" ]; then\n")
	sb.WriteString(
		"        git commit -m \"chore(deps): upgraded Java to `$JAVA_VERSION` and updated all dependencies\"\n",
	)
	sb.WriteString("    else\n")
	sb.WriteString("        git commit -m \"chore(deps): updated Java dependencies\"\n")
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
		"BUILD_SYSTEM="+params.BuildSystem,
	)
	if params.JavaVersion != "" {
		env = append(env, "JAVA_VERSION="+params.JavaVersion)
	}
	if params.ChangelogFile != "" {
		env = append(env, "CHANGELOG_FILE="+params.ChangelogFile)
	}
	return env
}

// GeneratePRDescription builds a markdown PR description for a Java
// dependency upgrade. Exported so that the local-mode CLI handler can
// reuse the same description format.
func GeneratePRDescription(javaVersion, buildSys string, javaVersionUpdated bool) string {
	var sb strings.Builder
	sb.WriteString("## Summary\n\n")
	if javaVersionUpdated {
		sb.WriteString(
			"This PR upgrades the Java version to **" + javaVersion + "** and updates all dependencies.\n\n",
		)
	} else {
		sb.WriteString(
			"This PR updates all Java dependencies to their latest versions.\n\n",
		)
	}
	sb.WriteString("### Changes\n\n")
	if javaVersionUpdated {
		sb.WriteString("- Updated `.java-version` to `" + javaVersion + "`\n")
		sb.WriteString("- Updated Dockerfile Java image tags\n")
	}

	switch buildSys {
	case buildSystemMaven:
		sb.WriteString("- Ran `mvn versions:update-properties` to update dependency properties\n")
		sb.WriteString("- Ran `mvn versions:use-latest-releases` to update dependencies\n")
	default:
		sb.WriteString("- Ran `./gradlew wrapper --gradle-version latest` to upgrade the Gradle wrapper\n")
		sb.WriteString("- Updated Gradle dependency lockfiles (if present)\n")
	}

	sb.WriteString("\n### Review Checklist\n\n")
	sb.WriteString("- [ ] Verify build passes\n")
	sb.WriteString("- [ ] Verify tests pass\n")
	sb.WriteString("- [ ] Review dependency changes\n")
	sb.WriteString("\n---\n")
	sb.WriteString("*This PR was automatically created by [autoupdate](https://github.com/rios0rios0/autoupdate)*\n")
	return sb.String()
}
