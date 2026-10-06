package support

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	logger "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// editableFragmentExtension is the only suffix chlog compiles at release time.
// A ".yml" fragment still counts as pending, but a statement merged into one
// would never reach a release, so autoupdate never edits it.
const editableFragmentExtension = ".yaml"

// chlogFragmentKeys are the keys of a fragment as chlog writes it. A fragment
// carrying any other key -- `breaking`, or one autoupdate does not know -- was
// shaped by hand, and rewriting it through ChlogFragment would drop that key.
//
//nolint:gochecknoglobals // read-only lookup table
var chlogFragmentKeys = map[string]bool{"kind": true, "body": true, "time": true}

// pendingFragment is one fragment waiting under the unreleased directory.
type pendingFragment struct {
	path string
	name string
	kind string
	body string
	time time.Time
	// editable reports whether autoupdate may rewrite or remove the fragment:
	// a regular ".yaml" file holding exactly the keys chlog writes.
	editable bool
}

// readPendingChlogFragments reads the fragments a repository has waiting under
// its unreleased directory, oldest first.
//
// Only regular files are read. In batch mode the repository is input autoupdate
// does not own, and a fragment that is a symbolic link would have the merge
// read, and rewrite, a file outside it. A directory that cannot be read yields
// no fragments rather than an error: the worst case is a statement filed twice,
// whereas refusing to write would drop a real changelog entry.
func readPendingChlogFragments(repoDir string, config *entities.ChlogConfig) []pendingFragment {
	unreleasedDir := entities.ChlogFragmentDiskPath(repoDir, config.UnreleasedPath())

	dirEntries, err := os.ReadDir(unreleasedDir)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warnf("Failed to read the chlog fragment directory %s: %v", unreleasedDir, err)
		}
		return nil
	}

	fragments := make([]pendingFragment, 0, len(dirEntries))
	for _, dirEntry := range dirEntries {
		if !dirEntry.Type().IsRegular() || !hasChlogFragmentExtension(dirEntry.Name()) {
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

		fragment := parsePendingFragment(content)
		fragment.path, fragment.name = fragmentPath, dirEntry.Name()
		fragment.editable = fragment.editable &&
			strings.HasSuffix(dirEntry.Name(), editableFragmentExtension)
		fragments = append(fragments, fragment)
	}

	sort.SliceStable(fragments, func(i, j int) bool {
		if !fragments[i].time.Equal(fragments[j].time) {
			return fragments[i].time.Before(fragments[j].time)
		}
		return fragments[i].name < fragments[j].name
	})
	return fragments
}

// parsePendingFragment decodes a fragment. One that does not parse is still
// pending -- the release check counts it -- but has no statement to compare and
// is never edited.
func parsePendingFragment(content []byte) pendingFragment {
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return pendingFragment{}
	}

	var fragment entities.ChlogFragment
	if err := document.Decode(&fragment); err != nil {
		return pendingFragment{}
	}

	return pendingFragment{
		kind:     fragment.Kind,
		body:     fragment.Body,
		time:     fragment.Time,
		editable: hasOnlyChlogKeys(&document),
	}
}

// hasOnlyChlogKeys reports whether a decoded fragment holds exactly the keys
// chlog writes, each once.
func hasOnlyChlogKeys(document *yaml.Node) bool {
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return false
	}

	mapping := document.Content[0]
	if mapping.Kind != yaml.MappingNode || len(mapping.Content) != 2*len(chlogFragmentKeys) {
		return false
	}

	seen := make(map[string]bool, len(chlogFragmentKeys))
	for i := 0; i < len(mapping.Content); i += 2 {
		key := mapping.Content[i].Value
		if !chlogFragmentKeys[key] || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

// fragmentBodies returns the statements the fragments make, skipping the ones
// that carry none.
func fragmentBodies(fragments []pendingFragment) []string {
	bodies := make([]string, 0, len(fragments))
	for _, fragment := range fragments {
		if fragment.body != "" {
			bodies = append(bodies, fragment.body)
		}
	}
	return bodies
}

// pendingRemoteChlogEntries reads the bodies of the fragments a repository
// that has not been cloned already has waiting, through the provider API, so
// the same statement is not filed twice. The duplicate check has to reach both
// formats or they drift apart: a chlog repository would keep collecting a fresh
// fragment per run saying exactly what the last run's fragment says.
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
		if body := parsePendingFragment([]byte(content)).body; body != "" {
			bodies = append(bodies, body)
		}
	}

	return bodies
}
