package support

import (
	"context"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	langEntities "github.com/rios0rios0/langforge/pkg/domain/entities"
	langRepos "github.com/rios0rios0/langforge/pkg/domain/repositories"
	"github.com/rios0rios0/langforge/pkg/support/fileutil"
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
	return fileutil.NewFileChecker(
		func(path string) (bool, error) {
			return provider.HasFile(ctx, repo, path), nil
		},
		func(pattern string) (bool, error) {
			ext := fileutil.ExtractExtension(pattern)
			files, err := provider.ListFiles(ctx, repo, ext)
			if err != nil {
				return false, err
			}
			return len(files) > 0, nil
		},
	)
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
