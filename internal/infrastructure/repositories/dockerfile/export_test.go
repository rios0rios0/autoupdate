package dockerfile

import (
	"context"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// ParseTag is exported for testing.
func ParseTag(tag string) (string, string, int, bool) {
	return parseTag(tag)
}

// FindBestUpgrade is exported for testing.
func FindBestUpgrade(current *parsedImageRef, availableTags []string, allowMajorUpdates bool) string {
	return findBestUpgrade(current, availableTags, allowMajorUpdates)
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
			Digest:     ref.parsed.Digest,
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
	Digest     string
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

// LocalScanAllDockerfiles is exported for testing.
func LocalScanAllDockerfiles(repoDir string) []ImageRefResult {
	refs := localScanAllDockerfiles(repoDir)
	results := make([]ImageRefResult, len(refs))
	for i, ref := range refs {
		results[i] = ImageRefResult{
			Name:       ref.dep.Name,
			CurrentVer: ref.dep.CurrentVer,
			FilePath:   ref.dep.FilePath,
			Line:       ref.dep.Line,
		}
	}
	return results
}

// ImageRefResult is a simplified representation of an imageRef for testing.
type ImageRefResult struct {
	Name       string
	CurrentVer string
	FilePath   string
	Line       int
}

// ApplyUpgrades is exported for testing.
func ApplyUpgrades(tasks []UpgradeTask, allRefs []ImageRef) []entities.FileChange {
	return applyUpgrades(tasks, allRefs)
}

// ImageRef is exported for testing.
type ImageRef = imageRef

// RegistryTag is exported for testing.
type RegistryTag = registryTag

// SetFetchTagsFunc overrides the tag fetching function for testing with one
// that reports tag names only, as a registry listing without digests would.
// It returns a cleanup function that restores the original.
func SetFetchTagsFunc(fn func(ctx context.Context, ref *parsedImageRef) ([]string, error)) func() {
	return SetFetchRegistryTagsFunc(
		func(ctx context.Context, ref *parsedImageRef) ([]RegistryTag, error) {
			names, err := fn(ctx, ref)
			if err != nil {
				return nil, err
			}

			tags := make([]RegistryTag, 0, len(names))
			for _, name := range names {
				tags = append(tags, RegistryTag{Name: name})
			}
			return tags, nil
		},
	)
}

// SetFetchRegistryTagsFunc overrides the tag fetching function for testing with
// one that reports the digest each tag resolves to, which is what a real
// listing carries. It returns a cleanup function that restores the original.
func SetFetchRegistryTagsFunc(
	fn func(ctx context.Context, ref *parsedImageRef) ([]RegistryTag, error),
) func() {
	original := fetchTagsFunc
	fetchTagsFunc = fn
	return func() { fetchTagsFunc = original }
}

// NewImageRefWithDigest creates an imageRef for a FROM clause that pins a
// digest alongside its tag.
func NewImageRefWithDigest(content, filePath, imageName, currentVer, digest string) ImageRef {
	ref := NewImageRefFromContent(content, filePath, imageName, imageName, currentVer)
	ref.parsed.Digest = digest
	return ref
}

// UpgradeTaskDigest returns the digest a task writes back, exported for testing.
func UpgradeTaskDigest(task UpgradeTask) string {
	return task.newDigest
}

// DetermineUpgrades is exported for testing.
func DetermineUpgrades(ctx context.Context, allRefs []ImageRef, allowMajorUpdates bool) []UpgradeTask {
	return determineUpgrades(ctx, allRefs, allowMajorUpdates)
}

// CreateUpgradePR is exported for testing.
func CreateUpgradePR(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
	upgrades []upgradeTask,
	allRefs []imageRef,
) ([]entities.PullRequest, error) {
	return createUpgradePR(ctx, provider, repo, opts, upgrades, allRefs)
}

// NewUpgradeTaskFull creates an upgradeTask with all fields populated for testing.
func NewUpgradeTaskFull(imageName, source, currentVer, newTag, filePath string) UpgradeTask {
	return upgradeTask{
		dep: entities.Dependency{
			Name:       imageName,
			Source:     source,
			CurrentVer: currentVer,
			FilePath:   filePath,
		},
		newTag: newTag,
		parsed: &parsedImageRef{Image: imageName},
	}
}

// NewUpgradeTaskWithDigest creates an upgradeTask that re-pins a digest, for
// testing.
func NewUpgradeTaskWithDigest(imageName, currentVer, newTag, filePath, digest string) UpgradeTask {
	task := NewUpgradeTaskFull(imageName, imageName, currentVer, newTag, filePath)
	task.newDigest = digest
	return task
}

// NewImageRefFromContent creates an imageRef from Dockerfile content for testing.
// It parses the currentVer tag to populate version, suffix, and precision fields.
func NewImageRefFromContent(content, filePath, imageName, source, currentVer string) ImageRef {
	namespace := ""
	image := imageName
	if strings.Contains(imageName, "/") {
		parts := splitNamespace(imageName)
		namespace = parts[0]
		image = parts[1]
	}

	version, suffix, precision, _ := parseTag(currentVer)

	return imageRef{
		dep: entities.Dependency{
			Name:       imageName,
			Source:     source,
			CurrentVer: currentVer,
			FilePath:   filePath,
		},
		parsed: &parsedImageRef{
			Namespace: namespace,
			Image:     image,
			Tag:       currentVer,
			Version:   version,
			Suffix:    suffix,
			Precision: precision,
		},
		fileContent: content,
	}
}

// splitNamespace splits an image name into namespace and image parts.
func splitNamespace(imageName string) [2]string {
	for i, c := range imageName {
		if c == '/' {
			return [2]string{imageName[:i], imageName[i+1:]}
		}
	}
	return [2]string{"", imageName}
}
