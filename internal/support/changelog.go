package support

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// ChangelogFileName is the Keep a Changelog document autoupdate appends to when
// a repository does not use chlog.
const ChangelogFileName = "CHANGELOG.md"

// StagedChangelog is a changelog payload staged in a temporary file, waiting to
// be copied into a cloned repository by a generated upgrade script.
//
// The script-driven updaters (Go, Python, JavaScript, Ruby, Java, C#) do the
// clone themselves, so they cannot write the file directly; they hand the pair
// to bash through the CHANGELOG_FILE and CHANGELOG_DEST environment variables.
// Carrying the destination alongside the content is what lets the same script
// serve both formats: a Keep a Changelog run copies to CHANGELOG.md, a chlog
// run copies to .changes/unreleased/<fragment>.yaml.
type StagedChangelog struct {
	// TempPath is the host path holding the content, or "" when there is
	// nothing to write.
	TempPath string
	// RepoPath is the repository-relative destination, using forward slashes.
	RepoPath string
	// Fragment reports whether RepoPath is a new chlog fragment rather than an
	// edit of an existing changelog. It decides how an abandoned run is cleaned
	// up: git restores an edited file, but a created one has to be deleted.
	Fragment bool
}

// IsEmpty reports whether nothing was staged, in which case the script must not
// touch the changelog at all.
func (s StagedChangelog) IsEmpty() bool {
	return s.TempPath == ""
}

// Remove deletes the temporary file. It is safe to call on an empty result.
func (s StagedChangelog) Remove() {
	if s.TempPath != "" {
		_ = os.Remove(s.TempPath)
	}
}

// Discard removes the copy the script placed in repoDir, for a run the caller
// decided to abandon.
//
// Only a chlog fragment needs it: an edited CHANGELOG.md is a tracked file that
// the caller's "git checkout" restores, whereas a fragment is a new untracked
// file that would survive the checkout and be left behind in the repository.
func (s StagedChangelog) Discard(repoDir string) {
	if s.IsEmpty() || !s.Fragment {
		return
	}

	path := entities.ChlogFragmentDiskPath(repoDir, s.RepoPath)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		logger.Warnf("Failed to discard the chlog fragment %s: %v", path, err)
	}
}

// Env returns the environment entries ChangelogUpdateScript reads. An empty
// result contributes nothing, which leaves CHANGELOG_FILE unset and makes the
// script skip the copy.
func (s StagedChangelog) Env() []string {
	if s.IsEmpty() {
		return nil
	}
	return []string{
		"CHANGELOG_FILE=" + s.TempPath,
		"CHANGELOG_DEST=" + s.RepoPath,
	}
}

// ChangelogUpdateScript is the bash fragment that copies a staged changelog
// into the repository the upgrade script has cloned.
//
// The destination comes from the environment rather than being hard-coded, so
// the same fragment serves both formats: a Keep a Changelog run copies over
// CHANGELOG.md, a chlog run creates a new file under the unreleased directory.
// The copy is skipped when the upgrade produced no other change, which is what
// prevents pull requests that only touch the changelog.
func ChangelogUpdateScript() string {
	return `# Update the changelog only if the upgrade produced actual changes.
# This prevents creating empty PRs that only touch the changelog.
if [ -n "${CHANGELOG_FILE:-}" ] && [ -f "$CHANGELOG_FILE" ]; then
    if [ -n "$(git status --porcelain)" ]; then
        CHANGELOG_DEST="${CHANGELOG_DEST:-` + ChangelogFileName + `}"
        echo "Updating $CHANGELOG_DEST..."
        mkdir -p "$(dirname "$CHANGELOG_DEST")"
        cp "$CHANGELOG_FILE" "$CHANGELOG_DEST"
    else
        echo "No dependency changes detected, skipping the changelog update."
    fi
fi

`
}

// LocalChangelogUpdate records the given Keep a Changelog bullet entries in a
// repository on disk. Returns true when a file was written.
//
// A repository using chlog gets one fragment per entry under its unreleased
// directory; every other repository gets the entries inserted into the
// [Unreleased] section of CHANGELOG.md, as before.
func LocalChangelogUpdate(repoDir string, entries []string) bool {
	if len(entries) == 0 {
		return false
	}

	config, usesChlog, err := DetectLocalChlog(repoDir)
	if err != nil {
		logger.Warnf("Failed to detect chlog in %s, leaving the changelog untouched: %v", repoDir, err)
		return false
	}
	if usesChlog {
		return writeLocalChlogFragments(repoDir, config, entries)
	}

	return insertLocalChangelogEntries(repoDir, entries)
}

// writeLocalChlogFragments renders the entries as chlog fragments and writes
// them under the repository's unreleased directory, creating it when a
// .chlog.yaml declares a directory that does not exist yet.
func writeLocalChlogFragments(
	repoDir string,
	config *entities.ChlogConfig,
	entries []string,
) bool {
	fragments, err := config.NewChlogFragments(entries, time.Now())
	if err != nil {
		logger.Warnf("Failed to build the chlog fragments: %v", err)
		return false
	}
	if len(fragments) == 0 {
		return false
	}

	unreleasedDir := entities.ChlogFragmentDiskPath(repoDir, config.UnreleasedPath())
	// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
	if err = os.MkdirAll(unreleasedDir, entities.ChlogFragmentDirMode); err != nil {
		logger.Warnf("Failed to create the chlog fragment directory %s: %v", unreleasedDir, err)
		return false
	}

	written := false
	for _, fragment := range fragments {
		fragmentPath := entities.ChlogFragmentDiskPath(repoDir, fragment.Path)

		// path is validated against escaping the repository root
		if err = os.WriteFile(fragmentPath, []byte(fragment.Content), 0o600); err != nil {
			logger.Warnf("Failed to write the chlog fragment %s: %v", fragmentPath, err)
			continue
		}
		written = true
	}

	if written {
		logger.Infof("Recorded %d chlog fragment(s) in %s", len(fragments), config.UnreleasedPath())
	}
	return written
}

// insertLocalChangelogEntries appends the entries to the [Unreleased] section of
// CHANGELOG.md on disk. A repository without a changelog is left alone.
func insertLocalChangelogEntries(repoDir string, entries []string) bool {
	changelogPath := filepath.Clean(filepath.Join(repoDir, ChangelogFileName))
	data, err := os.ReadFile(changelogPath)
	if err != nil {
		logger.Warnf("Failed to read %s: %v", ChangelogFileName, err)
		return false
	}

	content := string(data)
	modified := entities.InsertChangelogEntry(content, entries)
	if modified == content {
		return false
	}

	writeErr := os.WriteFile( //nolint:gosec // repoDir is a controlled internal path
		changelogPath,
		[]byte(modified),
		0o600,
	)
	if writeErr != nil {
		logger.Warnf("Failed to write %s: %v", ChangelogFileName, writeErr)
		return false
	}
	return true
}

// RemoteChangelogChanges builds the file changes that record the given entries
// in a repository reachable through the provider API, appending them to the
// change set the caller is assembling.
//
// A repository using chlog gets one added fragment per entry; every other
// repository gets an edited CHANGELOG.md. A repository with neither chlog nor a
// changelog gets nothing, leaving the change set untouched.
func RemoteChangelogChanges(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	entries []string,
	fileChanges []entities.FileChange,
) []entities.FileChange {
	if len(entries) == 0 {
		return fileChanges
	}

	config, usesChlog, err := DetectRemoteChlog(ctx, provider, repo)
	if err != nil {
		logger.Warnf("Failed to detect chlog in %s, leaving the changelog untouched: %v",
			entities.RepoKey(repo), err)
		return fileChanges
	}

	if usesChlog {
		fragments, fragErr := config.NewChlogFragments(entries, time.Now())
		if fragErr != nil {
			logger.Warnf("Failed to build the chlog fragments: %v", fragErr)
			return fileChanges
		}
		return append(fileChanges, fragments...)
	}

	change, ok := remoteChangelogEdit(ctx, provider, repo, entries)
	if !ok {
		return fileChanges
	}
	return append(fileChanges, change)
}

// remoteChangelogEdit fetches CHANGELOG.md through the provider and returns the
// edited file. The boolean is false when the repository has no changelog, the
// fetch fails, or the insertion changed nothing.
func remoteChangelogEdit(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	entries []string,
) (entities.FileChange, bool) {
	if !provider.HasFile(ctx, repo, ChangelogFileName) {
		return entities.FileChange{}, false
	}

	content, err := provider.GetFileContent(ctx, repo, ChangelogFileName)
	if err != nil {
		logger.Warnf("Failed to read %s: %v", ChangelogFileName, err)
		return entities.FileChange{}, false
	}

	modified := entities.InsertChangelogEntry(content, entries)
	if modified == content {
		return entities.FileChange{}, false
	}

	return entities.FileChange{
		Path:       ChangelogFileName,
		Content:    modified,
		ChangeType: "edit",
	}, true
}

// StageLocalChangelog prepares the changelog payload for a repository on disk
// without writing it into the repository, for the updaters whose upgrade runs
// through a generated script that performs the copy itself.
//
// The caller must Remove the result once the script has run.
func StageLocalChangelog(repoDir string, entries []string) StagedChangelog {
	if len(entries) == 0 {
		return StagedChangelog{}
	}

	config, usesChlog, err := DetectLocalChlog(repoDir)
	if err != nil {
		logger.Warnf("Failed to detect chlog in %s, leaving the changelog untouched: %v", repoDir, err)
		return StagedChangelog{}
	}
	if usesChlog {
		return stageChlogFragment(config, entries)
	}

	content, err := os.ReadFile(filepath.Join(repoDir, ChangelogFileName))
	if err != nil {
		return StagedChangelog{} // no changelog present
	}
	return stageChangelogEdit(string(content), entries)
}

// StageRemoteChangelog is StageLocalChangelog for a repository that has not been
// cloned yet: the current content is fetched through the provider API, and the
// generated script copies the staged file into the clone it makes itself.
//
// The caller must Remove the result once the script has run.
func StageRemoteChangelog(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	entries []string,
) StagedChangelog {
	if len(entries) == 0 {
		return StagedChangelog{}
	}

	config, usesChlog, err := DetectRemoteChlog(ctx, provider, repo)
	if err != nil {
		logger.Warnf("Failed to detect chlog in %s, leaving the changelog untouched: %v",
			entities.RepoKey(repo), err)
		return StagedChangelog{}
	}
	if usesChlog {
		return stageChlogFragment(config, entries)
	}

	if !provider.HasFile(ctx, repo, ChangelogFileName) {
		return StagedChangelog{}
	}

	content, err := provider.GetFileContent(ctx, repo, ChangelogFileName)
	if err != nil {
		logger.Warnf("Failed to read %s: %v", ChangelogFileName, err)
		return StagedChangelog{}
	}
	return stageChangelogEdit(content, entries)
}

// stageChlogFragment writes the entries to a temporary fragment file.
//
// The script copies exactly one file, so the entries are merged into a single
// fragment. Every script-driven updater passes a single entry anyway -- the
// per-dependency wording belongs to the updaters that build the change set
// themselves -- so nothing is lost, and a multi-line body is what chlog renders
// as one bullet with its continuation lines.
func stageChlogFragment(config *entities.ChlogConfig, entries []string) StagedChangelog {
	fragments, err := config.NewChlogFragments([]string{joinChangelogEntries(entries)}, time.Now())
	if err != nil {
		logger.Warnf("Failed to build the chlog fragment: %v", err)
		return StagedChangelog{}
	}
	if len(fragments) == 0 {
		return StagedChangelog{}
	}

	tempPath, err := writeTempChangelog(fragments[0].Content, "autoupdate-chlog-*.yaml")
	if err != nil {
		logger.Warnf("Failed to stage the chlog fragment: %v", err)
		return StagedChangelog{}
	}

	return StagedChangelog{TempPath: tempPath, RepoPath: fragments[0].Path, Fragment: true}
}

// stageChangelogEdit writes the edited CHANGELOG.md to a temporary file. An
// insertion that changed nothing stages nothing, so the script does not produce
// a commit that only rewrites the changelog identically.
func stageChangelogEdit(content string, entries []string) StagedChangelog {
	modified := entities.InsertChangelogEntry(content, entries)
	if modified == content {
		return StagedChangelog{}
	}

	tempPath, err := writeTempChangelog(modified, "autoupdate-changelog-*.md")
	if err != nil {
		logger.Warnf("Failed to stage %s: %v", ChangelogFileName, err)
		return StagedChangelog{}
	}

	return StagedChangelog{TempPath: tempPath, RepoPath: ChangelogFileName}
}

// joinChangelogEntries collapses multiple bullets into one fragment body,
// keeping each on its own line so chlog renders them as a bullet with nested
// continuation lines.
func joinChangelogEntries(entries []string) string {
	bodies := make([]string, 0, len(entries))
	for _, entry := range entries {
		if body := entities.StripBulletPrefix(entry); body != "" {
			bodies = append(bodies, body)
		}
	}
	return strings.Join(bodies, "\n")
}

// writeTempChangelog writes content to a new temporary file and returns its path.
func writeTempChangelog(content, pattern string) (string, error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("failed to create the temporary file: %w", err)
	}

	if _, err = file.WriteString(content); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("failed to write the temporary file: %w", err)
	}
	_ = file.Close()

	return file.Name(), nil
}
