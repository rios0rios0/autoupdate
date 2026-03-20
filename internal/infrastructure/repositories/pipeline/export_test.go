package pipeline

import (
	"context"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// TruncateToGranularity is exported for testing.
func TruncateToGranularity(latest, reference string) string {
	return truncateToGranularity(latest, reference)
}

// IsExactVersion is exported for testing.
func IsExactVersion(ver string) bool {
	return isExactVersion(ver)
}

// UpgradeTask is exported for testing.
type UpgradeTask = upgradeTask

// NewUpgradeTask creates an upgradeTask for testing.
func NewUpgradeTask(language, currentVer, newVersion string) UpgradeTask {
	return upgradeTask{
		match:      versionMatch{Language: language, CurrentVer: currentVer},
		newVersion: newVersion,
	}
}

// AppendChangelogEntry is exported for testing.
func AppendChangelogEntry(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	upgrades []upgradeTask,
	fileChanges []entities.FileChange,
) []entities.FileChange {
	return appendChangelogEntry(ctx, provider, repo, upgrades, fileChanges)
}
