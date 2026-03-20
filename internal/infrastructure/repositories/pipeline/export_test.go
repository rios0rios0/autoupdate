//go:build unit

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

// NewUpgradeTaskWithFullMatch creates an upgradeTask with a custom FullMatch for testing.
func NewUpgradeTaskWithFullMatch(language, currentVer, newVersion, filePath, fullMatch string) UpgradeTask {
	return upgradeTask{
		match: versionMatch{
			Language:   language,
			CurrentVer: currentVer,
			FullMatch:  fullMatch,
			FilePath:   filePath,
		},
		newVersion: newVersion,
	}
}

// ReplaceLastOccurrence is exported for testing.
func ReplaceLastOccurrence(s, old, replacement string) string {
	return replaceLastOccurrence(s, old, replacement)
}

// ApplyUpgrades is exported for testing.
func ApplyUpgrades(upgrades []UpgradeTask, fileContents map[string]string) []entities.FileChange {
	return applyUpgrades(upgrades, fileContents)
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
