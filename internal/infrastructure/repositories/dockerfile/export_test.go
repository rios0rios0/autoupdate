package dockerfile

// ParseTag is exported for testing.
var ParseTag = parseTag

// FindBestUpgrade is exported for testing.
var FindBestUpgrade = findBestUpgrade

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
var IsDockerfilePath = isDockerfilePath

// ParsedImageRef is exported for testing.
type ParsedImageRef = parsedImageRef

// IsDockerHubImage is exported for testing.
var IsDockerHubImage = isDockerHubImage
