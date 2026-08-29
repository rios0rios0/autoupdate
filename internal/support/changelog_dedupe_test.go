package support_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

// goDepsEntry is the bullet the Go updater records on every run that refreshes
// the module dependencies. Its wording never varies, which is what made it pile
// up: an unfiltered insert restated it once a day until the next release.
const goDepsEntry = "- changed the Go module dependencies to their latest versions"

// changelogRecording is a CHANGELOG.md whose [Unreleased] section already
// records goDepsEntry, as it does the day after autoupdate opened a pull
// request that was merged.
const changelogRecording = "# Changelog\n\n" +
	"## [Unreleased]\n\n" +
	"### Changed\n\n" +
	goDepsEntry + "\n\n" +
	"## [1.0.0] - 2026-01-01\n"

func TestLocalChangelogUpdateDeduplication(t *testing.T) {
	t.Parallel()

	t.Run("should not restate an entry the unreleased section already records", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": changelogRecording})

		// when
		updated := support.LocalChangelogUpdate(root, []string{goDepsEntry})

		// then
		assert.False(t, updated)
		assert.Equal(t, changelogRecording, readChangelog(t, root))
	})

	t.Run("should ignore formatting differences when comparing", func(t *testing.T) {
		t.Parallel()

		// given: the same statement, reformatted by hand after it was recorded
		recorded := "# Changelog\n\n## [Unreleased]\n\n### Changed\n\n" +
			"- Changed the Go module dependencies to their\n" +
			"  latest versions\n\n## [1.0.0] - 2026-01-01\n"
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": recorded})

		// when
		updated := support.LocalChangelogUpdate(root, []string{goDepsEntry})

		// then
		assert.False(t, updated)
		assert.Equal(t, recorded, readChangelog(t, root))
	})

	t.Run("should record an entry that states something new", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": changelogRecording})
		entry := "- changed the Docker base image `python` from `3.13` to `3.14`"

		// when
		updated := support.LocalChangelogUpdate(root, []string{entry})

		// then
		assert.True(t, updated)
		assert.Contains(t, readChangelog(t, root), entry)
	})

	t.Run("should record an entry naming a version the section does not", func(t *testing.T) {
		t.Parallel()

		// given: a second, real upgrade of the same image is a new fact, so the
		// comparison is deliberately not fuzzy about version numbers
		recorded := "# Changelog\n\n## [Unreleased]\n\n### Changed\n\n" +
			"- changed the Docker base image `python` from `3.12` to `3.13`\n\n" +
			"## [1.0.0] - 2026-01-01\n"
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": recorded})
		entry := "- changed the Docker base image `python` from `3.13` to `3.14`"

		// when
		updated := support.LocalChangelogUpdate(root, []string{entry})

		// then
		assert.True(t, updated)
		assert.Contains(t, readChangelog(t, root), entry)
	})

	t.Run("should record an entry only a released section states", func(t *testing.T) {
		t.Parallel()

		// given: the statement shipped in 1.0.0, so the dependency moving again
		// is a new fact the changelog is supposed to carry
		recorded := "# Changelog\n\n## [Unreleased]\n\n" +
			"## [1.0.0] - 2026-01-01\n\n### Changed\n\n" + goDepsEntry + "\n"
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": recorded})

		// when
		updated := support.LocalChangelogUpdate(root, []string{goDepsEntry})

		// then
		assert.True(t, updated)
		assert.Equal(t, 2, strings.Count(readChangelog(t, root), goDepsEntry))
	})

	t.Run("should collapse repeats inside a single call", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": baseChangelog})
		entry := "- changed the Docker base image `alpine` from `3.19` to `3.20`"

		// when
		updated := support.LocalChangelogUpdate(root, []string{entry, entry})

		// then
		assert.True(t, updated)
		assert.Equal(t, 1, strings.Count(readChangelog(t, root), entry))
	})

	t.Run("should keep the entries a second updater adds on the same branch", func(t *testing.T) {
		t.Parallel()

		// given: batch mode runs every updater against one clone, so the second
		// reads the file the first already wrote
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": baseChangelog})
		dockerEntry := "- changed the Docker base image `python` from `3.13` to `3.14`"

		// when
		require.True(t, support.LocalChangelogUpdate(root, []string{goDepsEntry}))
		updated := support.LocalChangelogUpdate(root, []string{goDepsEntry, dockerEntry})

		// then
		assert.True(t, updated)
		changelog := readChangelog(t, root)
		assert.Equal(t, 1, strings.Count(changelog, goDepsEntry))
		assert.Contains(t, changelog, dockerEntry)
	})
}

func TestStageLocalChangelogDeduplication(t *testing.T) {
	t.Parallel()

	t.Run("should stage nothing when the entry is already recorded", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": changelogRecording})

		// when
		staged := support.StageLocalChangelog(root, []string{goDepsEntry})
		defer staged.Remove()

		// then
		assert.True(t, staged.IsEmpty())
	})

	t.Run("should stage an entry that states something new", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": changelogRecording})

		// when
		staged := support.StageLocalChangelog(root,
			[]string{"- changed the Python dependencies to their latest versions"})
		defer staged.Remove()

		// then
		require.False(t, staged.IsEmpty())
		assert.Equal(t, "CHANGELOG.md", staged.RepoPath)
	})
}

func TestChlogFragmentDeduplication(t *testing.T) {
	t.Parallel()

	t.Run("should not file a fragment for a statement already pending", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{".changes/unreleased/": ""})
		require.True(t, support.LocalChangelogUpdate(root, []string{goDepsEntry}))

		// when
		updated := support.LocalChangelogUpdate(root, []string{goDepsEntry})

		// then
		assert.False(t, updated)
		assert.Len(t, readChlogFragments(t, root, ".changes/unreleased"), 1)
	})

	t.Run("should file a fragment for a statement not pending yet", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{".changes/unreleased/": ""})
		require.True(t, support.LocalChangelogUpdate(root, []string{goDepsEntry}))

		// when
		updated := support.LocalChangelogUpdate(root,
			[]string{"- changed the Docker base image `python` from `3.13` to `3.14`"})

		// then
		assert.True(t, updated)
		assert.Len(t, readChlogFragments(t, root, ".changes/unreleased"), 2)
	})

	t.Run("should stage nothing when the statement is already pending", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{".changes/unreleased/": ""})
		require.True(t, support.LocalChangelogUpdate(root, []string{goDepsEntry}))

		// when
		staged := support.StageLocalChangelog(root, []string{goDepsEntry})
		defer staged.Remove()

		// then
		assert.True(t, staged.IsEmpty())
	})
}

func TestRemoteChangelogChangesDeduplication(t *testing.T) {
	t.Parallel()

	t.Run("should leave the change set untouched when the entry is recorded", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": changelogRecording}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		changes := support.RemoteChangelogChanges(
			t.Context(), provider, repo, []string{goDepsEntry}, nil)

		// then
		assert.Empty(t, changes)
	})

	t.Run("should add the edited changelog when the entry is new", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": changelogRecording}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		entry := "- changed the Python dependencies to their latest versions"

		// when
		changes := support.RemoteChangelogChanges(
			t.Context(), provider, repo, []string{entry}, nil)

		// then
		require.Len(t, changes, 1)
		assert.Contains(t, changes[0].Content, entry)
	})
}

// readChangelog returns the current content of the repository's CHANGELOG.md.
func readChangelog(t *testing.T, root string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	require.NoError(t, err)

	return string(content)
}
