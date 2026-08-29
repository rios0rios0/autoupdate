package support_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

const baseChangelog = "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n"

func TestLocalChangelogUpdateWithChlog(t *testing.T) {
	t.Parallel()

	t.Run("should write a fragment instead of touching CHANGELOG.md", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{
			".chlog.yaml":  "changesDir: .changes\n",
			"CHANGELOG.md": baseChangelog,
		})

		// when
		updated := support.LocalChangelogUpdate(root,
			[]string{"- changed the Go module dependencies to their latest versions"})

		// then
		assert.True(t, updated)

		fragments := readChlogFragments(t, root, ".changes/unreleased")
		require.Len(t, fragments, 1)

		var fragment entities.ChlogFragment
		require.NoError(t, yaml.Unmarshal([]byte(fragments[0]), &fragment))
		assert.Equal(t, "Changed", fragment.Kind)
		assert.Equal(t, "changed the Go module dependencies to their latest versions", fragment.Body)

		changelog, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
		require.NoError(t, err)
		assert.Equal(t, baseChangelog, string(changelog),
			"the changelog chlog compiles into must be left untouched")
	})

	t.Run("should write one fragment per entry", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{".changes/unreleased/": ""})

		// when
		updated := support.LocalChangelogUpdate(root, []string{
			"- changed the Docker base image `alpine` from `3.19` to `3.20`",
			"- changed the Docker base image `golang` from `1.24` to `1.25`",
		})

		// then
		assert.True(t, updated)
		assert.Len(t, readChlogFragments(t, root, ".changes/unreleased"), 2)
	})

	t.Run("should create the unreleased directory the configuration declares", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{
			".chlog.yaml": "changesDir: docs/changes\nunreleasedDir: pending\n",
		})

		// when
		updated := support.LocalChangelogUpdate(root, []string{"- changed a dependency"})

		// then
		assert.True(t, updated)
		assert.Len(t, readChlogFragments(t, root, "docs/changes/pending"), 1)
	})

	t.Run("should leave the repository untouched when the configuration is broken", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{
			".chlog.yaml":  "changesDir: ../../etc\n",
			"CHANGELOG.md": baseChangelog,
		})

		// when
		updated := support.LocalChangelogUpdate(root, []string{"- changed a dependency"})

		// then
		assert.False(t, updated)

		changelog, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
		require.NoError(t, err)
		assert.Equal(t, baseChangelog, string(changelog))
	})

	t.Run("should still edit CHANGELOG.md when the repository does not use chlog", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": baseChangelog})

		// when
		updated := support.LocalChangelogUpdate(root, []string{"- changed a dependency"})

		// then
		assert.True(t, updated)

		changelog, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
		require.NoError(t, err)
		assert.Contains(t, string(changelog), "- changed a dependency")
	})

	t.Run("should do nothing when there are no entries", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{".changes/unreleased/": ""})

		// when
		updated := support.LocalChangelogUpdate(root, nil)

		// then
		assert.False(t, updated)
		assert.Empty(t, readChlogFragments(t, root, ".changes/unreleased"))
	})
}

func TestRemoteChangelogChanges(t *testing.T) {
	t.Parallel()

	repo := entities.Repository{Organization: "org", Name: "repo"}

	t.Run("should add a fragment per entry when the repository uses chlog", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".chlog.yaml": true, "CHANGELOG.md": true}).
			WithFileContents(map[string]string{
				".chlog.yaml":  "changesDir: .changes\n",
				"CHANGELOG.md": baseChangelog,
			}).
			BuildSpy()
		entries := []string{"- changed dependency `a`", "- changed dependency `b`"}

		// when
		changes := support.RemoteChangelogChanges(t.Context(), provider, repo, entries, nil)

		// then
		require.Len(t, changes, 2)
		for _, change := range changes {
			assert.Equal(t, "add", change.ChangeType)
			assert.True(t, strings.HasPrefix(change.Path, ".changes/unreleased/"), change.Path)
			assert.NotContains(t, change.Path, "CHANGELOG.md")
		}
	})

	t.Run("should edit CHANGELOG.md when the repository does not use chlog", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": baseChangelog}).
			BuildSpy()

		// when
		changes := support.RemoteChangelogChanges(
			t.Context(), provider, repo, []string{"- changed dependency `a`"}, nil)

		// then
		require.Len(t, changes, 1)
		assert.Equal(t, "CHANGELOG.md", changes[0].Path)
		assert.Equal(t, "edit", changes[0].ChangeType)
		assert.Contains(t, changes[0].Content, "- changed dependency `a`")
	})

	t.Run("should keep the existing changes when the repository has no changelog", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		existing := []entities.FileChange{{Path: "go.mod", ChangeType: "edit"}}

		// when
		changes := support.RemoteChangelogChanges(
			t.Context(), provider, repo, []string{"- changed dependency `a`"}, existing)

		// then
		assert.Equal(t, existing, changes)
	})

	t.Run("should keep the existing changes when detection fails", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".chlog.yaml": true}).
			WithFileContents(map[string]string{".chlog.yaml": "changesDir: /etc\n"}).
			BuildSpy()
		existing := []entities.FileChange{{Path: "go.mod", ChangeType: "edit"}}

		// when
		changes := support.RemoteChangelogChanges(
			t.Context(), provider, repo, []string{"- changed dependency `a`"}, existing)

		// then
		assert.Equal(t, existing, changes)
	})
}

func TestStageLocalChangelog(t *testing.T) {
	t.Parallel()

	t.Run("should stage a fragment aimed at the unreleased directory", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{".chlog.yaml": "changesDir: .changes\n"})

		// when
		staged := support.StageLocalChangelog(root, []string{"- changed a dependency"})
		defer staged.Remove()

		// then
		require.False(t, staged.IsEmpty())
		assert.True(t, strings.HasPrefix(staged.RepoPath, ".changes/unreleased/"), staged.RepoPath)

		content, err := os.ReadFile(staged.TempPath)
		require.NoError(t, err)

		var fragment entities.ChlogFragment
		require.NoError(t, yaml.Unmarshal(content, &fragment))
		assert.Equal(t, "Changed", fragment.Kind)
		assert.Equal(t, "changed a dependency", fragment.Body)
	})

	t.Run("should merge several entries into one fragment body", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{".changes/unreleased/": ""})

		// when
		staged := support.StageLocalChangelog(root, []string{"- changed `a`", "- changed `b`"})
		defer staged.Remove()

		// then
		require.False(t, staged.IsEmpty())

		content, err := os.ReadFile(staged.TempPath)
		require.NoError(t, err)

		var fragment entities.ChlogFragment
		require.NoError(t, yaml.Unmarshal(content, &fragment))
		assert.Equal(t, "changed `a`\nchanged `b`", fragment.Body)
	})

	t.Run("should stage the edited CHANGELOG.md when the repository does not use chlog", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": baseChangelog})

		// when
		staged := support.StageLocalChangelog(root, []string{"- changed a dependency"})
		defer staged.Remove()

		// then
		require.False(t, staged.IsEmpty())
		assert.Equal(t, "CHANGELOG.md", staged.RepoPath)

		content, err := os.ReadFile(staged.TempPath)
		require.NoError(t, err)
		assert.Contains(t, string(content), "- changed a dependency")
	})

	t.Run("should stage nothing when the repository has neither chlog nor a changelog", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{"go.mod": "module example\n"})

		// when
		staged := support.StageLocalChangelog(root, []string{"- changed a dependency"})

		// then
		assert.True(t, staged.IsEmpty())
		assert.Empty(t, staged.Env())
	})
}

func TestStageRemoteChangelog(t *testing.T) {
	t.Parallel()

	repo := entities.Repository{Organization: "org", Name: "repo"}

	t.Run("should stage a fragment when the repository uses chlog", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".chlog.yaml": true}).
			WithFileContents(map[string]string{".chlog.yaml": "unreleasedDir: pending\n"}).
			BuildSpy()

		// when
		staged := support.StageRemoteChangelog(
			t.Context(), provider, repo, []string{"- changed a dependency"})
		defer staged.Remove()

		// then
		require.False(t, staged.IsEmpty())
		assert.True(t, strings.HasPrefix(staged.RepoPath, ".changes/pending/"), staged.RepoPath)
		assert.Equal(t, []string{
			"CHANGELOG_FILE=" + staged.TempPath,
			"CHANGELOG_DEST=" + staged.RepoPath,
		}, staged.Env())
	})

	t.Run("should stage the edited CHANGELOG.md when the repository does not use chlog", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": baseChangelog}).
			BuildSpy()

		// when
		staged := support.StageRemoteChangelog(
			t.Context(), provider, repo, []string{"- changed a dependency"})
		defer staged.Remove()

		// then
		require.False(t, staged.IsEmpty())
		assert.Equal(t, "CHANGELOG.md", staged.RepoPath)
	})

	t.Run("should stage nothing when the repository has no changelog", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()

		// when
		staged := support.StageRemoteChangelog(
			t.Context(), provider, repo, []string{"- changed a dependency"})

		// then
		assert.True(t, staged.IsEmpty())
	})

	t.Run("should stage nothing when fetching the changelog fails", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContentErr(assert.AnError).
			BuildSpy()

		// when
		staged := support.StageRemoteChangelog(
			t.Context(), provider, repo, []string{"- changed a dependency"})

		// then
		assert.True(t, staged.IsEmpty())
	})
}

func TestChangelogUpdateScript(t *testing.T) {
	t.Parallel()

	t.Run("should copy the staged file to the destination the environment names", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := support.ChangelogUpdateScript()

		// then
		assert.Contains(t, script, `cp "$CHANGELOG_FILE" "$CHANGELOG_DEST"`)
		assert.Contains(t, script, `mkdir -p "$(dirname "$CHANGELOG_DEST")"`)
	})

	t.Run("should default the destination to CHANGELOG.md", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := support.ChangelogUpdateScript()

		// then
		assert.Contains(t, script, `CHANGELOG_DEST="${CHANGELOG_DEST:-CHANGELOG.md}"`)
	})

	t.Run("should skip the copy when the upgrade produced no other change", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := support.ChangelogUpdateScript()

		// then
		assert.Contains(t, script, "git status --porcelain")
	})
}
