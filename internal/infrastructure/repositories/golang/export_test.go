//go:build unit

package golang

import (
	"context"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// ParseGoDirective is exported for testing.
func ParseGoDirective(content string) string {
	return parseGoDirective(content)
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

// PrepareChangelog is exported for testing.
func PrepareChangelog(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	vCtx *versionContext,
) string {
	return prepareChangelog(ctx, provider, repo, vCtx)
}

// NewUpdaterRepositoryForTest creates an updater with an injected version fetcher.
func NewUpdaterRepositoryForTest(vf VersionFetcher) *UpdaterRepository {
	return &UpdaterRepository{versionFetcher: vf}
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
