package support

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// chlogFragmentExtensions are the suffixes a pending fragment can carry. chlog
// writes ".yaml"; ".yml" is accepted so a hand-written fragment still counts as
// evidence that the repository uses the tool.
//
//nolint:gochecknoglobals // read-only lookup table
var chlogFragmentExtensions = []string{".yaml", ".yml"}

// DetectLocalChlog reports whether a repository on disk uses chlog and returns
// the effective configuration.
//
// A repository counts as a chlog user when it commits a .chlog.yaml or when the
// unreleased fragment directory exists -- the configuration file is optional.
// Unlike chlog itself, the search never walks above repoDir: autoupdate is
// strictly per-repository, and a .chlog.yaml in a parent directory says nothing
// about this repo.
//
// A malformed or unreadable configuration is reported as an error rather than
// as "not a chlog repository": falling back would make autoupdate edit the
// CHANGELOG.md that chlog exists to keep untouched.
func DetectLocalChlog(repoDir string) (*entities.ChlogConfig, bool, error) {
	config, hasConfigFile, err := loadLocalChlogConfig(repoDir)
	if err != nil {
		return nil, false, err
	}
	if hasConfigFile {
		return config, true, nil
	}

	unreleasedDir := entities.ChlogFragmentDiskPath(repoDir, config.UnreleasedPath())
	info, err := os.Stat(unreleasedDir)
	switch {
	case err == nil:
		return config, info.IsDir(), nil
	case errors.Is(err, os.ErrNotExist):
		return config, false, nil
	default:
		// A stat failure other than "absent" -- a permission error, a broken
		// mount -- must not be read as "this project does not use chlog": that
		// would silently write the entry into the wrong file.
		return nil, false, fmt.Errorf(
			"failed to inspect the chlog fragment directory %s: %w", unreleasedDir, err)
	}
}

// loadLocalChlogConfig reads .chlog.yaml (or .chlog.yml) from the repository
// root. The boolean reports whether a file was found.
func loadLocalChlogConfig(repoDir string) (*entities.ChlogConfig, bool, error) {
	for _, name := range []string{entities.ChlogConfigFile, entities.ChlogAltConfigFile} {
		configPath := filepath.Join(repoDir, name)

		data, err := os.ReadFile(configPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, false, fmt.Errorf("failed to read %s: %w", configPath, err)
		}

		config, err := entities.ParseChlogConfig(data)
		if err != nil {
			return nil, false, err
		}
		return config, true, nil
	}

	defaults := entities.DefaultChlogConfig()
	return &defaults, false, nil
}

// DetectRemoteChlog reports whether a repository reachable through the provider
// API uses chlog and returns the effective configuration.
//
// It mirrors DetectLocalChlog, trading the two stat calls for one HasFile probe
// per configuration file name plus, only when neither is present, a single tree
// listing to look for pending fragments.
func DetectRemoteChlog(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) (*entities.ChlogConfig, bool, error) {
	for _, name := range []string{entities.ChlogConfigFile, entities.ChlogAltConfigFile} {
		if !provider.HasFile(ctx, repo, name) {
			continue
		}

		content, err := provider.GetFileContent(ctx, repo, name)
		if err != nil {
			return nil, false, fmt.Errorf("failed to fetch %s for %s: %w",
				name, entities.RepoKey(repo), err)
		}

		config, err := entities.ParseChlogConfig([]byte(content))
		if err != nil {
			return nil, false, err
		}
		return config, true, nil
	}

	defaults := entities.DefaultChlogConfig()
	hasFragments, err := hasRemoteChlogFragments(ctx, provider, repo, &defaults)
	if err != nil {
		return nil, false, err
	}
	return &defaults, hasFragments, nil
}

// hasRemoteChlogFragments reports whether the repository carries any pending
// fragment.
//
// ListFiles only filters on a path suffix, and every provider fetches the whole
// tree regardless of the pattern, so the listing is requested once unfiltered
// and both the directory prefix and the extension are matched here. That keeps
// the probe to a single API call while still recognising either extension.
func hasRemoteChlogFragments(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	config *entities.ChlogConfig,
) (bool, error) {
	files, err := provider.ListFiles(ctx, repo, "")
	if err != nil {
		return false, fmt.Errorf("failed to list the files of %s: %w", entities.RepoKey(repo), err)
	}

	prefix := config.UnreleasedPath() + "/"
	for _, file := range files {
		if file.IsDir {
			continue
		}
		// Azure DevOps returns tree paths rooted at "/", the other providers
		// return them relative to the repository root.
		path := strings.TrimPrefix(file.Path, "/")
		if strings.HasPrefix(path, prefix) && hasChlogFragmentExtension(path) {
			return true, nil
		}
	}

	return false, nil
}

// hasChlogFragmentExtension reports whether a path carries a fragment suffix.
func hasChlogFragmentExtension(path string) bool {
	for _, ext := range chlogFragmentExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}
