package java

import (
	"context"
	"fmt"
	"net/http"
	"os"
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
	updaterName = "java"
	// runtimeName is how the runtime is spelled in log lines, commit subjects
	// and pull request text.
	runtimeName        = "Java"
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

// defaultRunner is the package-level command runner for remote-mode functions.
var defaultRunner cmdrunner.Runner = cmdrunner.NewDefaultRunner() //nolint:gochecknoglobals // test override

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

	latestJavaVersion := support.LatestVersion(
		support.VersionFeed{LogPrefix: updaterName, Runtime: runtimeName, Release: "LTS Java"},
		func() (string, error) { return u.versionFetcher.FetchLatestVersion(ctx) },
	)

	vCtx := resolveVersionContext(ctx, provider, repo, latestJavaVersion)

	return support.RunRemoteUpgrade(ctx, provider, repo, opts, support.RemoteUpgrade{
		LogPrefix:  updaterName,
		BranchName: vCtx.BranchName,
		DryRun:     func() { logDryRun(vCtx, repo) },
		Upgrade: func(ctx context.Context) (support.RemoteUpgradeResult, error) {
			buildSys := detectRemoteBuildSystem(ctx, provider, repo)
			return cloneAndUpgrade(ctx, provider, repo, vCtx, buildSys, opts.AllowMajorUpdates)
		},
	})
}

// logDryRun logs what would happen without actually performing the upgrade.
func logDryRun(vCtx *versionContext, repo entities.Repository) {
	support.LogRemoteDryRun(updaterName, repo, support.DryRunPlan{
		Runtime:         runtimeName,
		Version:         vCtx.LatestVersion,
		UpgradesVersion: vCtx.NeedsVersionUpgrade,
		Dependencies:    "Java dependencies",
	})
}

// cloneAndUpgrade prepares the changelog, clones the repository, runs the
// upgrade script, and describes the pull request the upgrade earned.
func cloneAndUpgrade(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	vCtx *versionContext,
	buildSys string,
	allowMajorUpdates bool,
) (support.RemoteUpgradeResult, error) {
	changelog := support.StageRemoteChangelog(ctx, provider, repo, changelogEntries(vCtx))
	defer changelog.Remove()

	result, err := upgradeRepo(ctx, upgradeParams{
		CloneTarget: support.CloneTargetFor(provider, repo, vCtx.BranchName, changelog),
		JavaVersion: vCtx.LatestVersion,
		BuildSystem: buildSys,

		AllowMajorUpdates: allowMajorUpdates,
	})
	if err != nil {
		return support.RemoteUpgradeResult{}, fmt.Errorf("failed to upgrade: %w", err)
	}

	return support.RemoteUpgradeResult{
		Pushed: result.HasChanges,
		PullRequest: support.PullRequestSpec{
			LogPrefix:   updaterName,
			BranchName:  vCtx.BranchName,
			Title:       upgradeSubject(vCtx.LatestVersion, result.JavaVersionUpdated),
			Description: GeneratePRDescription(vCtx.LatestVersion, buildSys, result.JavaVersionUpdated),
		},
	}, nil
}

// upgradeSubject is the one-line summary of what the run changed, used as both
// the commit subject and the pull request title.
func upgradeSubject(javaVersion string, javaVersionUpdated bool) string {
	if javaVersionUpdated {
		return fmt.Sprintf(
			"chore(deps): upgraded Java to `%s` and updated all dependencies",
			javaVersion,
		)
	}

	return javaCommitMsgDeps
}

// ApplyUpdates implements repositories.LocalUpdater. It runs language-specific
// Java upgrade operations on a locally cloned repository, without performing
// any git clone, branch, commit, or push operations.
func (u *UpdaterRepository) ApplyUpdates(
	ctx context.Context,
	repoDir string,
	_ repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
) (*repositories.LocalUpdateResult, error) {
	logger.Infof("[java] Processing local clone of %s/%s", repo.Organization, repo.Name)

	// resolveLocalVersionContext handles fetching + comparison
	vCtx := resolveLocalVersionContext(ctx, repoDir)

	buildSys := detectLocalBuildSystem(repoDir)

	env := append(os.Environ(), "BUILD_SYSTEM="+buildSys)
	if vCtx.LatestVersion != "" {
		env = append(env, "JAVA_VERSION="+vCtx.LatestVersion)
	}

	outputStr, runErr := cmdrunner.RunScript(ctx, u.cmdRunner, cmdrunner.ScriptRun{
		Body:        buildBatchJavaScript(buildSys, opts.AllowMajorUpdates),
		TempPattern: "autoupdate-java-local-*",
		Dir:         repoDir,
		Env:         env,
		LogPrefix:   updaterName,
		Verbose:     true,
	})
	if runErr != nil {
		return nil, runErr
	}

	javaVersionUpdated := strings.Contains(outputStr, "JAVA_VERSION_UPDATED=true")

	// Return early if the upgrade script made no filesystem changes
	if !support.HasUncommittedChanges(ctx, repoDir) {
		logger.Infof("[java] No filesystem changes detected after upgrade script")
		return nil, repositories.ErrNoUpdatesNeeded
	}

	// Record the upgrade in the repository's changelog.
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

	commitMsg := upgradeSubject(vCtx.LatestVersion, javaVersionUpdated)
	prTitle := commitMsg

	return &repositories.LocalUpdateResult{
		BranchName:    vCtx.BranchName,
		CommitMessage: commitMsg,
		PRTitle:       prTitle,
		PRDescription: GeneratePRDescription(vCtx.LatestVersion, buildSys, javaVersionUpdated),
	}, nil
}

// buildBatchJavaScript generates a bash script with only language-specific
// operations (no git clone, branch, commit, or push) for the batch pipeline.
func buildBatchJavaScript(buildSys string, allowMajorUpdates bool) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")
	sb.WriteString("BUILD_SYSTEM=\"" + buildSys + "\"\n\n")

	writeJavaUpgradeCommands(&sb, upgradeParams{ //nolint:exhaustruct // only these two reach the script
		BuildSystem:       buildSys,
		AllowMajorUpdates: allowMajorUpdates,
	})
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
	// CloneTarget identifies the repository the script clones and how it
	// authenticates against it, and carries the staged changelog.
	support.CloneTarget

	JavaVersion string
	BuildSystem string // "gradle" or "maven"
	// AllowMajorUpdates becomes -DallowMajorUpdates on both versions-maven-plugin
	// goals. See entities.Settings.AllowMajorUpdates for why it defaults to true.
	AllowMajorUpdates bool
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
			needsVersionUpgrade = support.IsNewerVersion(currentVersion, latestJavaVersion)
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
			needsVersionUpgrade = support.IsNewerVersion(currentVersion, latestJavaVersion)
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

// changelogEntries renders the Keep a Changelog bullet describing the
// upgrade. The staging helpers turn it into a chlog fragment when the
// target repository uses that format instead.
func changelogEntries(vCtx *versionContext) []string {
	if vCtx.NeedsVersionUpgrade {
		return []string{fmt.Sprintf(
			"- changed the Java version to `%s` and updated all dependencies",
			vCtx.LatestVersion,
		)}
	}
	return []string{javaChangelogEntryDeps}
}

// --- clone + upgrade ---

func upgradeRepo(
	ctx context.Context,
	params upgradeParams,
) (*upgradeResult, error) {
	output, err := cmdrunner.RunCloneScript(ctx, defaultRunner, cmdrunner.CloneScriptRun{
		Body:        buildUpgradeScript(params, ""),
		TempPattern: "autoupdate-java-*",
		Env:         func(repoDir string) []string { return buildEnv(params, repoDir) },
		Secrets:     []string{params.AuthToken},
	})
	if err != nil {
		return nil, err
	}

	return &upgradeResult{
		HasChanges:         strings.Contains(output, "CHANGES_PUSHED=true"),
		JavaVersionUpdated: strings.Contains(output, "JAVA_VERSION_UPDATED=true"),
		Output:             output,
	}, nil
}

func buildUpgradeScript(
	params upgradeParams,
	repoDir string,
) string {
	_ = repoDir // used via env vars in the script

	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	// Set up git credentials based on provider, then clone onto the branch.
	writeGitAuth(&sb, params)
	sb.WriteString(support.RemoteCloneScript())

	// Java upgrade commands
	writeJavaUpgradeCommands(&sb, params)

	// Update Dockerfile Java image tags
	writeDockerfileUpdate(&sb)

	// Copy in the staged changelog: an edited CHANGELOG.md, or a chlog
	// fragment when the target repository uses that format.
	sb.WriteString(support.ChangelogUpdateScript())

	// Check for changes and commit/push
	writeCommitAndPush(&sb)

	return sb.String()
}

func writeGitAuth(sb *strings.Builder, params upgradeParams) {
	sb.WriteString(support.GitAuthScript(params.ProviderName))
}

func writeJavaUpgradeCommands(sb *strings.Builder, params upgradeParams) {
	// Only when the fetched release is genuinely ahead of the pin: the feed
	// reports the newest LTS JDK, so a repository already on a later JDK is
	// ahead of it and must keep its pin.
	sb.WriteString(support.VersionPinUpdateScript(support.VersionPinUpdate{
		File:       ".java-version",
		Subject:    runtimeName,
		VersionVar: "JAVA_VERSION",
		CurrentVar: "CURRENT_JAVA_VERSION",
		ChangedVar: "JAVA_VERSION_CHANGED",
		MarkerVar:  "JAVA_VERSION",
	}))

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
	// Maven has no notion of a pre-release, so `3.0.0-beta3`, `7.1.0-M1` and
	// `5.7-alpha1` are ordinary releases to it and both goals below treat them
	// as candidates. MAVEN_VERSION_IGNORE supplies the missing concept.
	//
	// The major-version flag is a separate question and a configurable one: both
	// goals take -DallowMajorUpdates from MAVEN_ALLOW_MAJOR below, which carries
	// the resolved allow_major_updates -- on unless a layer turns it off. The
	// pre-release filtering is independent of it and applies in both modes, since
	// taking a beta is how CVEs get reintroduced rather than remediated.
	// Single-quoted, and every flag passed as its own argument rather than through
	// an accumulator variable. The ignore value is a list of regexes carrying `.*`
	// and `(?i)`, which an unquoted expansion would subject to word splitting and
	// pathname expansion -- so a file in the repository root could rewrite the flag
	// and the filtering would silently not apply. MavenVersionIgnore never emits a
	// single quote (assertMavenIgnoreQuotable guards that), so the wrapping is safe.
	fmt.Fprintf(sb, "        MAVEN_VERSION_IGNORE='%s'\n\n", support.MavenVersionIgnore())

	fmt.Fprintf(sb, "        MAVEN_ALLOW_MAJOR=%t\n\n", params.AllowMajorUpdates)

	writeMavenGoal(sb, "versions:update-properties",
		"Updating Maven properties...",
		"WARNING: Maven properties update had some errors (continuing anyway)")
	sb.WriteString("\n")
	writeMavenGoal(sb, "versions:use-latest-releases",
		"Updating Maven dependencies to latest releases...",
		"WARNING: Maven dependency update had some errors (continuing anyway)")
	sb.WriteString("        ;;\n")

	sb.WriteString("esac\n\n")
}

// writeMavenGoal emits one versions-maven-plugin invocation with the guard flags
// spelled out as individual quoted arguments.
//
// `-Dmaven.version.ignore` is double-quoted so the variable expands without being
// split or glob-expanded; the rest carry no metacharacters but are written out
// rather than accumulated, so a later addition cannot reintroduce the unquoted
// expansion this replaced.
func writeMavenGoal(sb *strings.Builder, goal, announce, warning string) {
	fmt.Fprintf(sb, "        echo %q\n", announce)
	fmt.Fprintf(sb, "        $MVN_CMD %s \\\n", goal)
	sb.WriteString("            -DgenerateBackupPoms=false \\\n")
	sb.WriteString("            -DallowSnapshots=false \\\n")
	sb.WriteString("            -DallowMajorUpdates=$MAVEN_ALLOW_MAJOR \\\n")
	sb.WriteString("            \"-Dmaven.version.ignore=$MAVEN_VERSION_IGNORE\" 2>&1 || \\\n")
	fmt.Fprintf(sb, "            echo %q\n", warning)
}

func writeDockerfileUpdate(sb *strings.Builder) {
	// The JDK images pin a bare major, so the tag written is the major of the
	// fetched version rather than the version itself.
	sb.WriteString(support.DockerfileTagUpdateScript(support.DockerfileTagUpdate{
		ChangedVar: "JAVA_VERSION_CHANGED",
		VersionVar: "JAVA_MAJOR",
		Subject:    runtimeName,
		Prelude:    "    JAVA_MAJOR=$(echo \"$JAVA_VERSION\" | cut -d. -f1)\n",
		Images: []support.DockerfileImage{
			{Name: "eclipse-temurin", TagPattern: support.MajorOnlyTagPattern},
			{Name: "openjdk", TagPattern: support.MajorOnlyTagPattern},
			{Name: "amazoncorretto", TagPattern: support.MajorOnlyTagPattern},
		},
	}))
}

func writeCommitAndPush(sb *strings.Builder) {
	sb.WriteString(support.CommitAndPushScript(support.CommitAndPush{
		UpgradedWhen: `[ "$JAVA_VERSION_CHANGED" = "true" ]`,
		UpgradeMessage: "chore(deps): upgraded Java to `$JAVA_VERSION` " +
			"and updated all dependencies",
		DepsMessage: javaCommitMsgDeps,
	}))
}

func buildEnv(params upgradeParams, repoDir string) []string {
	env := append(support.CloneEnv(params.CloneTarget, repoDir),
		"BUILD_SYSTEM="+params.BuildSystem,
	)
	if params.JavaVersion != "" {
		env = append(env, "JAVA_VERSION="+params.JavaVersion)
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
		sb.WriteString(
			"- Held every version on its current major line and skipped pre-releases " +
				"(alpha, beta, milestone, RC, snapshot), which Maven reports as ordinary " +
				"releases\n",
		)
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
