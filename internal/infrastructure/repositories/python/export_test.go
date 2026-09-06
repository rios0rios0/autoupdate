package python

import (
	"context"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
)

// ParsePythonVersionFile is exported for testing.
func ParsePythonVersionFile(content string) string {
	return parsePythonVersionFile(content)
}

// IsActiveRelease is exported for testing.
func IsActiveRelease(release PythonRelease) bool {
	return isActiveRelease(release)
}

// PythonRelease is exported for testing.
type PythonRelease = pythonRelease

// ResolveVersionContext is exported for testing.
func ResolveVersionContext(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	latestVersion string,
) *versionContext {
	return resolveVersionContext(ctx, provider, repo, latestVersion)
}

// VersionContext is exported for testing.
type VersionContext = versionContext

// BuildLocalEnv is exported for testing.
func BuildLocalEnv(params LocalUpgradeParamsExported) []string {
	return buildLocalEnv(params)
}

// LocalUpgradeParamsExported is exported for testing.
type LocalUpgradeParamsExported = localUpgradeParams

// NewUpdaterRepositoryForTest creates an updater with injected dependencies.
func NewUpdaterRepositoryForTest(vf VersionFetcher, runner ...cmdrunner.Runner) *UpdaterRepository {
	r := cmdrunner.NewDefaultRunner()
	if len(runner) > 0 {
		r = runner[0]
	}
	return &UpdaterRepository{versionFetcher: vf, cmdRunner: r}
}

// UpgradeParamsExported is exported for testing.
type UpgradeParamsExported = upgradeParams

// UpgradeResultExported is exported for testing.
type UpgradeResultExported = upgradeResult

// BuildUpgradeScript is exported for testing.
func BuildUpgradeScript(params UpgradeParamsExported, repoDir string) string {
	return buildUpgradeScript(params, repoDir)
}

// BuildBatchPythonScript is exported for testing.
func BuildBatchPythonScript(
	hasRequirements, hasPyproject, hasPDMLock, pyprojectDeclaresPDM, allowMajorUpdates bool,
) string {
	return buildBatchPythonScript(
		NewPythonProject(hasRequirements, hasPyproject, hasPDMLock, pyprojectDeclaresPDM),
		allowMajorUpdates,
	)
}

// PythonProject is exported for testing.
type PythonProject = pythonProject

// NewPythonProject is exported for testing. Tests reach the dependency manager
// decision through the same constructor production code uses, so a test cannot
// express a combination the updater itself could never produce. The two PDM
// markers are passed separately because they do not carry the same weight: a
// committed lock file selects PDM, a pyproject.toml declaring it does not.
func NewPythonProject(hasRequirements, hasPyproject, hasPDMLock, pyprojectDeclaresPDM bool) PythonProject {
	return newPythonProject(
		hasRequirements,
		hasPyproject,
		pdmMarkers{lock: hasPDMLock, declared: pyprojectDeclaresPDM},
	)
}

// DetectLocalProject is exported for testing.
func DetectLocalProject(repoDir string) PythonProject {
	return detectLocalProject(repoDir)
}

// DetectRemoteProject is exported for testing.
func DetectRemoteProject(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) PythonProject {
	return detectRemoteProject(ctx, provider, repo)
}

// PyprojectUsesPDM is exported for testing.
func PyprojectUsesPDM(content string) bool {
	return pyprojectUsesPDM(content)
}

// HasPDMLocal is exported for testing. It reports whether any PDM marker was
// found, which is what the detection probes answer; which marker it was, and
// what that means for the toolchain, is [NewPythonProject]'s decision.
func HasPDMLocal(repoDir string) bool {
	return detectPDMLocal(repoDir).any()
}

// HasPDMRemote is exported for testing, with the same meaning as [HasPDMLocal].
func HasPDMRemote(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	hasPyproject bool,
) bool {
	return detectPDMRemote(ctx, provider, repo, hasPyproject).any()
}

// WriteManifestSnapshot is exported for testing.
func WriteManifestSnapshot(sb *strings.Builder) {
	writeManifestSnapshot(sb)
}

// WriteManifestRestore is exported for testing.
func WriteManifestRestore(sb *strings.Builder) {
	writeManifestRestore(sb)
}

// WriteEggInfoGitignore is exported for testing.
func WriteEggInfoGitignore(sb *strings.Builder) {
	writeEggInfoGitignore(sb)
}

// WriteGitAuth is exported for testing.
func WriteGitAuth(sb *strings.Builder, params UpgradeParamsExported) {
	writeGitAuth(sb, params)
}

// WritePythonUpgradeCommands is exported for testing.
func WritePythonUpgradeCommands(sb *strings.Builder, params UpgradeParamsExported) {
	writePythonUpgradeCommands(sb, params)
}

// BuildEnv is exported for testing.
func BuildEnv(params UpgradeParamsExported, repoDir string) []string {
	return buildEnv(params, repoDir)
}

// ChangelogEntries is exported for testing.
func ChangelogEntries(vCtx *VersionContext) []string {
	return changelogEntries(vCtx)
}

// LogDryRun is exported for testing.
func LogDryRun(vCtx *VersionContext, repo entities.Repository) {
	logDryRun(vCtx, repo)
}

// UpgradeSubject is exported for testing.
func UpgradeSubject(pyVersion string, pyVersionUpdated bool) string {
	return upgradeSubject(pyVersion, pyVersionUpdated)
}

// WriteDockerfileUpdate is exported for testing.
func WriteDockerfileUpdate(sb *strings.Builder) {
	writeDockerfileUpdate(sb)
}

// WriteCommitAndPush is exported for testing.
func WriteCommitAndPush(sb *strings.Builder) {
	writeCommitAndPush(sb)
}

// BuildLocalUpgradeScript is exported for testing.
func BuildLocalUpgradeScript(params LocalUpgradeParamsExported) string {
	return buildLocalUpgradeScript(params)
}

// WriteLocalAuth is exported for testing.
func WriteLocalAuth(sb *strings.Builder, params LocalUpgradeParamsExported) {
	writeLocalAuth(sb, params)
}

// HandleDryRun is exported for testing.
func HandleDryRun(vCtx *VersionContext, repoDir string) *LocalResult {
	return handleDryRun(vCtx, repoDir, detectLocalProject(repoDir))
}

// FindPythonBinary is exported for testing.
func FindPythonBinary() (string, error) {
	return findPythonBinary()
}

// SetLocalCmdRunner overrides the package-level local command runner for testing.
func SetLocalCmdRunner(r cmdrunner.Runner) func() {
	old := localCmdRunner
	localCmdRunner = r
	return func() { localCmdRunner = old }
}

// RunLanguageUpgradeScript is exported for testing the local upgrade script execution.
func RunLanguageUpgradeScript(
	ctx context.Context,
	repoDir string,
	vCtx *versionContext,
	opts LocalUpgradeOptions,
) (string, error) {
	return runLanguageUpgradeScript(ctx, repoDir, vCtx, detectLocalProject(repoDir), opts)
}
