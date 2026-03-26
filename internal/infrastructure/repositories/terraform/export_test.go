//go:build unit

package terraform

import (
	"context"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// --- Exported types ---

// DepKind is exported for testing.
type DepKind = depKind

// DepWithContent is exported for testing.
type DepWithContent = depWithContent

// UpgradeTask is exported for testing.
type UpgradeTask = upgradeTask

// --- Exported constants ---

const (
	DepKindModule = depKindModule
	DepKindImage  = depKindImage
)

// --- Exported constructors ---

// NewUpgradeTask creates an upgradeTask for testing.
func NewUpgradeTask(dep entities.Dependency, newVersion, fileContent string, kind depKind) UpgradeTask {
	return upgradeTask{
		dep:         dep,
		newVersion:  newVersion,
		fileContent: fileContent,
		kind:        kind,
	}
}

// NewDepWithContent creates a depWithContent for testing.
func NewDepWithContent(dep entities.Dependency, content string, kind depKind) DepWithContent {
	return depWithContent{
		Dependency:  dep,
		FileContent: content,
		Kind:        kind,
	}
}

// --- Exported function wrappers ---

// IsSemverLike is exported for testing.
func IsSemverLike(version string) bool {
	return isSemverLike(version)
}

// IsGitModule is exported for testing.
func IsGitModule(source string) bool {
	return isGitModule(source)
}

// ExtractVersion is exported for testing.
func ExtractVersion(source string) string {
	return extractVersion(source)
}

// RemoveVersionFromSource is exported for testing.
func RemoveVersionFromSource(source string) string {
	return removeVersionFromSource(source)
}

// ExtractRepoName is exported for testing.
func ExtractRepoName(source string) string {
	return extractRepoName(source)
}

// IsNewerVersion is exported for testing.
func IsNewerVersion(current, newVersion string) bool {
	return isNewerVersion(current, newVersion)
}

// NormalizeVersion is exported for testing.
func NormalizeVersion(version string) string {
	return normalizeVersion(version)
}

// ApplyVersionUpgrade is exported for testing.
func ApplyVersionUpgrade(content string, dep entities.Dependency, newVersion string) string {
	return applyVersionUpgrade(content, dep, newVersion)
}

// ApplyImageVersionUpgrade is exported for testing.
func ApplyImageVersionUpgrade(content string, dep entities.Dependency, newVersion string) string {
	return applyImageVersionUpgrade(content, dep, newVersion)
}

// BuildSourceWithVersion is exported for testing.
func BuildSourceWithVersion(source, version string) string {
	return buildSourceWithVersion(source, version)
}

// ScanTerraformFile is exported for testing.
func ScanTerraformFile(content, filePath string) []entities.Dependency {
	return scanTerraformFile(content, filePath)
}

// ScanWithRegex is exported for testing.
func ScanWithRegex(content, filePath string) []entities.Dependency {
	return scanWithRegex(content, filePath)
}

// ScanHCLFile is exported for testing.
func ScanHCLFile(content, filePath string) []entities.Dependency {
	return scanHCLFile(content, filePath)
}

// CountByKind is exported for testing.
func CountByKind(tasks []upgradeTask) (int, int) {
	return countByKind(tasks)
}

// GenerateBranchName is exported for testing.
func GenerateBranchName(tasks []upgradeTask) string {
	return generateBranchName(tasks)
}

// GenerateCommitMessage is exported for testing.
func GenerateCommitMessage(tasks []upgradeTask) string {
	return generateCommitMessage(tasks)
}

// GeneratePRTitle is exported for testing.
func GeneratePRTitle(tasks []upgradeTask) string {
	return generatePRTitle(tasks)
}

// GeneratePRDescription is exported for testing.
func GeneratePRDescription(tasks []upgradeTask) string {
	return generatePRDescription(tasks)
}

// ApplyUpgrades is exported for testing.
func ApplyUpgrades(tasks []upgradeTask) []entities.FileChange {
	return applyUpgrades(tasks)
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

// StripVersionPrefix is exported for testing.
func StripVersionPrefix(v string) string {
	return stripVersionPrefix(v)
}

// ExtractChangelogVersions is exported for testing.
func ExtractChangelogVersions(content string) map[string]bool {
	return extractChangelogVersions(content)
}

// FindLatestChangelogVersion is exported for testing.
func FindLatestChangelogVersion(
	ctx context.Context,
	provider repositories.ProviderRepository,
	depRepo *entities.Repository,
	tags []string,
) string {
	return findLatestChangelogVersion(ctx, provider, depRepo, tags)
}

// UpgradeTaskNewVersion returns the newVersion field from an upgradeTask.
func UpgradeTaskNewVersion(t UpgradeTask) string {
	return t.newVersion
}

// LocalScanAllDependencies is exported for testing.
func LocalScanAllDependencies(u *UpdaterRepository, repoDir string) []DepWithContent {
	return u.localScanAllDependencies(repoDir)
}

// DetermineUpgrades is exported for testing.
func DetermineUpgrades(
	u *UpdaterRepository,
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	allDeps []DepWithContent,
) []UpgradeTask {
	return u.determineUpgrades(ctx, provider, repo, allDeps)
}

// CreateUpgradePR is exported for testing.
func CreateUpgradePR(
	u *UpdaterRepository,
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
	upgrades []UpgradeTask,
) ([]entities.PullRequest, error) {
	return u.createUpgradePR(ctx, provider, repo, opts, upgrades)
}

// ResolveTagsForSource is exported for testing.
func ResolveTagsForSource(
	ctx context.Context,
	provider repositories.ProviderRepository,
	currentRepo entities.Repository,
	source string,
) ([]string, *entities.Repository) {
	return resolveTagsForSource(ctx, provider, currentRepo, source)
}
