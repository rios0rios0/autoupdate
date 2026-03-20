package dockerfile

import (
	"context"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// ParseTag is exported for testing.
func ParseTag(tag string) (string, string, int, bool) {
	return parseTag(tag)
}

// FindBestUpgrade is exported for testing.
func FindBestUpgrade(current *parsedImageRef, availableTags []string) string {
	return findBestUpgrade(current, availableTags)
}

// ScanDockerfile is exported for testing. It returns dependencies found in a Dockerfile.
func ScanDockerfile(content, filePath string) []scanResult {
	refs := scanDockerfile(content, filePath)
	results := make([]scanResult, len(refs))
	for i, ref := range refs {
		results[i] = scanResult{
			Name:       ref.dep.Name,
			CurrentVer: ref.dep.CurrentVer,
			FilePath:   ref.dep.FilePath,
			Line:       ref.dep.Line,
		}
	}
	return results
}

// scanResult is a simplified representation of an imageRef for testing.
type scanResult struct {
	Name       string
	CurrentVer string
	FilePath   string
	Line       int
}

// IsDockerfilePath is exported for testing.
func IsDockerfilePath(path string) bool {
	return isDockerfilePath(path)
}

// ParsedImageRef is exported for testing.
type ParsedImageRef = parsedImageRef

// IsDockerHubImage is exported for testing.
func IsDockerHubImage(imageName string) bool {
	return isDockerHubImage(imageName)
}

// UpgradeTask is exported for testing.
type UpgradeTask = upgradeTask

// NewUpgradeTask creates an upgradeTask for testing.
func NewUpgradeTask(imageName, currentVer, newTag string) UpgradeTask {
	return upgradeTask{
		dep:    entities.Dependency{Name: imageName, CurrentVer: currentVer},
		newTag: newTag,
		parsed: &parsedImageRef{Image: imageName},
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
