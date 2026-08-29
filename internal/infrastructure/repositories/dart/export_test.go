package dart

import (
	"context"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// VersionContextExported exposes versionContext for testing.
type VersionContextExported = versionContext

// UpgradeParamsExported exposes upgradeParams for testing.
type UpgradeParamsExported = upgradeParams

// LocalUpgradeParamsExported exposes localUpgradeParams for testing.
type LocalUpgradeParamsExported = localUpgradeParams

// NewVersionContext exports newVersionContext for testing.
var NewVersionContext = newVersionContext //nolint:gochecknoglobals // test export

// ChangelogEntries exports changelogEntries for testing.
var ChangelogEntries = changelogEntries //nolint:gochecknoglobals // test export

// CommitMessage exports commitMessage for testing.
var CommitMessage = commitMessage //nolint:gochecknoglobals // test export

// LogDryRun exports logDryRun for testing.
var LogDryRun = logDryRun //nolint:gochecknoglobals // test export

// ApplyFvmPin exports applyFvmPin for testing.
var ApplyFvmPin = applyFvmPin //nolint:gochecknoglobals // test export

// SDKVersionFor exports sdkVersionFor for testing.
var SDKVersionFor = sdkVersionFor //nolint:gochecknoglobals // test export

// BuildBatchDartScript exports buildBatchDartScript for testing.
var BuildBatchDartScript = buildBatchDartScript //nolint:gochecknoglobals // test export

// BuildUpgradeScript exports buildUpgradeScript for testing.
var BuildUpgradeScript = buildUpgradeScript //nolint:gochecknoglobals // test export

// BuildLocalUpgradeScript exports buildLocalUpgradeScript for testing.
var BuildLocalUpgradeScript = buildLocalUpgradeScript //nolint:gochecknoglobals // test export

// BuildEnv exports buildEnv for testing.
var BuildEnv = buildEnv //nolint:gochecknoglobals // test export

// BuildLocalEnv exports buildLocalEnv for testing.
var BuildLocalEnv = buildLocalEnv //nolint:gochecknoglobals // test export

// HandleDryRun exports handleDryRun for testing.
var HandleDryRun = handleDryRun //nolint:gochecknoglobals // test export

// NewUpdaterRepositoryForTest builds the concrete updater with injected fetchers.
func NewUpdaterRepositoryForTest(dartFetcher, flutterFetcher VersionFetcher) *UpdaterRepository {
	return &UpdaterRepository{dartFetcher: dartFetcher, flutterFetcher: flutterFetcher}
}

// WriteGitAuth exports writeGitAuth for testing.
func WriteGitAuth(sb *strings.Builder, params upgradeParams) {
	writeGitAuth(sb, params)
}

// WriteLocalAuth exports writeLocalAuth for testing.
func WriteLocalAuth(sb *strings.Builder, params localUpgradeParams) {
	writeLocalAuth(sb, params)
}

// WriteDartUpgradeCommands exports writeDartUpgradeCommands for testing.
func WriteDartUpgradeCommands(sb *strings.Builder) {
	writeDartUpgradeCommands(sb)
}

// WriteFvmPinUpdate exports writeFvmPinUpdate for testing.
func WriteFvmPinUpdate(sb *strings.Builder) {
	writeFvmPinUpdate(sb)
}

// WriteCommitAndPush exports writeCommitAndPush for testing.
func WriteCommitAndPush(sb *strings.Builder) {
	writeCommitAndPush(sb)
}

// ResolveVersionContextForTest exports resolveVersionContext for testing.
func (u *UpdaterRepository) ResolveVersionContextForTest(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) *versionContext {
	return u.resolveVersionContext(ctx, provider, repo)
}

// ResolveLocalVersionContextForTest exports resolveLocalVersionContext for testing.
func (u *UpdaterRepository) ResolveLocalVersionContextForTest(
	ctx context.Context,
	repoDir string,
) *versionContext {
	return u.resolveLocalVersionContext(ctx, repoDir)
}
