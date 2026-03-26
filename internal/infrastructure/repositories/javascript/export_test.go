//go:build unit

package javascript

import (
	"context"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// HasOnlyLockfileVersionChanges is exported for testing.
func HasOnlyLockfileVersionChanges(ctx context.Context, repoDir string) bool {
	return hasOnlyLockfileVersionChanges(ctx, repoDir)
}

// IsPackageLockOnlyVersionSync is exported for testing.
func IsPackageLockOnlyVersionSync(ctx context.Context, repoDir string) bool {
	return isPackageLockOnlyVersionSync(ctx, repoDir)
}

// RevertWorkingTreeChanges is exported for testing.
func RevertWorkingTreeChanges(ctx context.Context, repoDir string) {
	revertWorkingTreeChanges(ctx, repoDir)
}

// ParseNodeVersionFile is exported for testing.
func ParseNodeVersionFile(content string) string {
	return parseNodeVersionFile(content)
}

// IsLTSRelease is exported for testing.
func IsLTSRelease(release nodeRelease) bool {
	return isLTSRelease(release)
}

// DetectPackageManager is exported for testing (remote-mode detection).
func DetectPackageManager(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) string {
	return detectPackageManager(ctx, provider, repo)
}

// NodeRelease is exported for testing.
type NodeRelease = nodeRelease

// VersionContext is exported for testing.
type VersionContext = versionContext

// UpgradeParams is exported for testing.
type UpgradeParams = upgradeParams

// NewUpdaterRepositoryForTest creates an updater with an injected version fetcher.
func NewUpdaterRepositoryForTest(vf VersionFetcher) *UpdaterRepository {
	return &UpdaterRepository{versionFetcher: vf}
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

// ReadCurrentNodeVersion is exported for testing.
func ReadCurrentNodeVersion(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) string {
	return readCurrentNodeVersion(ctx, provider, repo)
}

// PrepareChangelog is exported for testing.
func PrepareChangelog(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	vCtx *versionContext,
) string {
	return prepareChangelog(ctx, provider, repo, vCtx)
}

// BuildUpgradeScript is exported for testing.
func BuildUpgradeScript(params upgradeParams, repoDir string) string {
	return buildUpgradeScript(params, repoDir)
}

// WriteGitAuth is exported for testing.
func WriteGitAuth(params upgradeParams) string {
	var sb strings.Builder
	writeGitAuth(&sb, params)
	return sb.String()
}

// WriteJSUpgradeCommands is exported for testing.
func WriteJSUpgradeCommands(params upgradeParams) string {
	var sb strings.Builder
	writeJSUpgradeCommands(&sb, params)
	return sb.String()
}

// WriteDockerfileUpdate is exported for testing.
func WriteDockerfileUpdate() string {
	var sb strings.Builder
	writeDockerfileUpdate(&sb)
	return sb.String()
}

// WriteChangelogUpdate is exported for testing.
func WriteChangelogUpdate() string {
	var sb strings.Builder
	writeChangelogUpdate(&sb)
	return sb.String()
}

// WriteCommitAndPush is exported for testing.
func WriteCommitAndPush() string {
	var sb strings.Builder
	writeCommitAndPush(&sb)
	return sb.String()
}

// BuildEnv is exported for testing.
func BuildEnv(params upgradeParams, repoDir string) []string {
	return buildEnv(params, repoDir)
}

// BuildBatchJSScript is exported for testing.
func BuildBatchJSScript() string {
	return buildBatchJSScript()
}

// LocalUpgradeParamsExported is exported for testing.
type LocalUpgradeParamsExported = localUpgradeParams

// BuildLocalUpgradeScript is exported for testing.
func BuildLocalUpgradeScript(params localUpgradeParams) string {
	return buildLocalUpgradeScript(params)
}

// WriteLocalAuth is exported for testing.
func WriteLocalAuth(params localUpgradeParams) string {
	var sb strings.Builder
	writeLocalAuth(&sb, params)
	return sb.String()
}

// BuildLocalEnv is exported for testing.
func BuildLocalEnv(params localUpgradeParams) []string {
	return buildLocalEnv(params)
}

// DetectLocalPackageManager is exported for testing.
func DetectLocalPackageManager(repoDir string) string {
	return detectLocalPackageManager(repoDir)
}

// ReadLocalNodeVersion is exported for testing.
func ReadLocalNodeVersion(repoDir string) string {
	return readLocalNodeVersion(repoDir)
}

// NewUpdaterRepositoryWithDepsExported creates a JavaScript updater with injected dependencies (for testing).
func NewUpdaterRepositoryWithDepsExported(vf VersionFetcher) *UpdaterRepository {
	r, _ := NewUpdaterRepositoryWithDeps(vf).(*UpdaterRepository)
	return r
}
