package dockerfile

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
