//go:build unit

package java

import (
	"context"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
)

// ParseJavaVersionFile is exported for testing.
func ParseJavaVersionFile(content string) string {
	return parseJavaVersionFile(content)
}

// ExtractMajorVersion is exported for testing.
func ExtractMajorVersion(version string) string {
	return extractMajorVersion(version)
}

// IsLTSRelease is exported for testing.
func IsLTSRelease(release JavaRelease) bool {
	return isLTSRelease(release)
}

// IsActiveRelease is exported for testing.
func IsActiveRelease(release JavaRelease) bool {
	return isActiveRelease(release)
}

// JavaRelease is exported for testing.
type JavaRelease = javaRelease

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

// BuildBatchJavaScript is exported for testing.
func BuildBatchJavaScript(buildSys string) string {
	return buildBatchJavaScript(buildSys)
}

// WriteGitAuth is exported for testing.
func WriteGitAuth(sb *strings.Builder, params UpgradeParamsExported) {
	writeGitAuth(sb, params)
}

// WriteJavaUpgradeCommands is exported for testing.
func WriteJavaUpgradeCommands(sb *strings.Builder, params UpgradeParamsExported) {
	writeJavaUpgradeCommands(sb, params)
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
	buildSys string,
) ([]entities.PullRequest, error) {
	return openPullRequest(ctx, provider, repo, opts, vCtx, result, buildSys)
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

// DetectLocalBuildSystem is exported for testing.
func DetectLocalBuildSystem(repoDir string) string {
	return detectLocalBuildSystem(repoDir)
}
