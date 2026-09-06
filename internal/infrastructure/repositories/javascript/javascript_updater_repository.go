package javascript

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
	"github.com/rios0rios0/autoupdate/internal/support"
	langNode "github.com/rios0rios0/langforge/pkg/infrastructure/languages/node"
)

const (
	updaterName = "javascript"
	// runtimeName is how the runtime is spelled in log lines, commit subjects
	// and pull request text.
	runtimeName        = "Node.js"
	nodeVersionTimeout = 15 * time.Second
	scriptFileMode     = 0o700

	// nodeVersionMarker is the variable the upgrade script spells its
	// "the version pin moved" marker after.
	nodeVersionMarker = "NODE_VERSION"

	// Package manager identifiers.
	pkgMgrPnpm = "pnpm"
	pkgMgrYarn = "yarn"
	pkgMgrNpm  = "npm"

	// Branch name patterns for JavaScript/Node.js updates.
	branchNodeVersionFmt = "chore/upgrade-node-%s"
	branchJSDepsFmt      = "chore/upgrade-js-deps"

	// Commit/PR messages and changelog entries used across remote and local modes.
	jsCommitMsgDeps      = "chore(deps): updated JavaScript dependencies"
	jsChangelogEntryDeps = "- changed the JavaScript dependencies to their latest versions"
)

// defaultRunner is the package-level command runner for remote-mode functions.
var defaultRunner cmdrunner.Runner = cmdrunner.NewDefaultRunner() //nolint:gochecknoglobals // test override

// UpdaterRepository implements repositories.UpdaterRepository for JavaScript/Node.js dependencies.
// It clones the repository locally, runs the appropriate package manager
// to update dependencies, pushes the changes, and creates a PR via the
// provider API.
type UpdaterRepository struct {
	versionFetcher VersionFetcher
	cmdRunner      cmdrunner.Runner
}

// NewUpdaterRepository creates a new JavaScript updater with default dependencies.
func NewUpdaterRepository() repositories.UpdaterRepository {
	return &UpdaterRepository{
		versionFetcher: NewHTTPNodeVersionFetcher(&http.Client{Timeout: nodeVersionTimeout}),
		cmdRunner:      cmdrunner.NewDefaultRunner(),
	}
}

// NewUpdaterRepositoryWithDeps creates a JavaScript updater with injected dependencies (for testing).
func NewUpdaterRepositoryWithDeps(vf VersionFetcher) repositories.UpdaterRepository {
	return &UpdaterRepository{versionFetcher: vf, cmdRunner: cmdrunner.NewDefaultRunner()}
}

func (u *UpdaterRepository) Name() string { return updaterName }

// Detect returns true if the repository has Node/JS marker files (e.g. package.json, tsconfig.json).
func (u *UpdaterRepository) Detect(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) bool {
	found, err := support.DetectRemote(ctx, &langNode.Detector{}, provider, repo)
	if err != nil {
		logger.Warnf("[javascript] detection error for %s/%s: %v", repo.Organization, repo.Name, err)
		return false
	}
	return found
}

// CreateUpdatePRs clones the repo, upgrades Node.js dependencies,
// and creates a PR.
func (u *UpdaterRepository) CreateUpdatePRs(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
) ([]entities.PullRequest, error) {
	logger.Infof("[javascript] Processing %s/%s", repo.Organization, repo.Name)

	latestNodeVersion := support.LatestVersion(
		support.VersionFeed{LogPrefix: updaterName, Runtime: runtimeName, Release: "Node.js LTS"},
		func() (string, error) { return u.versionFetcher.FetchLatestVersion(ctx) },
	)

	vCtx := resolveVersionContext(ctx, provider, repo, latestNodeVersion)

	return support.RunRemoteUpgradeRun(ctx, provider, repo, opts, support.RemoteUpgradeRun{
		LogPrefix:  updaterName,
		BranchName: vCtx.BranchName,
		DryRun:     func() { logDryRun(vCtx, repo) },
		Changelog:  changelogEntries(vCtx),
		Upgrade: func(ctx context.Context, target support.CloneTarget) (support.UpgradeOutcome, error) {
			pkgMgr := detectPackageManager(ctx, provider, repo)
			return cloneAndUpgrade(ctx, vCtx, target, pkgMgr, opts.AllowMajorUpdates)
		},
	})
}

// logDryRun logs what would happen without actually performing the upgrade.
func logDryRun(vCtx *versionContext, repo entities.Repository) {
	support.LogRemoteDryRun(updaterName, repo, support.DryRunPlan{
		Runtime:         runtimeName,
		Version:         vCtx.LatestVersion,
		UpgradesVersion: vCtx.NeedsVersionUpgrade,
		Dependencies:    "JavaScript dependencies",
	})
}

// cloneAndUpgrade clones the repository into target, runs the upgrade script,
// and describes the pull request the upgrade earned.
func cloneAndUpgrade(
	ctx context.Context,
	vCtx *versionContext,
	target support.CloneTarget,
	pkgMgr string,
	allowMajorUpdates bool,
) (support.UpgradeOutcome, error) {
	result, err := upgradeRepo(ctx, upgradeParams{
		CloneTarget:    target,
		NodeVersion:    vCtx.LatestVersion,
		PackageManager: pkgMgr,

		AllowMajorUpdates: allowMajorUpdates,
	})
	if err != nil {
		return support.UpgradeOutcome{}, fmt.Errorf("failed to upgrade: %w", err)
	}

	return support.UpgradeOutcome{
		Pushed:      result.HasChanges,
		Title:       upgradeSubject(vCtx.LatestVersion, result.NodeVersionUpdated),
		Description: GeneratePRDescription(vCtx.LatestVersion, pkgMgr, result.NodeVersionUpdated),
	}, nil
}

// upgradeSubject is the one-line summary of what the run changed, used as both
// the commit subject and the pull request title.
func upgradeSubject(nodeVersion string, nodeVersionUpdated bool) string {
	if nodeVersionUpdated {
		return fmt.Sprintf(
			"chore(deps): upgraded Node.js to `%s` and updated all dependencies",
			nodeVersion,
		)
	}

	return jsCommitMsgDeps
}

// ApplyUpdates implements repositories.LocalUpdater. It runs language-specific
// JavaScript upgrade operations on a locally cloned repository, without
// performing any git clone, branch, commit, or push operations.
func (u *UpdaterRepository) ApplyUpdates(
	ctx context.Context,
	repoDir string,
	_ repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
) (*repositories.LocalUpdateResult, error) {
	logger.Infof("[javascript] Processing local clone of %s/%s", repo.Organization, repo.Name)

	// resolveLocalVersionContext (from local.go) handles fetching + comparison
	vCtx := resolveLocalVersionContext(ctx, repoDir)
	pkgMgr := detectLocalPackageManager(repoDir)

	env := append(os.Environ(), "PACKAGE_MANAGER="+pkgMgr)
	if vCtx.LatestVersion != "" {
		env = append(env, "NODE_VERSION="+vCtx.LatestVersion)
	}

	outputStr, runErr := cmdrunner.RunScript(ctx, u.cmdRunner, cmdrunner.ScriptRun{
		Body:        buildBatchJSScript(opts.AllowMajorUpdates),
		TempPattern: "autoupdate-js-local-*",
		Dir:         repoDir,
		Env:         env,
		LogPrefix:   updaterName,
		Verbose:     true,
	})
	if runErr != nil {
		return nil, runErr
	}

	nodeVersionUpdated := strings.Contains(outputStr, nodeVersionMarker+"_UPDATED=true")

	// Return early if the upgrade script made no filesystem changes
	if !support.HasUncommittedChanges(ctx, repoDir) {
		logger.Infof("[javascript] No filesystem changes detected after upgrade script")
		return nil, repositories.ErrNoUpdatesNeeded
	}

	// Skip when the only lockfile change is a cosmetic project-version sync
	// (e.g., npm update syncing package-lock.json "version" to match package.json).
	if hasOnlyLockfileVersionChanges(ctx, repoDir) {
		logger.Infof(
			"[javascript] Only cosmetic lockfile version changes detected (project version sync), skipping",
		)
		// This flow writes the changelog itself, after this check, so there is
		// no staged payload to undo.
		revertWorkingTreeChanges(ctx, repoDir, support.StagedChangelog{})
		return nil, repositories.ErrNoUpdatesNeeded
	}
	logger.Infof("[javascript] Filesystem changes detected, proceeding with commit")

	// Record the upgrade in the repository's changelog.
	var entry string
	if nodeVersionUpdated {
		entry = fmt.Sprintf(
			"- changed the Node.js version to `%s` and updated all JavaScript dependencies",
			vCtx.LatestVersion,
		)
	} else {
		entry = jsChangelogEntryDeps
	}
	support.LocalChangelogUpdate(repoDir, []string{entry})

	commitMsg := upgradeSubject(vCtx.LatestVersion, nodeVersionUpdated)

	return &repositories.LocalUpdateResult{
		BranchName:    vCtx.BranchName,
		CommitMessage: commitMsg,
		PRTitle:       commitMsg,
		PRDescription: GeneratePRDescription(vCtx.LatestVersion, pkgMgr, nodeVersionUpdated),
	}, nil
}

// buildBatchJSScript generates a bash script with only language-specific
// operations (no git clone, branch, commit, or push) for the batch pipeline.
func buildBatchJSScript(allowMajorUpdates bool) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	writeJSUpgradeCommands(&sb, upgradeParams{ //nolint:exhaustruct // only this field reaches the script
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

	NodeVersion    string
	PackageManager string // "npm", "yarn", or "pnpm"
	// AllowMajorUpdates raises the ranges declared in package.json rather than
	// re-resolving inside them. The zero value is the restrictive case; resolve
	// it with entities.MajorUpdatesAllowed at the call site.
	AllowMajorUpdates bool
}

type upgradeResult struct {
	HasChanges         bool
	NodeVersionUpdated bool
	Output             string
}

// --- Node.js version types and helpers ---

type nodeRelease struct {
	Version string `json:"version"`
	LTS     any    `json:"lts"` // false or string like "Jod"
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
	for line := range strings.SplitSeq(content, "\n") {
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
	provider repositories.ProviderRepository,
	repo entities.Repository,
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
	provider repositories.ProviderRepository,
	repo entities.Repository,
	latestNodeVersion string,
) *versionContext {
	needsVersionUpgrade := false

	if latestNodeVersion != "" {
		currentVersion := readCurrentNodeVersion(ctx, provider, repo)
		if currentVersion != "" {
			needsVersionUpgrade = support.IsNewerVersion(currentVersion, latestNodeVersion)
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
	provider repositories.ProviderRepository,
	repo entities.Repository,
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

// changelogEntries renders the Keep a Changelog bullet describing the
// upgrade. The staging helpers turn it into a chlog fragment when the
// target repository uses that format instead.
func changelogEntries(vCtx *versionContext) []string {
	if vCtx.NeedsVersionUpgrade {
		return []string{fmt.Sprintf(
			"- changed the Node.js version to `%s` and updated all JavaScript dependencies",
			vCtx.LatestVersion,
		)}
	}
	return []string{jsChangelogEntryDeps}
}

// --- clone + upgrade ---

func upgradeRepo(
	ctx context.Context,
	params upgradeParams,
) (*upgradeResult, error) {
	run, err := cmdrunner.RunUpgradeScript(ctx, defaultRunner, cmdrunner.UpgradeScriptRun{
		VersionMarker: nodeVersionMarker,
		Body:          buildUpgradeScript(params, ""),
		TempPattern:   "autoupdate-js-*",
		Env:           func(repoDir string) []string { return buildEnv(params, repoDir) },
		Secrets:       []string{params.AuthToken},
	})
	if err != nil {
		return nil, err
	}

	return &upgradeResult{
		HasChanges:         run.HasChanges,
		NodeVersionUpdated: run.VersionUpdated,
		Output:             run.Output,
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

	// JavaScript upgrade commands
	writeJSUpgradeCommands(&sb, params)

	// Update Dockerfile node image tags
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

// writeJSUpgradeCommands emits the upgrade for whichever package manager the
// repository uses.
//
// The plain forms -- `npm update`, `yarn upgrade`, `pnpm update` -- all resolve
// *inside* the ranges package.json already declares, so on their own this
// updater could never cross a major however `allow_major_updates` was set. Each
// manager needs a different mechanism to raise the ranges themselves, and npm
// has no built-in one at all:
//
//   - pnpm: `--latest` ignores the declared range and writes the newest.
//   - yarn: Berry's `up` and Classic's `upgrade --latest` differ, and nothing
//     here knows which is installed, so the script asks at run time.
//   - npm: `npm-check-updates` is the conventional answer. It is fetched with
//     `npx --yes`, and a failure degrades to the plain `npm update` below rather
//     than aborting -- an offline runner should still get the safe upgrade.
func writeJSUpgradeCommands(sb *strings.Builder, params upgradeParams) {
	// Update .nvmrc / .node-version when the release feed is genuinely ahead of
	// the pin. The feed reports the newest *LTS* line, so a repository tracking
	// a newer current release is ahead of it, and rewriting the pin would move
	// the project backwards under a pull request titled as an upgrade.
	sb.WriteString(support.VersionGuardScript())
	sb.WriteString("# Check and update Node.js version\n")
	sb.WriteString("NODE_VERSION_CHANGED=false\n")
	sb.WriteString("if [ -n \"${NODE_VERSION:-}\" ]; then\n")
	sb.WriteString("    for VERSION_FILE in .nvmrc .node-version; do\n")
	sb.WriteString("        if [ -f \"$VERSION_FILE\" ]; then\n")
	sb.WriteString("            CURRENT_NODE_VERSION=$(head -1 \"$VERSION_FILE\" | tr -d '[:space:]' | sed 's/^v//')\n")
	sb.WriteString(
		"            if autoupdate_version_is_newer \"$NODE_VERSION\" \"$CURRENT_NODE_VERSION\"; then\n",
	)
	sb.WriteString("                echo \"Updating $VERSION_FILE from $CURRENT_NODE_VERSION to $NODE_VERSION...\"\n")
	sb.WriteString("                echo \"$NODE_VERSION\" > \"$VERSION_FILE\"\n")
	sb.WriteString("                NODE_VERSION_CHANGED=true\n")
	sb.WriteString("                echo \"NODE_VERSION_UPDATED=true\"\n")
	sb.WriteString("            else\n")
	sb.WriteString(
		"                echo \"Keeping $VERSION_FILE at $CURRENT_NODE_VERSION (not older than $NODE_VERSION)\"\n",
	)
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
	if params.AllowMajorUpdates {
		writeJSMajorUpgradeCommands(sb)

		return
	}

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

// writeJSMajorUpgradeCommands emits the range-raising variant used when
// allow_major_updates is on, which is the default.
func writeJSMajorUpgradeCommands(sb *strings.Builder) {
	sb.WriteString("case \"$PACKAGE_MANAGER\" in\n")
	sb.WriteString("    pnpm)\n")
	sb.WriteString("        echo \"Running pnpm update --latest...\"\n")
	sb.WriteString(
		"        pnpm update --latest 2>&1 || " +
			"echo \"WARNING: pnpm update --latest had some errors (continuing anyway)\"\n",
	)
	sb.WriteString("        ;;\n")
	sb.WriteString("    yarn)\n")
	// Berry replaced `upgrade` with `up`, and only Classic understands --latest,
	// so the installed major decides. `yarn --version` is the one thing both
	// answer the same way.
	sb.WriteString("        YARN_MAJOR=\"$(yarn --version 2>/dev/null | cut -d. -f1)\"\n")
	sb.WriteString("        if [ \"$YARN_MAJOR\" = \"1\" ]; then\n")
	sb.WriteString("            echo \"Running yarn upgrade --latest...\"\n")
	sb.WriteString(
		"            yarn upgrade --latest 2>&1 || " +
			"echo \"WARNING: yarn upgrade --latest had some errors (continuing anyway)\"\n",
	)
	sb.WriteString("        else\n")
	sb.WriteString("            echo \"Running yarn up '*'...\"\n")
	sb.WriteString(
		"            yarn up '*' 2>&1 || " +
			"echo \"WARNING: yarn up had some errors (continuing anyway)\"\n",
	)
	sb.WriteString("        fi\n")
	sb.WriteString("        ;;\n")
	sb.WriteString("    *)\n")
	// ncu rewrites the ranges in package.json; npm install then refreshes the
	// lockfile against them. When ncu cannot be fetched the plain update still
	// runs, so the result is a smaller upgrade rather than none.
	sb.WriteString("        echo \"Raising package.json ranges with npm-check-updates...\"\n")
	sb.WriteString(
		"        npx --yes npm-check-updates -u 2>&1 || " +
			"echo \"WARNING: npm-check-updates unavailable, falling back to npm update\"\n",
	)
	sb.WriteString("        echo \"Running npm update...\"\n")
	sb.WriteString(
		"        npm update 2>&1 || " +
			"echo \"WARNING: npm update had some errors (continuing anyway)\"\n",
	)
	sb.WriteString("        npm install 2>&1 || " +
		"echo \"WARNING: npm install had some errors (continuing anyway)\"\n")
	sb.WriteString("        ;;\n")
	sb.WriteString("esac\n\n")
}

func writeDockerfileUpdate(sb *strings.Builder) {
	sb.WriteString(support.DockerfileTagUpdateScript(support.DockerfileTagUpdate{
		ChangedVar: "NODE_VERSION_CHANGED",
		VersionVar: "NODE_VERSION",
		Subject:    runtimeName,
		Images:     []support.DockerfileImage{{Name: "node"}},
	}))
}

func writeCommitAndPush(sb *strings.Builder) {
	// The lockfile guard runs before anything is staged: `npm update` may sync
	// the root "version" field in package-lock.json to match package.json even
	// when no dependency version changed, and that alone is not worth a pull
	// request.
	var guard strings.Builder
	writeLockfileOnlyCheck(&guard)

	sb.WriteString(support.CommitAndPushScript(support.CommitAndPush{
		Guard:        guard.String(),
		UpgradedWhen: `[ "$NODE_VERSION_CHANGED" = "true" ]`,
		UpgradeMessage: "chore(deps): upgraded Node.js to `$NODE_VERSION` " +
			"and updated all dependencies",
		DepsMessage: jsCommitMsgDeps,
	}))
}

// writeLockfileOnlyCheck emits a bash guard that reverts and exits early
// when the only change is a package-lock.json project-version sync.
//
// The comparison is delegated to Node rather than done with grep: it strips
// only the root "version" and packages[""]["version"] and compares the rest,
// where a grep-based filter would also drop the dependency "version" fields
// this guard exists to notice. It mirrors isPackageLockOnlyVersionSync, which
// makes the same decision in Go for the local flow.
//
// CHANGELOG.md is filtered out of the changed-file list because the changelog
// copy runs before this guard, and its presence would otherwise hide a
// lockfile-only change.
func writeLockfileOnlyCheck(sb *strings.Builder) {
	sb.WriteString(`    # Skip cosmetic lockfile-only version sync
    CHANGED_FILES=$(git diff --name-only)
    SIGNIFICANT_FILES=$(echo "$CHANGED_FILES" | grep -v '^` + support.ChangelogFileName + `$')
    if [ "$SIGNIFICANT_FILES" = "package-lock.json" ]; then
        if node -e 'const fs=require("fs"),{execSync:e}=require("child_process");` +
		`try{const h=JSON.parse(e("git show HEAD:package-lock.json",{encoding:"utf8"})),` +
		`c=JSON.parse(fs.readFileSync("package-lock.json","utf8"));` +
		`delete h.version;delete c.version;` +
		`if(h.packages&&h.packages[""])delete h.packages[""].version;` +
		`if(c.packages&&c.packages[""])delete c.packages[""].version;` +
		`process.exit(JSON.stringify(h)===JSON.stringify(c)?0:1)}catch(x){process.exit(1)}' 2>/dev/null; then
            echo "Only cosmetic lockfile version changes detected, skipping."
            git checkout -- .
            echo "CHANGES_PUSHED=false"
            exit 0
        fi
    fi

`)
}

func buildEnv(params upgradeParams, repoDir string) []string {
	env := append(support.CloneEnv(params.CloneTarget, repoDir),
		"PACKAGE_MANAGER="+params.PackageManager,
	)
	if params.NodeVersion != "" {
		env = append(env, "NODE_VERSION="+params.NodeVersion)
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
