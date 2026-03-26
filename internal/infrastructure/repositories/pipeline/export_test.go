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

// --- GitHub Actions exports ---

// ActionRef is exported for testing.
type ActionRef = actionRef

// ActionUpgrade is exported for testing.
type ActionUpgrade = actionUpgrade

// RefStyle is exported for testing.
type RefStyle = refStyle

// ActionTagCache is exported for testing.
type ActionTagCache = actionTagCache

// Exported refStyle constants for testing.
const (
	RefStyleMajor  = refStyleMajor
	RefStyleSemver = refStyleSemver
)

// ScanFileForActions is exported for testing.
func ScanFileForActions(content, filePath string) []ActionRef {
	return scanFileForActions(content, filePath)
}

// ClassifyRefStyle is exported for testing.
func ClassifyRefStyle(ref string) RefStyle {
	return classifyRefStyle(ref)
}

// DetermineActionUpgrade is exported for testing.
func DetermineActionUpgrade(ref ActionRef, tags []string) *ActionUpgrade {
	return determineActionUpgrade(ref, tags)
}

// NormalizeActionVersion is exported for testing.
func NormalizeActionVersion(ref string) string {
	return normalizeActionVersion(ref)
}

// ExtractMajor is exported for testing.
func ExtractMajor(ref string) int {
	return extractMajor(ref)
}

// FindActionUpgradesInFile is exported for testing.
func FindActionUpgradesInFile(
	ctx context.Context,
	provider repositories.ProviderRepository,
	content, filePath string,
	cache ActionTagCache,
) []UpgradeTask {
	return findActionUpgradesInFile(ctx, provider, content, filePath, cache)
}

// SanitizeBranchSegment is exported for testing.
func SanitizeBranchSegment(s string) string {
	return sanitizeBranchSegment(s)
}

// UpgradeTaskLanguage returns the language of an upgrade task.
func UpgradeTaskLanguage(t UpgradeTask) string { return t.match.Language }

// UpgradeTaskCurrentVer returns the current version of an upgrade task.
func UpgradeTaskCurrentVer(t UpgradeTask) string { return t.match.CurrentVer }

// UpgradeTaskNewVersion returns the new version of an upgrade task.
func UpgradeTaskNewVersion(t UpgradeTask) string { return t.newVersion }

// ActionUpgradeNewRef returns the new ref of an action upgrade.
func ActionUpgradeNewRef(u *ActionUpgrade) string { return u.newRef }

// ClassifyFile is exported for testing.
func ClassifyFile(path string) CISystem {
	return classifyFile(path)
}

// CISystem is exported for testing.
type CISystem = ciSystem

// Exported ciSystem constants for testing.
const (
	CIGitHubActions = ciGitHubActions
	CIAzureDevOps   = ciAzureDevOps
)

// GenerateBranchName is exported for testing.
func GenerateBranchName(tasks []UpgradeTask) string {
	return generateBranchName(tasks)
}

// GenerateCommitMessage is exported for testing.
func GenerateCommitMessage(tasks []UpgradeTask) string {
	return generateCommitMessage(tasks)
}

// GeneratePRTitle is exported for testing.
func GeneratePRTitle(tasks []UpgradeTask) string {
	return generatePRTitle(tasks)
}

// GeneratePRDescription is exported for testing.
func GeneratePRDescription(tasks []UpgradeTask) string {
	return generatePRDescription(tasks)
}

// LocalScanAndDetermineUpgrades is exported for testing.
func LocalScanAndDetermineUpgrades(
	ctx context.Context,
	repoDir string,
	provider repositories.ProviderRepository,
	latestVersions map[string]string,
) ([]UpgradeTask, map[string]string) {
	return localScanAndDetermineUpgrades(ctx, repoDir, provider, latestVersions)
}

// CreateUpgradePR is exported for testing.
func CreateUpgradePR(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
	upgrades []UpgradeTask,
	fileContents map[string]string,
) ([]entities.PullRequest, error) {
	return createUpgradePR(ctx, provider, repo, opts, upgrades, fileContents)
}
