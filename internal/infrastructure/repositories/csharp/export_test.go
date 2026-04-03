//go:build unit

package csharp

import (
	"context"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
)

// ParseGlobalJSON is exported for testing.
func ParseGlobalJSON(content string) string {
	return parseGlobalJSON(content)
}

// IsActiveRelease is exported for testing.
func IsActiveRelease(release DotnetRelease) bool {
	return isActiveRelease(release)
}

// DotnetRelease is exported for testing.
type DotnetRelease = dotnetRelease

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

// BuildBatchDotnetScript is exported for testing.
func BuildBatchDotnetScript() string {
	return buildBatchDotnetScript()
}

// WriteGitAuth is exported for testing.
func WriteGitAuth(sb *strings.Builder, params UpgradeParamsExported) {
	writeGitAuth(sb, params)
}

// WriteDotnetUpgradeCommands is exported for testing.
func WriteDotnetUpgradeCommands(sb *strings.Builder) {
	writeDotnetUpgradeCommands(sb)
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

// FindDotnetBinary is exported for testing.
func FindDotnetBinary() (string, error) {
	return findDotnetBinary()
}

// SetDefaultRunner overrides the package-level command runner for testing.
func SetDefaultRunner(r cmdrunner.Runner) func() {
	old := defaultRunner
	defaultRunner = r
	return func() { defaultRunner = old }
}

// ResolveLocalVersionContext is exported for testing.
func ResolveLocalVersionContext(ctx context.Context, repoDir string) *versionContext {
	return resolveLocalVersionContext(ctx, repoDir)
}
