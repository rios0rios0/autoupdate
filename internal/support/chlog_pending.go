package support

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	logger "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// pendingChlogEntries reads the bodies of the fragments a repository already
// has waiting under its unreleased directory, so the same statement is not
// filed twice.
//
// The duplicate check has to reach both formats or they drift apart: a chlog
// repository would keep collecting a fresh fragment per run saying exactly what
// the last run's fragment says, and the drift would only surface at release
// time, when the fragments are compiled into one section and the same bullet
// appears several times.
//
// A directory that cannot be read yields no entries rather than an error. The
// worst case is the duplicate this check exists to avoid, whereas refusing to
// write would drop a real changelog entry -- so the unreadable case fails open.
func pendingChlogEntries(repoDir string, config *entities.ChlogConfig) []string {
	unreleasedDir := entities.ChlogFragmentDiskPath(repoDir, config.UnreleasedPath())

	dirEntries, err := os.ReadDir(unreleasedDir)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warnf("Failed to read the chlog fragment directory %s: %v", unreleasedDir, err)
		}
		return nil
	}

	bodies := make([]string, 0, len(dirEntries))
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() || !hasChlogFragmentExtension(dirEntry.Name()) {
			continue
		}

		fragmentPath := filepath.Join(unreleasedDir, dirEntry.Name())

		// fragmentPath is repoDir joined with the validated configuration and a
		// name that came from listing that very directory.
		content, readErr := os.ReadFile(fragmentPath)
		if readErr != nil {
			logger.Warnf("Failed to read the chlog fragment %s: %v", dirEntry.Name(), readErr)
			continue
		}
		if body := chlogFragmentBody(content); body != "" {
			bodies = append(bodies, body)
		}
	}

	return bodies
}

// pendingRemoteChlogEntries is pendingChlogEntries for a repository that has
// not been cloned, reading the fragments through the provider API.
//
// It costs one tree listing plus one fetch per pending fragment, which is why
// it runs only for a repository that both uses chlog and has something to
// record. Like the local reader, a failure yields no entries rather than
// blocking the write.
func pendingRemoteChlogEntries(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	config *entities.ChlogConfig,
) []string {
	files, err := provider.ListFiles(ctx, repo, "")
	if err != nil {
		logger.Warnf("Failed to list the files of %s: %v", entities.RepoKey(repo), err)
		return nil
	}

	prefix := config.UnreleasedPath() + "/"
	var bodies []string
	for _, file := range files {
		// Azure DevOps returns tree paths rooted at "/", the other providers
		// return them relative to the repository root.
		filePath := strings.TrimPrefix(file.Path, "/")
		if file.IsDir || !strings.HasPrefix(filePath, prefix) || !hasChlogFragmentExtension(filePath) {
			continue
		}

		content, contentErr := provider.GetFileContent(ctx, repo, file.Path)
		if contentErr != nil {
			logger.Warnf("Failed to read the chlog fragment %s: %v", filePath, contentErr)
			continue
		}
		if body := chlogFragmentBody([]byte(content)); body != "" {
			bodies = append(bodies, body)
		}
	}

	return bodies
}

// chlogFragmentBody extracts the statement a fragment records. A fragment that
// does not parse contributes nothing: it is not autoupdate's file to interpret,
// and the only cost of ignoring it is a duplicate.
func chlogFragmentBody(content []byte) string {
	var fragment entities.ChlogFragment
	if err := yaml.Unmarshal(content, &fragment); err != nil {
		return ""
	}
	return fragment.Body
}
