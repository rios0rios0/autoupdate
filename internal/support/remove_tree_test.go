package support_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/support"
)

const (
	// readOnlyDirMode is the mode Go gives every directory of its module cache.
	readOnlyDirMode = 0o555
	// writableDirMode lets t.TempDir() clean up a directory a test made read-only.
	writableDirMode = 0o700
)

// writeReadOnlyModuleCache lays a module out under root the way `go mod download`
// leaves it: a file inside directories nobody may write to.
func writeReadOnlyModuleCache(t *testing.T, root string) {
	t.Helper()

	module := filepath.Join(root, "go", "mod", "example.com", "dependency@v1.0.0")
	// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
	require.NoError(t, os.MkdirAll(module, writableDirMode))
	require.NoError(t, os.WriteFile(filepath.Join(module, "go.mod"), []byte("module example.com/dependency\n"), 0o400))

	for dir := module; dir != root; dir = filepath.Dir(dir) {
		require.NoError(t, os.Chmod(dir, readOnlyDirMode))
	}
}

func TestRemoveTree(t *testing.T) {
	t.Parallel()

	t.Run("should remove a tree whose directories are read-only, as Go leaves its module cache", func(t *testing.T) {
		t.Parallel()

		// given
		tree := filepath.Join(t.TempDir(), "workspace")
		writeReadOnlyModuleCache(t, tree)

		// when
		err := support.RemoveTree(tree)

		// then
		require.NoError(t, err)
		assert.NoDirExists(t, tree)
	})

	t.Run("should succeed when the tree does not exist", func(t *testing.T) {
		t.Parallel()

		// given
		missing := filepath.Join(t.TempDir(), "never-created")

		// when
		err := support.RemoveTree(missing)

		// then
		require.NoError(t, err)
	})

	t.Run("should remove a link without touching the directory it points to", func(t *testing.T) {
		t.Parallel()

		// given a read-only directory outside the tree, linked from inside it, and
		// a read-only cache that forces the removal down its second, chmod path
		outside := t.TempDir()
		kept := filepath.Join(outside, "gradle.properties")
		require.NoError(t, os.WriteFile(kept, []byte("org.gradle.java.home=/opt/jdk\n"), 0o600))
		require.NoError(t, os.Chmod(outside, readOnlyDirMode))
		// A directory needs the owner search bit to be removed by t.TempDir().
		// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
		t.Cleanup(func() { _ = os.Chmod(outside, writableDirMode) })

		tree := filepath.Join(t.TempDir(), "workspace")
		writeReadOnlyModuleCache(t, tree)
		require.NoError(t, os.Symlink(outside, filepath.Join(tree, "operator-config")))

		// when
		err := support.RemoveTree(tree)

		// then
		require.NoError(t, err)
		assert.NoDirExists(t, tree)
		assert.FileExists(t, kept)
		info, statErr := os.Stat(outside)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(readOnlyDirMode), info.Mode().Perm())
	})
}
