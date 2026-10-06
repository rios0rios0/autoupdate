package support

import (
	"os"
	"slices"
	"strings"
	"time"

	logger "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// recordChlogChanges records plain entries and dependency changes in a chlog
// repository, returning true when a fragment was written or removed.
//
// A dependency some pending fragment already names is merged into that
// fragment, which keeps its file, kind and time -- chlog orders fragments by
// time, so a rewritten one stays where the release will list it. Everything
// else becomes a new fragment, one per statement.
func recordChlogChanges(
	repoDir string,
	config *entities.ChlogConfig,
	plain []string,
	changes []entities.DependencyChange,
) bool {
	fragments := readPendingChlogFragments(repoDir, config)

	statements := make([]pendingStatement, len(fragments))
	changedKind := config.KindLabel(entities.ChlogUpdateKind)
	for i, fragment := range fragments {
		statements[i].preferred = !strings.EqualFold(fragment.kind, changedKind)
		if fragment.editable {
			statements[i].items, _ = entities.ParseDependencyEntry(fragment.body)
		}
	}

	merge := mergeDependencyChanges(statements, changes)
	written := rewritePendingFragments(fragments, merge.rewrites)

	additions := append(slices.Clone(plain), entities.RenderDependencyEntries(merge.additions)...)
	if writeNewChlogFragments(repoDir, config, newChangelogEntries(fragmentBodies(fragments), additions)) {
		written = true
	}
	return written
}

// rewritePendingFragments applies the merge's rewrites to pending fragments: a
// fragment left naming nothing is removed, any other is rewritten in chlog's
// own shape with its kind and time unchanged.
func rewritePendingFragments(fragments []pendingFragment, rewrites map[int]string) bool {
	written := false
	for index, body := range rewrites {
		fragment := fragments[index]

		if body == "" {
			// fragment.path came from listing the validated unreleased
			// directory, and readPendingChlogFragments only keeps regular files.
			if err := os.Remove(fragment.path); err != nil {
				logger.Warnf("Failed to remove the chlog fragment %s: %v", fragment.name, err)
				continue
			}
			written = true
			continue
		}

		content, err := yaml.Marshal(&entities.ChlogFragment{Kind: fragment.kind, Body: body, Time: fragment.time})
		if err != nil {
			logger.Warnf("Failed to render the chlog fragment %s: %v", fragment.name, err)
			continue
		}
		if err = os.WriteFile(fragment.path, content, 0o600); err != nil {
			logger.Warnf("Failed to rewrite the chlog fragment %s: %v", fragment.name, err)
			continue
		}
		written = true
	}

	if written {
		logger.Infof("Updated %d pending chlog fragment(s) in place", len(rewrites))
	}
	return written
}

// writeNewChlogFragments writes one fragment per entry under the repository's
// unreleased directory, creating it when a .chlog.yaml declares a directory that
// does not exist yet.
//
// Each fragment is a nanosecond older than the one before it. chlog lists the
// newest first, so the statements come out at release in the order they were
// written, and no two fragments of one run share a file name prefix.
func writeNewChlogFragments(repoDir string, config *entities.ChlogConfig, entries []string) bool {
	if len(entries) == 0 {
		return false
	}

	unreleasedDir := entities.ChlogFragmentDiskPath(repoDir, config.UnreleasedPath())
	// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
	if err := os.MkdirAll(unreleasedDir, entities.ChlogFragmentDirMode); err != nil {
		logger.Warnf("Failed to create the chlog fragment directory %s: %v", unreleasedDir, err)
		return false
	}

	now := time.Now()
	written := 0
	for i, entry := range entries {
		fragments, err := config.NewChlogFragments([]string{entry}, now.Add(-time.Duration(i)))
		if err != nil {
			logger.Warnf("Failed to build the chlog fragment: %v", err)
			continue
		}

		for _, fragment := range fragments {
			fragmentPath := entities.ChlogFragmentDiskPath(repoDir, fragment.Path)

			// path is validated against escaping the repository root
			if err = os.WriteFile(fragmentPath, []byte(fragment.Content), 0o600); err != nil {
				logger.Warnf("Failed to write the chlog fragment %s: %v", fragmentPath, err)
				continue
			}
			written++
		}
	}

	if written > 0 {
		logger.Infof("Recorded %d chlog fragment(s) in %s", written, config.UnreleasedPath())
	}
	return written > 0
}
