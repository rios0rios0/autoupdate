package golang

import (
	"context"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
)

// ParseGoDirective is exported for testing.
func ParseGoDirective(content string) string {
	return parseGoDirective(content)
}

// ModuleDirsFromPaths is exported for testing.
func ModuleDirsFromPaths(paths []string) []string {
	return moduleDirsFromPaths(paths)
}

// DiscoverLocalModuleDirs is exported for testing.
func DiscoverLocalModuleDirs(repoDir string) []string {
	return discoverLocalModuleDirs(repoDir)
}

// DiscoverRemoteModuleDirs is exported for testing.
func DiscoverRemoteModuleDirs(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) []string {
	return discoverRemoteModuleDirs(ctx, provider, repo)
}

// GoModPathFor is exported for testing.
func GoModPathFor(dir string) string {
	return goModPathFor(dir)
}

// ResolveVersionUpgradeNeed is exported for testing.
func ResolveVersionUpgradeNeed(
	read func(dir string) (string, error),
	moduleDirs func() []string,
	targetVersion string,
) (bool, string, bool) {
	return resolveVersionUpgradeNeed(read, moduleDirs, targetVersion)
}

// WriteCommitAndPush is exported for testing.
func WriteCommitAndPush(sb *strings.Builder) {
	writeCommitAndPush(sb)
}

// WriteGoUpgradeCommands is exported for testing.
func WriteGoUpgradeCommands(sb *strings.Builder) {
	writeGoUpgradeCommands(sb)
}

// ResolveVersionContext is exported for testing.
func ResolveVersionContext(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	latestVersion string,
) *versionContext {
	return resolveVersionContext(ctx, provider, repo, latestVersion)
}

// LocalResolveVersionContext is exported for testing.
func LocalResolveVersionContext(repoDir, latestVersion string) *versionContext {
	return localResolveVersionContext(repoDir, latestVersion)
}

// VersionContext is exported for testing.
type VersionContext = versionContext

// BuildUpgradeScript is exported for testing.
func BuildUpgradeScript(params upgradeParams, repoDir, goBinary string) string {
	return buildUpgradeScript(params, repoDir, goBinary)
}

// BuildEnv is exported for testing.
func BuildEnv(params upgradeParams, repoDir, goBinary string) []string {
	return buildEnv(params, repoDir, goBinary)
}

// BuildLocalGoScript is exported for testing.
func BuildLocalGoScript(providerName string, hasConfigSH bool) string {
	return buildLocalGoScript(providerName, hasConfigSH)
}

// UpgradeParams is exported for testing.
type UpgradeParams = upgradeParams

// UpgradeResult is exported for testing.
type UpgradeResult = upgradeResult

// NewUpdaterRepositoryForTest creates an updater with injected dependencies.
func NewUpdaterRepositoryForTest(vf VersionFetcher, runner ...cmdrunner.Runner) *UpdaterRepository {
	r := cmdrunner.NewDefaultRunner()
	if len(runner) > 0 {
		r = runner[0]
	}
	return &UpdaterRepository{versionFetcher: vf, cmdRunner: r}
}

// SetDefaultRunner overrides the package-level command runner for testing.
func SetDefaultRunner(r cmdrunner.Runner) func() {
	old := defaultRunner
	defaultRunner = r
	return func() { defaultRunner = old }
}

// WriteAzureDevOpsAuth is exported for testing.
func WriteAzureDevOpsAuth(sb *strings.Builder) {
	writeAzureDevOpsAuth(sb)
}

// WriteGitLabAuth is exported for testing.
func WriteGitLabAuth(sb *strings.Builder) {
	writeGitLabAuth(sb)
}

// OpenPullRequest is exported for testing.
func OpenPullRequest(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
	vCtx *versionContext,
	result *upgradeResult,
	hasConfigSH bool,
) ([]entities.PullRequest, error) {
	return openPullRequest(ctx, provider, repo, opts, vCtx, result, hasConfigSH)
}

// FileExistsLocally is exported for testing.
func FileExistsLocally(path string) bool {
	return fileExistsLocally(path)
}

// LocalUpgradeParamsType is exported for testing.
type LocalUpgradeParamsType = localUpgradeParams

// HandleDryRun is exported for testing.
func HandleDryRun(vCtx *versionContext, repoDir string) *LocalResult {
	return handleDryRun(vCtx, repoDir)
}

// BuildLocalUpgradeScriptFull is exported for testing.
func BuildLocalUpgradeScriptFull(params localUpgradeParams) string {
	return buildLocalUpgradeScript(params)
}

// WriteLocalAuth is exported for testing.
func WriteLocalAuth(sb *strings.Builder, params localUpgradeParams) {
	writeLocalAuth(sb, params)
}

// BuildLocalEnvFull is exported for testing.
func BuildLocalEnvFull(params localUpgradeParams, goBinary string) []string {
	return buildLocalEnv(params, goBinary)
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

// ResolveClosestGolangTag is exported for testing.
func ResolveClosestGolangTag(available []string, goVersion, suffix string) (string, bool) {
	return resolveClosestGolangTag(available, goVersion, suffix)
}

// ParseGolangTag is exported for testing.
func ParseGolangTag(tag string) (string, string, bool) {
	return parseGolangTag(tag)
}

// RewriteGolangTags is exported for testing.
func RewriteGolangTags(content, goVersion string, available []string, relPath string) string {
	return rewriteGolangTags(content, goVersion, available, relPath)
}

// IsDockerfileName is exported for testing.
func IsDockerfileName(name string) bool {
	return isDockerfileName(name)
}

// UpdateDockerfileGolangTags is exported for testing. The tag lister is
// injected directly so tests avoid shared global state and run in parallel.
func UpdateDockerfileGolangTags(
	ctx context.Context,
	repoDir, goVersion string,
	listTags func(ctx context.Context) ([]string, error),
) (bool, error) {
	return updateDockerfileGolangTags(ctx, repoDir, goVersion, listTags)
}

// ChangelogEntries is exported for testing.
func ChangelogEntries(vCtx *versionContext) []string {
	return changelogEntries(vCtx)
}
