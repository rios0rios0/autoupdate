package support

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// errDependencyObserverPanicked reports a dependency reader that panicked
// instead of returning an error.
var errDependencyObserverPanicked = errors.New("the dependency reader panicked")

// DependencyObserver reports the declared dependencies a run moved, by comparing
// what the repository declared at HEAD with what its working tree declares now.
//
// It returns (nil, nil) only when every file it owns was read on both sides
// and no declared dependency moved. A file in a format it cannot read is an
// error, never an empty result: the caller falls back to a generic statement
// for an error, whereas an empty result would quietly record nothing.
type DependencyObserver func(ctx context.Context, repoDir string) ([]entities.DependencyChange, error)

// LocalDependencyChangelogUpdate records the dependencies a run moved in a
// repository on disk, returning true when a file was written or removed.
//
// A dependency the pending section already names is updated where it is
// instead of being stated again, and the rest are written as new lines naming
// at most [entities.MaxDependenciesPerEntry] dependencies each. See
// [mergeDependencyChanges] for the rules.
func LocalDependencyChangelogUpdate(repoDir string, changes []entities.DependencyChange) bool {
	folded := FoldDependencyChanges(changes)
	if len(folded) == 0 {
		return false
	}
	return recordLocalChangelog(repoDir, nil, folded)
}

// RecordObservedDependencyChanges records what an upgrade moved, as the
// observer reads it from the repository.
//
// The observer's answer decides the statement:
//
//   - a list of changes names each dependency, merged into what is pending;
//   - an error, or a panic, records fallback -- the generic statement that names
//     nothing -- so a reader that cannot parse a file never costs the entry;
//   - nothing moved records summary, and only when the repository has nothing
//     pending at all. A run that moved only transitive dependencies has nothing
//     to name, but the shared pipelines fail an autoupdate pull request that
//     adds no changelog change while none is pending, so a run straight after a
//     release would otherwise open a pull request that cannot pass.
func RecordObservedDependencyChanges(
	ctx context.Context,
	repoDir string,
	observe DependencyObserver,
	fallback, summary string,
) bool {
	changes, err := observeDependencyChanges(ctx, repoDir, observe)
	if err != nil {
		logger.Warnf("Could not tell which dependencies moved, recording the upgrade generically: %v", err)
		return LocalChangelogUpdate(repoDir, []string{fallback})
	}

	if len(FoldDependencyChanges(changes)) > 0 {
		return LocalDependencyChangelogUpdate(repoDir, changes)
	}

	if hasPendingChangelogEntries(repoDir) {
		logger.Info("No declared dependency moved, and the changelog already records pending entries")
		return false
	}
	return LocalChangelogUpdate(repoDir, []string{summary})
}

// observeDependencyChanges runs the observer, turning a panic into an error.
// Batch mode processes repositories concurrently, and a panic escaping one
// reader would end the run for every repository still in flight.
func observeDependencyChanges(
	ctx context.Context,
	repoDir string,
	observe DependencyObserver,
) ([]entities.DependencyChange, error) {
	var (
		changes []entities.DependencyChange
		err     error
	)

	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("%w: %v", errDependencyObserverPanicked, recovered)
			}
		}()
		changes, err = observe(ctx, repoDir)
	}()

	return changes, err
}

// hasPendingChangelogEntries reports whether a repository already has entries
// waiting to be released: a fragment under the chlog unreleased directory, or a
// bullet under [Unreleased]. A repository whose changelog cannot be read or
// written counts as having some, since nothing could be added to it anyway.
func hasPendingChangelogEntries(repoDir string) bool {
	config, usesChlog, err := DetectLocalChlog(repoDir)
	if err != nil {
		return true
	}
	if usesChlog {
		return len(readPendingChlogFragments(repoDir, config)) > 0
	}

	data, err := os.ReadFile(filepath.Clean(filepath.Join(repoDir, ChangelogFileName)))
	if err != nil {
		return true
	}

	doc := parseChangelogDocument(string(data))
	return doc.start < 0 || len(doc.bullets) > 0
}

// recordLocalChangelog writes plain entries and dependency changes to a
// repository on disk in whichever format it uses, detecting the format and
// reading what is pending once for both.
func recordLocalChangelog(repoDir string, plain []string, changes []entities.DependencyChange) bool {
	config, usesChlog, err := DetectLocalChlog(repoDir)
	if err != nil {
		logger.Warnf("Failed to detect chlog in %s, leaving the changelog untouched: %v", repoDir, err)
		return false
	}

	if usesChlog {
		return recordChlogChanges(repoDir, config, plain, changes)
	}
	return recordKeepAChangelogChanges(repoDir, plain, changes)
}
