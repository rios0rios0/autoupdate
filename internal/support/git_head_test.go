package support_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/support"
	"github.com/rios0rios0/autoupdate/test/infrastructure/gitrepo"
)

func TestModifiedPaths(t *testing.T) {
	t.Parallel()

	t.Run("should list only the tracked files modified since HEAD", func(t *testing.T) {
		t.Parallel()

		// given
		root := gitrepo.New(t, map[string]string{
			"go.mod":         "module example.com/a\n",
			"tools/go.mod":   "module example.com/tools\n",
			"deleted.txt":    "gone soon\n",
			"untouched.txt":  "same\n",
			"dir with/space": "before\n",
		})
		gitrepo.Write(t, root, map[string]string{
			"go.mod":         "module example.com/a\n\ngo 1.27\n",
			"tools/go.mod":   "module example.com/tools\n\ngo 1.27\n",
			"dir with/space": "after\n",
			"added.txt":      "new\n",
		})
		require.NoError(t, os.Remove(filepath.Join(root, "deleted.txt")))

		// when
		paths, err := support.ModifiedPaths(t.Context(), root)

		// then
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"go.mod", "tools/go.mod", "dir with/space"}, paths)
	})

	t.Run("should fail outside a repository", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()

		// when
		paths, err := support.ModifiedPaths(t.Context(), root)

		// then
		require.Error(t, err)
		assert.Nil(t, paths)
	})
}

func TestHeadFileContent(t *testing.T) {
	t.Parallel()

	t.Run("should return the content a file had at HEAD", func(t *testing.T) {
		t.Parallel()

		// given
		root := gitrepo.New(t, map[string]string{"tools/go.mod": "module example.com/tools\n"})
		gitrepo.Write(t, root, map[string]string{"tools/go.mod": "module example.com/tools\n\ngo 1.27\n"})

		// when
		content, err := support.HeadFileContent(t.Context(), root, "tools/go.mod")

		// then
		require.NoError(t, err)
		assert.Equal(t, "module example.com/tools\n", string(content))
	})

	t.Run("should fail for a file HEAD does not have", func(t *testing.T) {
		t.Parallel()

		// given
		root := gitrepo.New(t, map[string]string{"go.mod": "module example.com/a\n"})

		// when
		content, err := support.HeadFileContent(t.Context(), root, "missing.mod")

		// then
		require.Error(t, err)
		assert.Nil(t, content)
	})
}

func TestReadModifiedFiles(t *testing.T) {
	t.Parallel()

	t.Run("should return both versions of the modified files that match", func(t *testing.T) {
		t.Parallel()

		// given
		root := gitrepo.New(t, map[string]string{"a/go.mod": "before\n", "README.md": "before\n"})
		gitrepo.Write(t, root, map[string]string{"a/go.mod": "after\n", "README.md": "after\n"})

		// when
		files, err := support.ReadModifiedFiles(t.Context(), root, func(path string) bool {
			return path == "a/go.mod"
		})

		// then
		require.NoError(t, err)
		assert.Equal(t, []support.ModifiedFile{
			{Path: "a/go.mod", Before: []byte("before\n"), After: []byte("after\n")},
		}, files)
	})

	t.Run("should fail outside a repository", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()

		// when
		files, err := support.ReadModifiedFiles(t.Context(), root, func(string) bool { return true })

		// then
		require.Error(t, err)
		assert.Nil(t, files)
	})
}
