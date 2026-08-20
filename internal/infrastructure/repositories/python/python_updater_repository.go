package python

import (
	"context"
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
	langPython "github.com/rios0rios0/langforge/pkg/infrastructure/languages/python"
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

	// Commit/PR messages and changelog entries used across remote and local modes.
	pyCommitMsgDeps      = "chore(deps): updated Python dependencies"
	pyChangelogEntryDeps = "- changed the Python dependencies to their latest versions"

	// Toolchain identifiers. A repository is upgraded either through PDM (when
	// it is PDM-managed) or through plain pip, and the two produce different
	// commands, changelog wording and PR descriptions.
	toolchainPDM = "pdm"
	toolchainPip = "pip"
)

// UpdaterRepository implements repositories.UpdaterRepository for Python dependencies.
// It clones the repository locally, runs pip commands to update
// dependencies, pushes the changes, and creates a PR via the provider API.
type UpdaterRepository struct {
	versionFetcher VersionFetcher
	cmdRunner      cmdrunner.Runner
}

// NewUpdaterRepository creates a new Python updater with default dependencies.
func NewUpdaterRepository() repositories.UpdaterRepository {
	return &UpdaterRepository{
		versionFetcher: NewHTTPPythonVersionFetcher(&http.Client{Timeout: pyVersionTimeout}),
		cmdRunner:      cmdrunner.NewDefaultRunner(),
	}
}

// NewUpdaterRepositoryWithDeps creates a Python updater with injected dependencies (for testing).
func NewUpdaterRepositoryWithDeps(vf VersionFetcher) repositories.UpdaterRepository {
	return &UpdaterRepository{versionFetcher: vf, cmdRunner: cmdrunner.NewDefaultRunner()}
}

func (u *UpdaterRepository) Name() string { return updaterName }

// Detect returns true if the repository has Python marker files (e.g. pyproject.toml, requirements.txt).
func (u *UpdaterRepository) Detect(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) bool {
	found, err := support.DetectRemote(ctx, &langPython.Detector{}, provider, repo)
	if err != nil {
		logger.Warnf("[python] detection error for %s/%s: %v", repo.Organization, repo.Name, err)
		return false
	}
	return found
}

// CreateUpdatePRs clones the repo, upgrades Python dependencies,
// and creates a PR.
func (u *UpdaterRepository) CreateUpdatePRs(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
) ([]entities.PullRequest, error) {
	logger.Infof("[python] Processing %s/%s", repo.Organization, repo.Name)

	latestPyVersion, err := u.versionFetcher.FetchLatestVersion(ctx)
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
	changelog := support.StageRemoteChangelog(ctx, provider, repo, changelogEntries(vCtx))
	defer changelog.Remove()
	project := detectRemoteProject(ctx, provider, repo)

	cloneURL := provider.CloneURL(repo)
	defaultBranch := strings.TrimPrefix(repo.DefaultBranch, "refs/heads/")

	pythonBinary, err := findPythonBinary()
	if err != nil {
		return nil, fmt.Errorf("python binary not found: %w", err)
	}

	result, err := upgradeRepo(ctx, upgradeParams{
		CloneURL:      cloneURL,
		DefaultBranch: defaultBranch,
		BranchName:    vCtx.BranchName,
		PythonVersion: vCtx.LatestVersion,
		AuthToken:     provider.AuthToken(),
		ProviderName:  provider.Name(),
		Changelog:     changelog,
		Project:       project,
		PythonBinary:  pythonBinary,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upgrade: %w", err)
	}

	result.Toolchain = project.Toolchain()

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

	prTitle := pyCommitMsgDeps
	if result.PythonVersionUpdated {
		prTitle = fmt.Sprintf(
			"chore(deps): upgraded Python to `%s` and updated all dependencies",
			vCtx.LatestVersion,
		)
	}
	prDesc := GeneratePRDescription(vCtx.LatestVersion, result.Toolchain, result.PythonVersionUpdated)

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

// ApplyUpdates implements repositories.LocalUpdater. It runs language-specific
// Python upgrade operations on a locally cloned repository, without performing
// any git clone, branch, commit, or push operations.
func (u *UpdaterRepository) ApplyUpdates(
	ctx context.Context,
	repoDir string,
	_ repositories.ProviderRepository,
	repo entities.Repository,
	_ entities.UpdateOptions,
) (*repositories.LocalUpdateResult, error) {
	logger.Infof("[python] Processing local clone of %s/%s", repo.Organization, repo.Name)

	// resolveLocalVersionContext (from local.go) handles fetching + comparison
	vCtx := resolveLocalVersionContext(ctx, repoDir)

	project := detectLocalProject(repoDir)

	pythonBinary, binErr := findPythonBinary()
	if binErr != nil {
		return nil, fmt.Errorf("python binary not found: %w", binErr)
	}

	script := buildBatchPythonScript(project)
	scriptPath := filepath.Join(repoDir, ".autoupdate-upgrade.sh")
	if writeErr := os.WriteFile(scriptPath, []byte(script), scriptFileMode); writeErr != nil {
		return nil, fmt.Errorf("failed to write script: %w", writeErr)
	}
	defer func() { _ = os.Remove(scriptPath) }()

	cmd := exec.CommandContext(ctx, "bash", scriptPath)
	cmd.Dir = repoDir
	env := append(os.Environ(), "PYTHON_BINARY="+pythonBinary)
	if vCtx.LatestVersion != "" {
		env = append(env, "PYTHON_VERSION="+vCtx.LatestVersion)
	}
	cmd.Env = env

	output, cmdErr := cmd.CombinedOutput()
	outputStr := string(output)
	logger.Debugf("[python] Upgrade script output:\n%s", outputStr)

	if cmdErr != nil {
		return nil, fmt.Errorf("upgrade script failed: %w\nOutput:\n%s", cmdErr, outputStr)
	}

	// Remove the script before checking worktree state so it does not
	// appear as an untracked file in the git status check below.
	_ = os.Remove(scriptPath)
	pyVersionUpdated := strings.Contains(outputStr, "PYTHON_VERSION_UPDATED=true")

	// Return early if the upgrade script made no filesystem changes
	if !support.HasUncommittedChanges(ctx, repoDir) {
		logger.Infof("[python] No filesystem changes detected after upgrade script")
		return nil, repositories.ErrNoUpdatesNeeded
	}

	// Record the upgrade in the repository's changelog.
	var entry string
	if pyVersionUpdated {
		entry = fmt.Sprintf(
			"- changed the Python version to `%s` and updated all %s dependencies",
			vCtx.LatestVersion, project.Toolchain(),
		)
	} else {
		entry = pyChangelogEntryDeps
	}
	support.LocalChangelogUpdate(repoDir, []string{entry})

	commitMsg := pyCommitMsgDeps
	prTitle := commitMsg
	if pyVersionUpdated {
		commitMsg = fmt.Sprintf(
			"chore(deps): upgraded Python to `%s` and updated all dependencies",
			vCtx.LatestVersion,
		)
		prTitle = commitMsg
	}

	return &repositories.LocalUpdateResult{
		BranchName:    vCtx.BranchName,
		CommitMessage: commitMsg,
		PRTitle:       prTitle,
		PRDescription: GeneratePRDescription(vCtx.LatestVersion, project.Toolchain(), pyVersionUpdated),
	}, nil
}

// buildBatchPythonScript generates a bash script with only language-specific
// operations (no git clone, branch, commit, or push) for the batch pipeline.
func buildBatchPythonScript(project pythonProject) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	writePythonUpgradeCommands(&sb, upgradeParams{Project: project})
	writeEggInfoGitignore(&sb)
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
	PythonVersion string
	AuthToken     string
	ProviderName  string
	// Changelog is the staged changelog payload the script copies into
	// the clone; an empty value leaves the repository's changelog untouched.
	Changelog support.StagedChangelog
	// Project carries the manifests the repository has and the dependency
	// manager selected from them.
	Project      pythonProject
	PythonBinary string
}

type upgradeResult struct {
	HasChanges           bool
	PythonVersionUpdated bool
	Toolchain            string
	Output               string
}

// pyprojectUsesPDM reports whether a pyproject.toml belongs to a PDM-managed
// project. PDM keeps its own configuration under a [tool.pdm] table, and the
// pdm-backend build backend is only ever declared by PDM projects, so either
// marker identifies the project without needing a TOML parser.
//
// Comments are discarded before matching, so a marker named only in prose does
// not select the PDM upgrade path. The table match is anchored to [tool.pdm]
// and its sub-tables so an unrelated table such as [tool.pdmx] is not mistaken
// for one of PDM's.
func pyprojectUsesPDM(content string) bool {
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(stripTOMLComment(line))
		if trimmed == "" {
			continue
		}
		if trimmed == "[tool.pdm]" || strings.HasPrefix(trimmed, "[tool.pdm.") {
			return true
		}
		if strings.Contains(trimmed, "pdm.backend") || strings.Contains(trimmed, "pdm-backend") {
			return true
		}
	}
	return false
}

// stripTOMLComment removes an inline comment from a TOML line. A '#' carried
// inside a quoted value is part of that value rather than the start of a
// comment, so quoting is tracked while scanning; basic (double-quoted) strings
// additionally honour backslash escapes, while literal (single-quoted) strings
// have none. This is a best-effort scan, which is all the marker matching above
// needs — it never has to interpret the values themselves.
func stripTOMLComment(line string) string {
	var quote rune
	escaped := false

	for i, r := range line {
		switch {
		case escaped:
			escaped = false
		case quote == '"' && r == '\\':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '#':
			return line[:i]
		}
	}

	return line
}

// detectPDMRemote reports which of PDM's markers the remote repository carries.
// A committed pdm.lock settles the toolchain on its own, so the pyproject.toml
// is only read when there is none.
//
// hasPyproject is the caller's already-established answer for that file. A
// provider's HasFile is itself a file fetch, so re-reading a pyproject.toml the
// caller has just found to be absent would spend a second request per
// repository to learn nothing — wasted against the provider's rate limit on
// every requirements.txt-only project, which is the common pip layout. The
// pdm.lock probe still runs, so a stray lock is reported and warned about.
func detectPDMRemote(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	hasPyproject bool,
) pdmMarkers {
	markers := pdmMarkers{lock: provider.HasFile(ctx, repo, "pdm.lock")}

	if markers.lock || !hasPyproject {
		return markers
	}

	content, err := provider.GetFileContent(ctx, repo, "pyproject.toml")
	if err != nil {
		return markers
	}
	markers.declared = pyprojectUsesPDM(content)

	return markers
}

// detectPDMLocal reports which of PDM's markers a checked-out repository
// carries, using the same rules as [detectPDMRemote].
func detectPDMLocal(repoDir string) pdmMarkers {
	markers := pdmMarkers{lock: fileExists(filepath.Join(repoDir, "pdm.lock"))}

	if markers.lock {
		return markers
	}

	content, err := os.ReadFile(filepath.Clean(filepath.Join(repoDir, "pyproject.toml")))
	if err != nil {
		return markers
	}
	markers.declared = pyprojectUsesPDM(string(content))

	return markers
}

// parsePythonVersionFile extracts the Python version from a .python-version
// file content. The file typically contains just a version string like "3.12.8".
func parsePythonVersionFile(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
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
			needsVersionUpgrade = support.IsNewerVersion(currentVersion, latestPyVersion)
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

// changelogEntries renders the Keep a Changelog bullet describing the
// upgrade. The staging helpers turn it into a chlog fragment when the
// target repository uses that format instead.
func changelogEntries(vCtx *versionContext) []string {
	if vCtx.NeedsVersionUpgrade {
		return []string{fmt.Sprintf(
			"- changed the Python version to `%s` and updated all pip dependencies",
			vCtx.LatestVersion,
		)}
	}
	return []string{pyChangelogEntryDeps}
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

	// Keep generated build metadata out of the commit
	writeEggInfoGitignore(&sb)

	// Update Dockerfile python image tags
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

// writePythonVersionPin emits the .python-version rewrite, guarded so the pin
// only ever moves forwards: the feed reports the newest *stable* series, so a
// repository tracking a newer pre-release series is ahead of it and keeps its
// pin rather than being rolled back.
func writePythonVersionPin(sb *strings.Builder) {
	sb.WriteString(support.VersionGuardScript())
	sb.WriteString("# Check and update Python version\n")
	sb.WriteString("PYTHON_VERSION_CHANGED=false\n")
	sb.WriteString("if [ -n \"${PYTHON_VERSION:-}\" ] && [ -f \".python-version\" ]; then\n")
	sb.WriteString("    CURRENT_PY_VERSION=$(head -1 .python-version | tr -d '[:space:]')\n")
	sb.WriteString("    if autoupdate_version_is_newer \"$PYTHON_VERSION\" \"$CURRENT_PY_VERSION\"; then\n")
	sb.WriteString("        echo \"Updating .python-version from $CURRENT_PY_VERSION to $PYTHON_VERSION...\"\n")
	sb.WriteString("        echo \"$PYTHON_VERSION\" > .python-version\n")
	sb.WriteString("        PYTHON_VERSION_CHANGED=true\n")
	sb.WriteString("        echo \"PYTHON_VERSION_UPDATED=true\"\n")
	sb.WriteString("    else\n")
	sb.WriteString(
		"        echo \"Keeping .python-version at $CURRENT_PY_VERSION (not older than $PYTHON_VERSION)\"\n",
	)
	sb.WriteString("        echo \"PYTHON_VERSION_UPDATED=false\"\n")
	sb.WriteString("    fi\n")
	sb.WriteString("else\n")
	sb.WriteString("    echo \"PYTHON_VERSION_UPDATED=false\"\n")
	sb.WriteString("fi\n\n")
}

func writePythonUpgradeCommands(sb *strings.Builder, params upgradeParams) {
	writePythonVersionPin(sb)

	// Create virtual environment and upgrade dependencies
	sb.WriteString("# Create virtual environment for dependency upgrade\n")
	sb.WriteString("VENV_DIR=$(mktemp -d)\n")
	sb.WriteString("\"$PYTHON_BINARY\" -m venv \"$VENV_DIR\"\n")
	sb.WriteString("# shellcheck disable=SC1091\n")
	sb.WriteString("source \"$VENV_DIR/bin/activate\"\n")
	sb.WriteString("pip install --upgrade pip 2>&1 || echo \"WARNING: pip upgrade had some errors\"\n\n")

	// A PDM-managed project is upgraded exclusively through PDM: pip has no
	// view of the PDM lock file, so running both would leave the lock stale
	// while still producing the local-install artefacts pip leaves behind.
	if params.Project.UsesPDM() {
		writePDMUpgradeCommands(sb)
		sb.WriteString("deactivate 2>/dev/null || true\n")
		sb.WriteString("rm -rf \"$VENV_DIR\"\n\n")
		return
	}

	writeManifestSnapshot(sb)

	if params.Project.HasRequirements {
		sb.WriteString("# Upgrade dependencies from requirements.txt\n")
		sb.WriteString("if [ -f \"requirements.txt\" ]; then\n")
		sb.WriteString("    echo \"Installing current requirements...\"\n")
		sb.WriteString("    pip install -r requirements.txt 2>&1 || echo \"WARNING: pip install had some errors\"\n\n")
		sb.WriteString("    echo \"Upgrading all packages...\"\n")
		sb.WriteString(
			"    pip install --upgrade -r requirements.txt 2>&1 || echo \"WARNING: pip upgrade had some errors\"\n\n",
		)
		sb.WriteString("    echo \"Freezing updated requirements...\"\n")
		sb.WriteString("    pip freeze | sed '/@\\s*file:\\/\\//d' > requirements.txt\n")
		sb.WriteString("fi\n\n")
	}

	if params.Project.HasPyproject {
		sb.WriteString("# Upgrade dependencies from pyproject.toml\n")
		sb.WriteString("if [ -f \"pyproject.toml\" ]; then\n")
		sb.WriteString("    echo \"Upgrading pyproject.toml dependencies...\"\n")
		sb.WriteString(
			"    pip install --upgrade . 2>&1 || echo \"WARNING: pip install --upgrade . had some errors\"\n",
		)
		sb.WriteString("    if [ -f \"requirements.txt\" ]; then\n")
		sb.WriteString("        pip freeze | sed '/@\\s*file:\\/\\//d' > requirements.txt\n")
		sb.WriteString("    fi\n")
		sb.WriteString("fi\n\n")
	}

	sb.WriteString("deactivate 2>/dev/null || true\n")
	sb.WriteString("rm -rf \"$VENV_DIR\"\n\n")

	writeManifestRestore(sb)
}

// writeManifestSnapshot records which dependency manifests the repository
// carried before the upgrade ran. It is emitted only on the pip path, and is
// read back by [writeManifestRestore].
//
// The `if` form is deliberate: `[ -f x ] && VAR=true` exits non-zero when the
// file is absent, which under `set -e` would abort the whole script.
func writeManifestSnapshot(sb *strings.Builder) {
	sb.WriteString("# Record the dependency manifests present before the upgrade.\n")
	sb.WriteString("PYPROJECT_EXISTED=false\n")
	sb.WriteString("if [ -f \"pyproject.toml\" ]; then\n")
	sb.WriteString("    PYPROJECT_EXISTED=true\n")
	sb.WriteString("fi\n")
	sb.WriteString("PDM_LOCK_EXISTED=false\n")
	sb.WriteString("if [ -f \"pdm.lock\" ]; then\n")
	sb.WriteString("    PDM_LOCK_EXISTED=true\n")
	sb.WriteString("fi\n\n")
}

// writeManifestRestore discards a dependency manifest the upgrade itself
// created. The dependency manager is chosen from the manifests a repository
// already carries, so an upgrade must never introduce a different one: a
// pyproject.toml or a pdm.lock appearing in a pip/requirements.txt repository
// would migrate it to another package manager inside what was only ever meant
// to be a dependency bump, and the pull request would carry that migration
// without anyone having asked for it.
//
// Only files that were absent before the upgrade are removed, so a manifest the
// repository owns is never touched.
func writeManifestRestore(sb *strings.Builder) {
	sb.WriteString("# Discard any dependency manifest the upgrade itself introduced, so a\n")
	sb.WriteString("# pip-managed repository is never migrated to another package manager.\n")
	sb.WriteString("if [ \"$PYPROJECT_EXISTED\" = \"false\" ] && [ -f \"pyproject.toml\" ]; then\n")
	sb.WriteString(
		"    echo \"WARNING: the upgrade created pyproject.toml in a pip-managed repository, removing it\"\n",
	)
	sb.WriteString("    rm -f \"pyproject.toml\"\n")
	sb.WriteString("fi\n")
	sb.WriteString("if [ \"$PDM_LOCK_EXISTED\" = \"false\" ] && [ -f \"pdm.lock\" ]; then\n")
	sb.WriteString("    echo \"WARNING: the upgrade created pdm.lock in a pip-managed repository, removing it\"\n")
	sb.WriteString("    rm -f \"pdm.lock\"\n")
	sb.WriteString("fi\n\n")
}

// writePDMUpgradeCommands emits the upgrade step for a PDM-managed project.
//
// `--no-sync` keeps the run to a dependency resolution: the updated pdm.lock is
// the only artefact worth committing, and skipping the sync also avoids
// building the project locally — the build is what leaves a *.egg-info
// directory behind for `git add -A` to sweep into the commit.
//
// The `-G :all` form covers every optional-dependency group; projects that
// declare none reject it, so the plain form is retried before giving up.
func writePDMUpgradeCommands(sb *strings.Builder) {
	sb.WriteString("# Upgrade dependencies with PDM (project is PDM-managed)\n")
	sb.WriteString("if ! command -v pdm > /dev/null 2>&1; then\n")
	sb.WriteString("    echo \"Installing PDM into the temporary virtual environment...\"\n")
	sb.WriteString("    pip install --upgrade pdm 2>&1 || echo \"WARNING: PDM installation had some errors\"\n")
	sb.WriteString("fi\n")
	sb.WriteString("echo \"Running pdm update...\"\n")
	sb.WriteString("pdm update --update-all --no-sync -G :all 2>&1 \\\n")
	sb.WriteString("    || pdm update --update-all --no-sync 2>&1 \\\n")
	sb.WriteString("    || echo \"WARNING: pdm update had some errors (continuing anyway)\"\n\n")
}

// writeEggInfoGitignore appends the setuptools build-metadata pattern to
// .gitignore when the upgrade left an *.egg-info directory behind. Those files
// are generated, so without the entry they are untracked-but-not-ignored and
// the `git add -A` further down commits them as though they were a dependency
// change. The entry is only added when such a directory actually exists, so
// repositories that never build the project keep their .gitignore untouched.
func writeEggInfoGitignore(sb *strings.Builder) {
	sb.WriteString("# Ignore setuptools build metadata so it is never committed as a change.\n")
	sb.WriteString("if ls -d ./*.egg-info > /dev/null 2>&1; then\n")
	sb.WriteString("    if ! grep -qE '^\\*\\.egg-info/?$' .gitignore 2>/dev/null; then\n")
	sb.WriteString("        echo \"Adding *.egg-info/ to .gitignore...\"\n")
	// A .gitignore whose last line lacks a trailing newline would otherwise
	// have the new pattern appended onto the end of that line.
	sb.WriteString("        if [ -s .gitignore ] && [ -n \"$(tail -c1 .gitignore)\" ]; then\n")
	sb.WriteString("            echo \"\" >> .gitignore\n")
	sb.WriteString("        fi\n")
	sb.WriteString("        echo \"*.egg-info/\" >> .gitignore\n")
	sb.WriteString("    fi\n")
	sb.WriteString("fi\n\n")
}

func writeDockerfileUpdate(sb *strings.Builder) {
	// Emitted here as well as by the upgrade block, so the fragment carries the
	// comparison it depends on instead of inheriting it from whatever ran first.
	sb.WriteString(support.VersionGuardScript())
	sb.WriteString("# Update Dockerfile python image tags when the Python version was bumped.\n")
	sb.WriteString("if [ \"$PYTHON_VERSION_CHANGED\" = \"true\" ]; then\n")
	sb.WriteString("    echo \"Updating Dockerfile python image tags to $PYTHON_VERSION...\"\n")
	sb.WriteString(
		"    find . -type f -not -path './.git/*' " +
			"\\( -name 'Dockerfile' -o -name 'Dockerfile.*' -o -name '*.Dockerfile' \\) " +
			"-print0 | while IFS= read -r -d '' df; do\n",
	)
	sb.WriteString("        if autoupdate_image_tag_is_older \"$df\" \"python\" \"$PYTHON_VERSION\"; then\n")
	sb.WriteString(
		"            sed \"s|python:[0-9][0-9.]*|python:${PYTHON_VERSION}|g\" \"$df\" > \"$df.tmp\" && mv \"$df.tmp\" \"$df\"\n",
	)
	sb.WriteString("            echo \"  Updated $df\"\n")
	sb.WriteString("        fi\n")
	sb.WriteString("    done\n")
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
	env = append(env, params.Changelog.Env()...)
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
// reuse the same description format. The toolchain ("pdm" or "pip") selects
// the commands and the review target named in the body, so the description
// always reports what the run actually did.
func GeneratePRDescription(pyVersion, toolchain string, pyVersionUpdated bool) string {
	var sb strings.Builder
	sb.WriteString("## Summary\n\n")
	if pyVersionUpdated {
		sb.WriteString(
			"This PR upgrades the Python version to **" + pyVersion +
				"** and updates all " + toolchain + " dependencies.\n\n",
		)
	} else {
		sb.WriteString(
			"This PR updates all Python " + toolchain + " dependencies to their latest versions.\n\n",
		)
	}
	sb.WriteString("### Changes\n\n")
	if pyVersionUpdated {
		sb.WriteString("- Updated `.python-version` to `" + pyVersion + "`\n")
	}
	if toolchain == toolchainPDM {
		sb.WriteString("- Ran `pdm update --update-all --no-sync` to resolve the latest dependency versions\n")
	} else {
		sb.WriteString("- Ran `pip install --upgrade -r requirements.txt` to update all dependencies\n")
		sb.WriteString("- Ran `pip freeze` to capture updated versions\n")
	}
	sb.WriteString("\n### Review Checklist\n\n")
	sb.WriteString("- [ ] Verify build passes\n")
	sb.WriteString("- [ ] Verify tests pass\n")
	if toolchain == toolchainPDM {
		sb.WriteString("- [ ] Review dependency changes in `pdm.lock`\n")
	} else {
		sb.WriteString("- [ ] Review dependency changes in `requirements.txt`\n")
	}
	sb.WriteString("\n---\n")
	sb.WriteString("*This PR was automatically created by [autoupdate](https://github.com/rios0rios0/autoupdate)*\n")
	return sb.String()
}
