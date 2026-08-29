package support_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

// writeChlogRepo lays out a repository on disk with the given files, where a
// path ending in "/" creates a directory.
func writeChlogRepo(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	for path, content := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if content == "" && path[len(path)-1] == '/' {
			// A directory needs the owner search bit, so 0o700 is the
			// least-privilege mode here.
			// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
			require.NoError(t, os.MkdirAll(full, 0o700))
			continue
		}
		// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o700))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
	}
	return root
}

// readChlogFragments returns the fragment files pending under dir.
func readChlogFragments(t *testing.T, root, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)

	contents := make([]string, 0, len(entries))
	for _, entry := range entries {
		data, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(dir), entry.Name()))
		require.NoError(t, readErr)
		contents = append(contents, string(data))
	}
	return contents
}

func TestDetectLocalChlog(t *testing.T) {
	t.Parallel()

	t.Run("should detect chlog when the repository commits a .chlog.yaml", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{".chlog.yaml": "changesDir: .changes\n"})

		// when
		config, usesChlog, err := support.DetectLocalChlog(root)

		// then
		require.NoError(t, err)
		assert.True(t, usesChlog)
		assert.Equal(t, ".changes/unreleased", config.UnreleasedPath())
	})

	t.Run("should detect chlog from the .chlog.yml spelling", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{".chlog.yml": "unreleasedDir: pending\n"})

		// when
		config, usesChlog, err := support.DetectLocalChlog(root)

		// then
		require.NoError(t, err)
		assert.True(t, usesChlog)
		assert.Equal(t, ".changes/pending", config.UnreleasedPath())
	})

	t.Run("should detect chlog from the unreleased directory alone", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{".changes/unreleased/": ""})

		// when
		_, usesChlog, err := support.DetectLocalChlog(root)

		// then
		require.NoError(t, err)
		assert.True(t, usesChlog)
	})

	t.Run("should not detect chlog for a plain Keep a Changelog repository", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": "# Changelog\n"})

		// when
		_, usesChlog, err := support.DetectLocalChlog(root)

		// then
		require.NoError(t, err)
		assert.False(t, usesChlog)
	})

	t.Run("should return an error when the configuration escapes the repository", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{".chlog.yaml": "changesDir: ../../etc\n"})

		// when
		_, usesChlog, err := support.DetectLocalChlog(root)

		// then
		require.ErrorIs(t, err, entities.ErrChlogPathEscapesRepo)
		assert.False(t, usesChlog)
	})

	t.Run("should return an error when the configuration is not valid YAML", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{".chlog.yaml": "kinds: [oops\n"})

		// when
		_, usesChlog, err := support.DetectLocalChlog(root)

		// then
		require.Error(t, err)
		assert.False(t, usesChlog)
		assert.Contains(t, err.Error(), ".chlog.yaml")
	})

	t.Run("should name the .chlog.yml file it actually read in a parse error", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{".chlog.yml": "kinds: [oops\n"})

		// when
		_, _, err := support.DetectLocalChlog(root)

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), ".chlog.yml")
		assert.NotContains(t, err.Error(), ".chlog.yaml")
	})
}

func TestDetectRemoteChlog(t *testing.T) {
	t.Parallel()

	repo := entities.Repository{Organization: "org", Name: "repo"}

	t.Run("should detect chlog when the repository commits a .chlog.yaml", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".chlog.yaml": true}).
			WithFileContents(map[string]string{".chlog.yaml": "unreleasedDir: pending\n"}).
			BuildSpy()

		// when
		config, usesChlog, err := support.DetectRemoteChlog(t.Context(), provider, repo)

		// then
		require.NoError(t, err)
		assert.True(t, usesChlog)
		assert.Equal(t, ".changes/pending", config.UnreleasedPath())
	})

	t.Run("should detect chlog from a pending fragment alone", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			WithFiles([]entities.File{
				{Path: "README.md"},
				{Path: ".changes/unreleased/1748359200-a1b2.yaml"},
			}).
			BuildSpy()

		// when
		_, usesChlog, err := support.DetectRemoteChlog(t.Context(), provider, repo)

		// then
		require.NoError(t, err)
		assert.True(t, usesChlog)
	})

	t.Run("should detect chlog from an Azure DevOps root-anchored path", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			WithFiles([]entities.File{{Path: "/.changes/unreleased/1748359200-a1b2.yaml"}}).
			BuildSpy()

		// when
		_, usesChlog, err := support.DetectRemoteChlog(t.Context(), provider, repo)

		// then
		require.NoError(t, err)
		assert.True(t, usesChlog)
	})

	t.Run("should not detect chlog for a plain Keep a Changelog repository", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFiles([]entities.File{{Path: "CHANGELOG.md"}, {Path: "go.mod"}}).
			BuildSpy()

		// when
		_, usesChlog, err := support.DetectRemoteChlog(t.Context(), provider, repo)

		// then
		require.NoError(t, err)
		assert.False(t, usesChlog)
	})

	t.Run("should not mistake the unreleased directory entry for a fragment", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			WithFiles([]entities.File{
				{Path: ".changes/unreleased/notes.md"},
				{Path: ".changes/unreleased/sub", IsDir: true},
			}).
			BuildSpy()

		// when
		_, usesChlog, err := support.DetectRemoteChlog(t.Context(), provider, repo)

		// then
		require.NoError(t, err)
		assert.False(t, usesChlog)
	})

	t.Run("should return an error when the configuration cannot be fetched", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".chlog.yaml": true}).
			WithFileContentErr(assert.AnError).
			BuildSpy()

		// when
		_, usesChlog, err := support.DetectRemoteChlog(t.Context(), provider, repo)

		// then
		require.Error(t, err)
		assert.False(t, usesChlog)
	})

	t.Run("should return an error when the tree cannot be listed", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			WithListFileErr(assert.AnError).
			BuildSpy()

		// when
		_, usesChlog, err := support.DetectRemoteChlog(t.Context(), provider, repo)

		// then
		require.Error(t, err)
		assert.False(t, usesChlog)
	})
}
