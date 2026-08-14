package dart

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
	langDart "github.com/rios0rios0/langforge/pkg/infrastructure/languages/dart"
)

const (
	updaterName       = "dart"
	sdkVersionTimeout = 15 * time.Second
	pubspecFile       = "pubspec.yaml"

	// Toolchain executables. A Flutter project must be driven through the
	// flutter wrapper: `dart pub get` cannot resolve the SDK-sourced packages
	// (flutter, flutter_test, flutter_localizations) that every Flutter project
	// depends on.
	toolchainDart    = "dart"
	toolchainFlutter = "flutter"

	// Branch name patterns. One is used when the pinned Flutter SDK is being
	// bumped as well; the other when only pub dependencies are refreshed.
	branchSDKVersionFmt = "chore/upgrade-flutter-%s"
	branchDartDepsFmt   = "chore/upgrade-dart-deps"

	// Commit/PR messages and changelog entries used across remote and local modes.
	dartCommitMsgDeps      = "chore(deps): updated Dart pub dependencies"
	dartChangelogEntryDeps = "- changed the Dart pub dependencies to their latest versions"
)

// UpdaterRepository implements repositories.UpdaterRepository for Dart and
// Flutter dependencies. It clones the repository locally, runs pub through the
// project's own toolchain, pushes the changes, and creates a PR via the
// provider API.
type UpdaterRepository struct {
	dartFetcher    VersionFetcher
	flutterFetcher VersionFetcher
	cmdRunner      cmdrunner.Runner
}

// NewUpdaterRepository creates a new Dart updater with default dependencies.
func NewUpdaterRepository() repositories.UpdaterRepository {
	return newUpdaterRepository()
}

// newUpdaterRepository builds the concrete updater, for the local-mode entry
// point which needs the struct's own helpers rather than the port.
func newUpdaterRepository() *UpdaterRepository {
	client := &http.Client{Timeout: sdkVersionTimeout}
	return &UpdaterRepository{
		dartFetcher:    NewHTTPDartVersionFetcher(client),
		flutterFetcher: NewHTTPFlutterVersionFetcher(client),
		cmdRunner:      cmdrunner.NewDefaultRunner(),
	}
}

// NewUpdaterRepositoryWithDeps creates a Dart updater with injected dependencies (for testing).
func NewUpdaterRepositoryWithDeps(dartFetcher, flutterFetcher VersionFetcher) repositories.UpdaterRepository {
	return &UpdaterRepository{
		dartFetcher:    dartFetcher,
		flutterFetcher: flutterFetcher,
		cmdRunner:      cmdrunner.NewDefaultRunner(),
	}
}

func (u *UpdaterRepository) Name() string { return updaterName }

// Detect returns true if the repository has a pubspec.yaml.
func (u *UpdaterRepository) Detect(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) bool {
	found, err := support.DetectRemote(ctx, &langDart.Detector{}, provider, repo)
	if err != nil {
		logger.Warnf("[dart] detection error for %s/%s: %v", repo.Organization, repo.Name, err)
		return false
	}
	return found
}

// CreateUpdatePRs clones the repo, upgrades pub dependencies, and creates a PR.
func (u *UpdaterRepository) CreateUpdatePRs(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
) ([]entities.PullRequest, error) {
	logger.Infof("[dart] Processing %s/%s", repo.Organization, repo.Name)

	vCtx := u.resolveVersionContext(ctx, provider, repo)

	exists, prCheckErr := provider.PullRequestExists(ctx, repo, vCtx.BranchName)
	if prCheckErr != nil {
		logger.Warnf("[dart] Failed to check existing PRs: %v", prCheckErr)
	}
	if exists {
		logger.Infof("[dart] PR already exists for branch %q, skipping", vCtx.BranchName)
		return []entities.PullRequest{}, nil
	}

	if opts.DryRun {
		logDryRun(vCtx, repo)
		return []entities.PullRequest{}, nil
	}

	result, upgradeErr := cloneAndUpgrade(ctx, u.cmdRunner, provider, repo, vCtx)
	if upgradeErr != nil {
		return nil, upgradeErr
	}

	if !result.HasChanges {
		logger.Infof("[dart] %s/%s: already up to date", repo.Organization, repo.Name)
		return []entities.PullRequest{}, nil
	}

	return openPullRequest(ctx, provider, repo, opts, vCtx, result)
}

// ApplyUpdates implements repositories.LocalUpdater. It runs the pub upgrade on
// an already-cloned repository, without any git clone, branch, commit or push.
func (u *UpdaterRepository) ApplyUpdates(
	ctx context.Context,
	repoDir string,
	_ repositories.ProviderRepository,
	repo entities.Repository,
	_ entities.UpdateOptions,
) (*repositories.LocalUpdateResult, error) {
	logger.Infof("[dart] Processing local clone of %s/%s", repo.Organization, repo.Name)

	vCtx := u.resolveLocalVersionContext(ctx, repoDir)

	// The SDK pin is rewritten here rather than in the script: .fvmrc is JSON,
	// and parsing JSON with grep and sed is how a flavors block gets destroyed.
	sdkUpdated := applyFvmPin(repoDir, vCtx)

	outputStr, runErr := runUpgradeScript(ctx, u.cmdRunner, repoDir, vCtx)
	if runErr != nil {
		return nil, runErr
	}
	logger.Debugf("[dart] Upgrade script output:\n%s", outputStr)

	if !support.HasUncommittedChanges(ctx, repoDir) {
		logger.Infof("[dart] No filesystem changes detected after upgrade script")
		return nil, repositories.ErrNoUpdatesNeeded
	}

	support.LocalChangelogUpdate(repoDir, changelogEntries(vCtx, sdkUpdated))

	commitMsg := commitMessage(vCtx, sdkUpdated)
	return &repositories.LocalUpdateResult{
		BranchName:    vCtx.BranchName,
		CommitMessage: commitMsg,
		PRTitle:       commitMsg,
		PRDescription: GeneratePRDescription(vCtx.LatestVersion, vCtx.Toolchain, sdkUpdated),
	}, nil
}

// runUpgradeScript executes the pub upgrade script with repoDir as the working
// directory. The script itself is written outside the repository, so it can
// never show up as an untracked file in the worktree the caller inspects next.
func runUpgradeScript(
	ctx context.Context,
	runner cmdrunner.Runner,
	repoDir string,
	vCtx *versionContext,
) (string, error) {
	return cmdrunner.RunScript(ctx, runner, cmdrunner.ScriptRun{
		Body:        buildBatchDartScript(),
		TempPattern: "autoupdate-dart-batch-*",
		Dir:         repoDir,
		Env:         append(os.Environ(), "PUB_EXECUTABLE="+vCtx.Toolchain),
	})
}

// applyFvmPin bumps the pinned Flutter SDK when one is pinned and outdated.
func applyFvmPin(repoDir string, vCtx *versionContext) bool {
	if !vCtx.NeedsVersionUpgrade || vCtx.LatestVersion == "" {
		return false
	}
	changed, err := WriteFvmVersion(repoDir, vCtx.LatestVersion)
	if err != nil {
		logger.Warnf("[dart] Failed to update %s: %v (continuing with the dependency upgrade)", FvmConfigFile, err)
		return false
	}
	if changed {
		logger.Infof("[dart] Updated %s to %s", FvmConfigFile, vCtx.LatestVersion)
	}
	return changed
}

// buildBatchDartScript generates a bash script with only the pub operations
// (no git clone, branch, commit or push) for the batch pipeline.
func buildBatchDartScript() string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	writeDartUpgradeCommands(&sb)

	return sb.String()
}

// --- internal types ---

type versionContext struct {
	// Toolchain is "dart" or "flutter"; see langforge's dart.IsFlutter, which is
	// the single place that decision is made.
	Toolchain           string
	LatestVersion       string
	NeedsVersionUpgrade bool
	BranchName          string
}

type upgradeParams struct {
	CloneURL      string
	DefaultBranch string
	BranchName    string
	Toolchain     string
	SDKVersion    string
	AuthToken     string
	ProviderName  string
	// Changelog is the staged changelog payload the script copies into
	// the clone; an empty value leaves the repository's changelog untouched.
	Changelog support.StagedChangelog
}

type upgradeResult struct {
	HasChanges bool
	SDKUpdated bool
	Output     string
}

// --- version context ---

// sdkFetcher returns the release channel that matches the project's toolchain.
func (u *UpdaterRepository) sdkFetcher(toolchain string) VersionFetcher {
	if toolchain == toolchainFlutter {
		return u.flutterFetcher
	}
	return u.dartFetcher
}

// resolveVersionContext reads the remote pubspec.yaml to pick the toolchain,
// then the remote .fvmrc to decide whether the pinned SDK is behind.
func (u *UpdaterRepository) resolveVersionContext(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) *versionContext {
	toolchain := toolchainDart
	if manifest, err := provider.GetFileContent(ctx, repo, pubspecFile); err == nil {
		if langDart.IsFlutterManifest(manifest) {
			toolchain = toolchainFlutter
		}
	} else {
		logger.Warnf("[dart] Failed to read %s: %v (assuming a plain Dart package)", pubspecFile, err)
	}

	latest := u.fetchLatestSDK(ctx, toolchain)

	// Only a Flutter project's pin is read. .fvmrc names a *Flutter* SDK, and
	// `latest` came from whichever channel the toolchain selected — so a plain
	// Dart package that happens to carry an .fvmrc (a Dart tool living beside a
	// Flutter app, say) would otherwise have its pin rewritten with a Dart
	// version, and the branch and commit would both claim a Flutter upgrade.
	currentPin := ""
	if toolchain == toolchainFlutter && latest != "" && provider.HasFile(ctx, repo, FvmConfigFile) {
		if content, err := provider.GetFileContent(ctx, repo, FvmConfigFile); err == nil {
			currentPin = ParseFvmVersion(content)
		}
	}

	return newVersionContext(toolchain, latest, currentPin)
}

// fetchLatestSDK resolves the latest stable SDK for the toolchain, degrading to
// a dependency-only upgrade when the release channel cannot be reached.
func (u *UpdaterRepository) fetchLatestSDK(ctx context.Context, toolchain string) string {
	latest, err := u.sdkFetcher(toolchain).FetchLatestVersion(ctx)
	if err != nil {
		logger.Warnf("[dart] Failed to fetch latest %s version: %v (continuing without SDK upgrade)", toolchain, err)
		return ""
	}
	logger.Infof("[dart] Latest stable %s version: %s", toolchain, latest)
	return latest
}

// newVersionContext assembles the context and picks the branch name.
func newVersionContext(toolchain, latest, currentPin string) *versionContext {
	needsVersionUpgrade := latest != "" && currentPin != "" && currentPin != latest
	if currentPin != "" {
		logger.Infof("[dart] Current %s pin: %s (upgrade needed: %v)", FvmConfigFile, currentPin, needsVersionUpgrade)
	}

	branchName := branchDartDepsFmt
	if needsVersionUpgrade {
		branchName = fmt.Sprintf(branchSDKVersionFmt, latest)
	}

	return &versionContext{
		Toolchain:           toolchain,
		LatestVersion:       latest,
		NeedsVersionUpgrade: needsVersionUpgrade,
		BranchName:          branchName,
	}
}

// changelogEntries renders the Keep a Changelog bullet describing the upgrade.
// The staging helpers turn it into a chlog fragment when the target repository
// uses that format instead.
func changelogEntries(vCtx *versionContext, sdkUpdated bool) []string {
	if sdkUpdated {
		return []string{fmt.Sprintf(
			"- changed the Flutter SDK to `%s` and updated all pub dependencies",
			vCtx.LatestVersion,
		)}
	}
	return []string{dartChangelogEntryDeps}
}

// commitMessage renders the commit subject for the upgrade.
func commitMessage(vCtx *versionContext, sdkUpdated bool) string {
	if sdkUpdated {
		return fmt.Sprintf(
			"chore(deps): upgraded Flutter to `%s` and updated all pub dependencies",
			vCtx.LatestVersion,
		)
	}
	return dartCommitMsgDeps
}

// logDryRun logs what would happen without actually performing the upgrade.
func logDryRun(vCtx *versionContext, repo entities.Repository) {
	if vCtx.NeedsVersionUpgrade {
		logger.Infof(
			"[dart] [DRY RUN] Would upgrade Flutter to %s and update pub dependencies for %s/%s",
			vCtx.LatestVersion, repo.Organization, repo.Name,
		)
		return
	}
	logger.Infof(
		"[dart] [DRY RUN] Would update pub dependencies for %s/%s with %s",
		repo.Organization, repo.Name, vCtx.Toolchain,
	)
}

// --- clone + upgrade ---

// cloneAndUpgrade prepares the changelog, clones the repository, runs the
// upgrade script, and returns the result.
func cloneAndUpgrade(
	ctx context.Context,
	runner cmdrunner.Runner,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	vCtx *versionContext,
) (*upgradeResult, error) {
	changelog := support.StageRemoteChangelog(ctx, provider, repo, changelogEntries(vCtx, vCtx.NeedsVersionUpgrade))
	defer changelog.Remove()

	result, err := upgradeRepo(ctx, runner, upgradeParams{
		CloneURL:      provider.CloneURL(repo),
		DefaultBranch: strings.TrimPrefix(repo.DefaultBranch, "refs/heads/"),
		BranchName:    vCtx.BranchName,
		Toolchain:     vCtx.Toolchain,
		SDKVersion:    sdkVersionFor(vCtx),
		AuthToken:     provider.AuthToken(),
		ProviderName:  provider.Name(),
		Changelog:     changelog,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upgrade: %w", err)
	}

	return result, nil
}

// sdkVersionFor returns the SDK version the script should pin, or empty when
// the repository pins none or is already current.
func sdkVersionFor(vCtx *versionContext) string {
	if vCtx.NeedsVersionUpgrade {
		return vCtx.LatestVersion
	}
	return ""
}

// openPullRequest creates the PR on the hosting provider after a successful upgrade.
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

	pr, createErr := provider.CreatePullRequest(ctx, repo, entities.PullRequestInput{
		SourceBranch: "refs/heads/" + vCtx.BranchName,
		TargetBranch: targetBranch,
		Title:        commitMessage(vCtx, result.SDKUpdated),
		Description:  GeneratePRDescription(vCtx.LatestVersion, vCtx.Toolchain, result.SDKUpdated),
		AutoComplete: opts.AutoComplete,
	})
	if createErr != nil {
		return nil, fmt.Errorf("failed to create PR: %w", createErr)
	}

	logger.Infof("[dart] Created PR #%d for %s/%s: %s", pr.ID, repo.Organization, repo.Name, pr.URL)
	return []entities.PullRequest{*pr}, nil
}

func upgradeRepo(ctx context.Context, runner cmdrunner.Runner, params upgradeParams) (*upgradeResult, error) {
	// The clone lives in its own throwaway directory rather than beside the
	// script, so nothing the script writes can outlive this call.
	cloneRoot, err := os.MkdirTemp("", "autoupdate-dart-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(cloneRoot)

	output, runErr := cmdrunner.RunScript(ctx, runner, cmdrunner.ScriptRun{
		Body:        buildUpgradeScript(params),
		TempPattern: "autoupdate-dart-script-*",
		Dir:         cloneRoot,
		Env:         buildEnv(params, filepath.Join(cloneRoot, "repo")),
		RedactOutput: func(out string) string {
			return support.RedactTokens(out, params.AuthToken)
		},
	})
	if runErr != nil {
		return nil, runErr
	}

	return &upgradeResult{
		Output:     output,
		HasChanges: strings.Contains(output, "CHANGES_PUSHED=true"),
		SDKUpdated: strings.Contains(output, "SDK_VERSION_UPDATED=true"),
	}, nil
}

func buildUpgradeScript(params upgradeParams) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	writeGitAuth(&sb, params)

	sb.WriteString("# Ensure git user identity is configured\n")
	sb.WriteString("if ! git config --global user.name > /dev/null 2>&1; then\n")
	sb.WriteString("    git config --global user.name \"autoupdate[bot]\"\n")
	sb.WriteString("fi\n")
	sb.WriteString("if ! git config --global user.email > /dev/null 2>&1; then\n")
	sb.WriteString("    git config --global user.email \"autoupdate[bot]@users.noreply.github.com\"\n")
	sb.WriteString("fi\n\n")

	sb.WriteString("echo \"Cloning repository...\"\n")
	sb.WriteString("git clone --depth=1 --branch \"$DEFAULT_BRANCH\" \"$CLONE_URL\" \"$REPO_DIR\" 2>&1\n")
	sb.WriteString("cd \"$REPO_DIR\"\n\n")

	sb.WriteString("git checkout -b \"$BRANCH_NAME\" 2>&1\n\n")

	writeFvmPinUpdate(&sb)
	writeDartUpgradeCommands(&sb)

	// Copy in the staged changelog: an edited CHANGELOG.md, or a chlog
	// fragment when the target repository uses that format.
	sb.WriteString(support.ChangelogUpdateScript())

	writeCommitAndPush(&sb)

	return sb.String()
}

func writeGitAuth(sb *strings.Builder, params upgradeParams) {
	sb.WriteString(support.GitAuthScript(params.ProviderName))
}

// writeFvmPinUpdate rewrites the .fvmrc pin in the remote flow, where the clone
// happens inside the script and the Go side never sees the worktree. Only the
// "flutter" value is substituted, so a flavors block survives.
func writeFvmPinUpdate(sb *strings.Builder) {
	sb.WriteString("# Update the pinned Flutter SDK when a newer stable was published\n")
	sb.WriteString("if [ -n \"${TARGET_SDK_VERSION:-}\" ] && [ -f \".fvmrc\" ]; then\n")
	sb.WriteString("    echo \"Updating .fvmrc to $TARGET_SDK_VERSION...\"\n")
	sb.WriteString(
		"    sed -E 's|(\"flutter\"[[:space:]]*:[[:space:]]*\")[^\"]*(\")|\\1'\"$TARGET_SDK_VERSION\"'\\2|' " +
			".fvmrc > .fvmrc.tmp && mv .fvmrc.tmp .fvmrc\n",
	)
	sb.WriteString("    echo \"SDK_VERSION_UPDATED=true\"\n")
	sb.WriteString("else\n")
	sb.WriteString("    echo \"SDK_VERSION_UPDATED=false\"\n")
	sb.WriteString("fi\n\n")
}

// writeDartUpgradeCommands runs pub through the project's own toolchain.
//
// --major-versions is what makes this an upgrade rather than a re-resolution: a
// plain `pub upgrade` only rewrites pubspec.lock within the constraints already
// declared, while --major-versions raises those constraints in pubspec.yaml to
// what pub reports as resolvable. Pub applies that rewrite through yaml_edit, so
// the manifest's comments survive it.
func writeDartUpgradeCommands(sb *strings.Builder) {
	sb.WriteString("# Upgrade pub dependencies\n")
	sb.WriteString("PUB=\"${PUB_EXECUTABLE:-dart}\"\n")
	// FVM installs the SDK per project, so when a repository pins one the
	// toolchain on PATH is the wrong one to run.
	sb.WriteString("if [ -f \".fvmrc\" ] && command -v fvm > /dev/null 2>&1; then\n")
	sb.WriteString("    PUB=\"fvm $PUB\"\n")
	sb.WriteString("fi\n")
	sb.WriteString("if [ -f \"pubspec.yaml\" ]; then\n")
	sb.WriteString("    echo \"Running $PUB pub upgrade --major-versions...\"\n")
	sb.WriteString(
		"    $PUB pub upgrade --major-versions 2>&1 || echo \"WARNING: pub upgrade had some errors (continuing anyway)\"\n",
	)
	sb.WriteString("    echo \"Running $PUB pub get...\"\n")
	sb.WriteString("    $PUB pub get 2>&1 || echo \"WARNING: pub get had some errors (continuing anyway)\"\n")
	sb.WriteString("fi\n\n")
}

func writeCommitAndPush(sb *strings.Builder) {
	sb.WriteString("if [ -n \"$(git status --porcelain)\" ]; then\n")
	sb.WriteString("    echo \"Changes detected, committing and pushing...\"\n")
	sb.WriteString("    git add -A\n")
	sb.WriteString("    if [ -n \"${TARGET_SDK_VERSION:-}\" ]; then\n")
	sb.WriteString(
		"        git commit -m \"chore(deps): upgraded Flutter to \\`$TARGET_SDK_VERSION\\` " +
			"and updated all pub dependencies\"\n",
	)
	sb.WriteString("    else\n")
	sb.WriteString("        git commit -m \"chore(deps): updated Dart pub dependencies\"\n")
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
		"PUB_EXECUTABLE="+params.Toolchain,
	)
	if params.SDKVersion != "" {
		env = append(env, "TARGET_SDK_VERSION="+params.SDKVersion)
	}
	env = append(env, params.Changelog.Env()...)
	return env
}

// GeneratePRDescription builds a markdown PR description for a pub dependency
// upgrade. Exported so that the local-mode CLI handler can reuse the format.
func GeneratePRDescription(sdkVersion, toolchain string, sdkUpdated bool) string {
	if toolchain == "" {
		toolchain = toolchainDart
	}

	var sb strings.Builder
	sb.WriteString("## Summary\n\n")
	if sdkUpdated {
		sb.WriteString(
			"This PR upgrades the pinned Flutter SDK to **" + sdkVersion +
				"** and updates all pub dependencies.\n\n",
		)
	} else {
		sb.WriteString("This PR updates all pub dependencies to their latest resolvable versions.\n\n")
	}
	sb.WriteString("### Changes\n\n")
	if sdkUpdated {
		sb.WriteString("- Updated `" + FvmConfigFile + "` to `" + sdkVersion + "`\n")
	}
	sb.WriteString("- Ran `" + toolchain + " pub upgrade --major-versions`, which raises the constraints in " +
		"`pubspec.yaml` to the latest resolvable versions rather than only re-resolving `pubspec.lock`\n")
	sb.WriteString("- Ran `" + toolchain + " pub get` to refresh `pubspec.lock`\n")
	sb.WriteString("\n### Review Checklist\n\n")
	sb.WriteString("- [ ] Verify `" + toolchain + " analyze` passes\n")
	sb.WriteString("- [ ] Verify tests pass\n")
	sb.WriteString("- [ ] Review the constraint changes in `pubspec.yaml` for breaking major bumps\n")
	sb.WriteString("\n---\n")
	sb.WriteString("*This PR was automatically created by [autoupdate](https://github.com/rios0rios0/autoupdate)*\n")
	return sb.String()
}
