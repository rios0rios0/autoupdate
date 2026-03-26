//go:build unit

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
	return buildLocalEnv(localUpgradeParams(params))
}

// LocalUpgradeParamsExported is exported for testing.
type LocalUpgradeParamsExported = localUpgradeParams

// NewUpdaterRepositoryForTest creates an updater with injected dependencies.
func NewUpdaterRepositoryForTest(vf VersionFetcher, runner ...cmdrunner.Runner) *UpdaterRepository {
	r := cmdrunner.Runner(cmdrunner.NewDefaultRunner())
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
func BuildBatchPythonScript(hasRequirements, hasPyproject bool) string {
	return buildBatchPythonScript(hasRequirements, hasPyproject)
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

// PrepareChangelog is exported for testing.
func PrepareChangelog(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	vCtx *VersionContext,
) string {
	return prepareChangelog(ctx, provider, repo, vCtx)
}

// LogDryRun is exported for testing.
func LogDryRun(vCtx *VersionContext, repo entities.Repository) {
	logDryRun(vCtx, repo)
}

// OpenPullRequest is exported for testing.
func OpenPullRequest(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
	vCtx *VersionContext,
	result *UpgradeResultExported,
) ([]entities.PullRequest, error) {
	return openPullRequest(ctx, provider, repo, opts, vCtx, result)
}

// WriteDockerfileUpdate is exported for testing.
func WriteDockerfileUpdate(sb *strings.Builder) {
	writeDockerfileUpdate(sb)
}

// WriteChangelogUpdate is exported for testing.
func WriteChangelogUpdate(sb *strings.Builder) {
	writeChangelogUpdate(sb)
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
	return handleDryRun(vCtx, repoDir)
}

// PrepareLocalChangelog is exported for testing.
func PrepareLocalChangelog(repoDir string, vCtx *VersionContext) string {
	return prepareLocalChangelog(repoDir, vCtx)
}

// FindPythonBinary is exported for testing.
func FindPythonBinary() (string, error) {
	return findPythonBinary()
}
