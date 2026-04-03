//go:build unit

package ruby

import (
	"context"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
)

// ParseRubyVersionFile is exported for testing.
func ParseRubyVersionFile(content string) string {
	return parseRubyVersionFile(content)
}

// IsActiveRelease is exported for testing.
func IsActiveRelease(release RubyRelease) bool {
	return isActiveRelease(release)
}

// RubyRelease is exported for testing.
type RubyRelease = rubyRelease

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

// BuildBatchRubyScript is exported for testing.
func BuildBatchRubyScript() string {
	return buildBatchRubyScript()
}

// WriteGitAuth is exported for testing.
func WriteGitAuth(sb *strings.Builder, params UpgradeParamsExported) {
	writeGitAuth(sb, params)
}

// WriteRubyUpgradeCommands is exported for testing.
func WriteRubyUpgradeCommands(sb *strings.Builder) {
	writeRubyUpgradeCommands(sb)
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
	return runLanguageUpgradeScript(ctx, repoDir, vCtx, opts)
}
