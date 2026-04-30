package support

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// LoadLocalRepoConfig reads the per-repository .autoupdate.yaml from a
// directory on disk. A missing file is treated as "no config" and returns
// the zero-value RepoConfig with no error so callers can use the result
// unconditionally.
func LoadLocalRepoConfig(repoDir string) (*entities.RepoConfig, error) {
	path := filepath.Join(repoDir, entities.RepoConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &entities.RepoConfig{}, nil
		}
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	return entities.ParseRepoConfig(data)
}

// LoadRemoteRepoConfig reads the per-repository .autoupdate.yaml via the
// provider's file-access API. A repository without the file returns the
// zero-value config without making the second round-trip to fetch
// content. Transient errors fetching content are propagated so callers
// can decide whether to fail-open or fail-closed.
func LoadRemoteRepoConfig(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) (*entities.RepoConfig, error) {
	if !provider.HasFile(ctx, repo, entities.RepoConfigFile) {
		return &entities.RepoConfig{}, nil
	}

	content, err := provider.GetFileContent(ctx, repo, entities.RepoConfigFile)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s for %s/%s: %w",
			entities.RepoConfigFile, repo.Organization, repo.Name, err)
	}
	return entities.ParseRepoConfig([]byte(content))
}
