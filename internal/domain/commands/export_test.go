//go:build unit

package commands

import (
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// ParseRemoteURL exports parseRemoteURL for testing.
var ParseRemoteURL = parseRemoteURL //nolint:gochecknoglobals // test export

// RemoteInfo exports remoteInfo for testing.
type RemoteInfo = remoteInfo

// LocalPRInfoForTest exports localPRInfo for testing.
type LocalPRInfoForTest = localPRInfo

// GeneratePRContent exports generatePRContent for testing.
var GeneratePRContent = generatePRContent //nolint:gochecknoglobals // test export

// FilterRepositories exports filterRepositories for testing.
func FilterRepositories(
	repos []entities.Repository,
	settings *entities.Settings,
) []entities.Repository {
	return filterRepositories(repos, settings)
}

// RunLocalUpgrade exports runLocalUpgrade for testing.
var RunLocalUpgrade = runLocalUpgrade //nolint:gochecknoglobals // test export

// LocalUpgradeHandlers exports localUpgradeHandlers for testing.
var LocalUpgradeHandlers = localUpgradeHandlers //nolint:gochecknoglobals // test export

// ServiceTypeToProvider exports serviceTypeToProvider for testing.
var ServiceTypeToProvider = serviceTypeToProvider //nolint:gochecknoglobals // test export

// DetectDefaultBranch exports detectDefaultBranch for testing.
var DetectDefaultBranch = detectDefaultBranch //nolint:gochecknoglobals // test export

// ParseGitRemote exports parseGitRemote for testing.
var ParseGitRemote = parseGitRemote //nolint:gochecknoglobals // test export

// AppliedUpdaterResult exports appliedUpdaterResult for testing.
type AppliedUpdaterResult = appliedUpdaterResult

// NewAppliedUpdaterResult constructs an appliedUpdaterResult fixture for tests.
func NewAppliedUpdaterResult(
	name string, result *repositories.LocalUpdateResult,
) AppliedUpdaterResult {
	return appliedUpdaterResult{name: name, result: result}
}

// ApplicableUpdater exports applicableUpdater for testing.
type ApplicableUpdater = applicableUpdater

// NewApplicableUpdaterForTest constructs an applicableUpdater fixture for tests.
func NewApplicableUpdaterForTest(
	updater repositories.UpdaterRepository, opts entities.UpdateOptions,
) ApplicableUpdater {
	return applicableUpdater{updater: updater, opts: opts}
}

// BuildAggregateBranchName exports buildAggregateBranchName for testing.
var BuildAggregateBranchName = buildAggregateBranchName //nolint:gochecknoglobals // test export

// BuildAggregateCommitMessage exports buildAggregateCommitMessage for testing.
var BuildAggregateCommitMessage = buildAggregateCommitMessage //nolint:gochecknoglobals // test export

// BuildAggregatePRTitle exports buildAggregatePRTitle for testing.
var BuildAggregatePRTitle = buildAggregatePRTitle //nolint:gochecknoglobals // test export

// BuildAggregatePRDescription exports buildAggregatePRDescription for testing.
var BuildAggregatePRDescription = buildAggregatePRDescription //nolint:gochecknoglobals // test export

// AnyAutoComplete exports anyAutoComplete for testing.
var AnyAutoComplete = anyAutoComplete //nolint:gochecknoglobals // test export

// AllDryRun exports allDryRun for testing.
var AllDryRun = allDryRun //nolint:gochecknoglobals // test export

// FirstLine exports firstLine for testing.
var FirstLine = firstLine //nolint:gochecknoglobals // test export

// ResolveAggregateTargetBranch exports resolveAggregateTargetBranch for testing.
var ResolveAggregateTargetBranch = resolveAggregateTargetBranch //nolint:gochecknoglobals // test export
