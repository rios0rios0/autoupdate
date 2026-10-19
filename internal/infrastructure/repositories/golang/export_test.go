package golang

import (
	"context"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
)

// UnpreparedMigrations exports unpreparedMigrations for testing: the pairing of
// findings with the fact that nothing was done about them, which is what every
// caller that cannot prepare a migration reports.
func UnpreparedMigrations(majors []MajorAvailable) []MigrationPrepared {
	return unpreparedMigrations(majors)
}

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
func WriteGoUpgradeCommands(sb *strings.Builder, allowMajorUpdates bool) {
	writeGoUpgradeCommands(sb, allowMajorUpdates)
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
func BuildLocalGoScript(providerName string, hasConfigSH, allowMajorUpdates bool) string {
	return buildLocalGoScript(providerName, hasConfigSH, allowMajorUpdates)
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

// UpgradeSubject is exported for testing.
func UpgradeSubject(goVersion string, goVersionUpdated bool) string {
	return upgradeSubject(goVersion, goVersionUpdated)
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

// BuildRemoteScriptForMajorMode is exported for testing the remote (clone-based)
// path's handling of allow_major_updates. It sets only the field that mode
// depends on, because that is the one the path previously failed to carry.
func BuildRemoteScriptForMajorMode(allowMajorUpdates bool) string {
	//nolint:exhaustruct // only the major-mode field is under test here
	return buildUpgradeScript(upgradeParams{AllowMajorUpdates: allowMajorUpdates}, "", "")
}

// DiffGoMod is exported for testing.
func DiffGoMod(modPath string, before, after []byte) ([]entities.DependencyChange, error) {
	return diffGoMod(modPath, before, after)
}

// ObserveModuleChanges is exported for testing.
func ObserveModuleChanges(ctx context.Context, repoDir string) ([]entities.DependencyChange, error) {
	return observeModuleChanges(ctx, repoDir)
}

// ChangelogEntry is exported for testing.
func ChangelogEntry(goVersionUpdated bool, goVersion string) string {
	return changelogEntry(goVersionUpdated, goVersion)
}

// MigrationParams is exported for testing.
type MigrationParams = migrationParams

// MigrationsForRun is exported for testing: the decision a run makes about the
// newer majors it found, which is the whole of the preparation.
func MigrationsForRun(
	ctx context.Context, params MigrationParams, enabled bool,
) []MigrationPrepared {
	return prepareMigrationsForRun(ctx, params, enabled)
}

// PrepareLocalMigrations is exported for testing: what the local (standalone)
// flow does about the newer majors an upgrade found.
func PrepareLocalMigrations(
	ctx context.Context,
	repoDir string,
	opts LocalUpgradeOptions,
	output, baseBranch string,
) []MigrationPrepared {
	return prepareLocalMigrations(ctx, repoDir, opts, output, baseBranch)
}

// MigrationBranchName is exported for testing.
func MigrationBranchName(major MajorAvailable) string {
	return migrationBranchName(major)
}

// MigrationSlug is exported for testing.
func MigrationSlug(modulePath string) string {
	return migrationSlug(modulePath)
}

// MigrateImportPath is exported for testing.
func MigrateImportPath(importPath, oldPath, newPath string) (string, bool) {
	return migrateImportPath(importPath, oldPath, newPath)
}

// RewriteModuleImports is exported for testing.
func RewriteModuleImports(root, oldPath, newPath string) ([]string, []string, error) {
	return rewriteModuleImports(root, oldPath, newPath)
}

// RewriteFileImports is exported for testing.
func RewriteFileImports(filePath, oldPath, newPath string) (bool, bool, error) {
	return rewriteFileImports(filePath, oldPath, newPath)
}

// MigrateImportPathExcept is exported for testing sibling module protection.
func MigrateImportPathExcept(importPath, oldPath, newPath string, protected []string) (string, bool) {
	return migrateImportPathExcept(importPath, oldPath, newPath, protected)
}

// CollectMigrationChanges is exported for testing.
func CollectMigrationChanges(
	ctx context.Context, worktree string,
) ([]entities.FileChange, string, error) {
	return collectMigrationChanges(ctx, worktree)
}

// MigrationNote is exported for testing.
func MigrationNote(message, token string) string {
	return migrationNote(message, token)
}

// MigrationCommitMessage is exported for testing.
func MigrationCommitMessage(major MajorAvailable, compiles bool, note string) string {
	return migrationCommitMessage(major, migrationBuild{Compiles: compiles, Note: note})
}

// MigrationChangelogEntry is exported for testing.
func MigrationChangelogEntry(major MajorAvailable) string {
	return migrationChangelogEntry(major)
}

// BaseBranchName is exported for testing.
func BaseBranchName(targetRef string) string {
	return baseBranchName(targetRef)
}

// MigrationWorktree is exported for testing the worktree lifecycle.
func MigrationWorktree(
	ctx context.Context, params MigrationParams,
) (string, func(), error) {
	return addMigrationWorktree(ctx, params)
}

// RemoveMigrationWorktree is exported for testing cleanup fallbacks.
func RemoveMigrationWorktree(ctx context.Context, repoDir, dir string) {
	removeMigrationWorktree(ctx, repoDir, dir)
}

// MigrationBuild is exported for testing the in-worktree preparation.
type MigrationBuild = migrationBuild

// PrepareMigrationInWorktree is exported for testing the mechanical migration.
func PrepareMigrationInWorktree(
	ctx context.Context,
	params MigrationParams,
	major MajorAvailable,
	worktree string,
	env []string,
) (MigrationBuild, error) {
	return prepareMigrationInWorktree(ctx, params, major, worktree, env)
}

// MigrationRun is exported for testing command execution and redaction.
func MigrationRun(
	ctx context.Context,
	params MigrationParams,
	dir string,
	args ...string,
) error {
	runner := &migrationRunner{
		ctx: ctx, env: nil, goBinary: params.GoBinary,
		token: params.AuthToken, runner: params.Runner,
	}
	return runner.run(dir, args...)
}

// ApplyEdits is exported for testing invalid edit ranges.
func ApplyEdits(source []byte, edits []SourceEdit) []byte {
	internal := make([]sourceEdit, 0, len(edits))
	for _, edit := range edits {
		internal = append(internal, sourceEdit{start: edit.Start, end: edit.End, text: edit.Text})
	}
	return applyEdits(source, internal)
}

// SourceEdit describes one test edit range.
type SourceEdit struct {
	Start int
	End   int
	Text  string
}

// VersionMajorNumber is exported for testing semantic version parsing.
func VersionMajorNumber(version string) string { return versionMajorNumber(version) }

// PathMajorSuffix is exported for testing module path parsing.
func PathMajorSuffix(modulePath string) string { return pathMajorSuffix(modulePath) }

// RefsHeads is exported for testing branch ref normalization.
func RefsHeads(branch string) string { return refsHeads(branch) }

// ShellQuote is exported for testing shell argument quoting.
func ShellQuote(value string) string { return shellQuote(value) }

// ReplacedModuleDir is exported for testing: the module whose go.mod redirects
// the given path, which is what stops a migration from being prepared.
func ReplacedModuleDir(worktree, modulePath string) string {
	return replacedModuleDir(worktree, modulePath)
}

// GoChangelogSummary is exported for testing.
const GoChangelogSummary = goChangelogSummary
