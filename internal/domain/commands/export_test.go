//go:build unit

package commands

import "github.com/rios0rios0/autoupdate/internal/domain/entities"

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
