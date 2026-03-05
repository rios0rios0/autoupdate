package support

import (
	"context"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	langEntities "github.com/rios0rios0/langforge/pkg/domain/entities"
	langRepos "github.com/rios0rios0/langforge/pkg/domain/repositories"
)

// RemoteFileChecker creates a langforge FileChecker that uses gitforge's
// provider API for remote file existence checks. For exact paths (e.g.
// "go.mod") it delegates to HasFile; for glob patterns (e.g. "*.tf") it
// extracts the extension and delegates to ListFiles.
func RemoteFileChecker(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) langEntities.FileChecker {
	return func(pathOrPattern string) (bool, error) {
		if isGlobPattern(pathOrPattern) {
			ext := extractExtension(pathOrPattern)
			files, err := provider.ListFiles(ctx, repo, ext)
			if err != nil {
				return false, err
			}
			return len(files) > 0, nil
		}
		return provider.HasFile(ctx, repo, pathOrPattern), nil
	}
}

// DetectRemote checks whether the given langforge detector matches a remote
// repository by building a RemoteFileChecker and delegating to DetectWith.
func DetectRemote(
	ctx context.Context,
	detector langRepos.LanguageDetector,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) (bool, error) {
	return langRepos.DetectWith(detector, RemoteFileChecker(ctx, provider, repo))
}

// isGlobPattern returns true if the path contains glob metacharacters.
func isGlobPattern(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

// extractExtension extracts the file extension from a glob pattern.
// For example, "*.tf" returns ".tf" and "*.hcl" returns ".hcl".
func extractExtension(pattern string) string {
	if idx := strings.LastIndex(pattern, "."); idx >= 0 {
		return pattern[idx:]
	}
	return pattern
}
