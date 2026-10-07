package support_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/support"
)

// newWorkspace creates a repository workspace that is removed when the test ends,
// whatever the test did to it.
func newWorkspace(t *testing.T) *support.RepositoryWorkspace {
	t.Helper()

	workspace, err := support.NewRepositoryWorkspace()
	require.NoError(t, err)
	t.Cleanup(func() { _ = workspace.Remove() })
	return workspace
}

func TestNewRepositoryWorkspace(t *testing.T) {
	t.Parallel()

	t.Run("should create the workspace in the temporary directory with room for the clone", func(t *testing.T) {
		t.Parallel()

		// given / when
		workspace := newWorkspace(t)

		// then
		//nolint:usetesting // the system temporary directory is what is being tested
		assert.Equal(t, filepath.Clean(os.TempDir()), filepath.Dir(workspace.Root()))
		assert.True(t, strings.HasPrefix(filepath.Base(workspace.Root()), "autoupdate-batch-"),
			"the stale-directory sweep matches the workspace by this prefix, got %s", workspace.Root())
		assert.Equal(t, filepath.Join(workspace.Root(), "repo"), workspace.RepoDir())
		assert.NoDirExists(t, workspace.RepoDir(), "the clone creates its own directory")
		assert.DirExists(t, filepath.Join(workspace.ToolingDir(), "tmp"),
			"mktemp fails in a TMPDIR that does not exist")
	})
}

func TestRepositoryWorkspaceRemove(t *testing.T) {
	t.Parallel()

	t.Run("should remove the clone and every cache the updaters left beside it", func(t *testing.T) {
		t.Parallel()

		// given
		workspace := newWorkspace(t)
		require.NoError(t, os.MkdirAll(workspace.RepoDir(), writableDirMode))
		require.NoError(t, os.WriteFile(filepath.Join(workspace.RepoDir(), "go.mod"), []byte("module m\n"), 0o600))
		writeReadOnlyModuleCache(t, workspace.ToolingDir())

		// when
		err := workspace.Remove()

		// then
		require.NoError(t, err)
		assert.NoDirExists(t, workspace.Root())
	})

	t.Run("should succeed when the workspace was already removed", func(t *testing.T) {
		t.Parallel()

		// given
		workspace := newWorkspace(t)
		require.NoError(t, workspace.Remove())

		// when
		err := workspace.Remove()

		// then
		require.NoError(t, err)
	})
}

// TestRepositoryWorkspaceGradleConfiguration is not parallel: it points
// GRADLE_USER_HOME at a directory of its own with t.Setenv.
func TestRepositoryWorkspaceGradleConfiguration(t *testing.T) {
	t.Run("should carry the operator's Gradle configuration over and leave it in place on removal", func(t *testing.T) {
		// given
		operatorHome := t.TempDir()
		properties := filepath.Join(operatorHome, "gradle.properties")
		initScript := filepath.Join(operatorHome, "init.d", "mirror.gradle")
		require.NoError(t, os.WriteFile(properties, []byte("systemProp.https.proxyHost=proxy\n"), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Dir(initScript), writableDirMode))
		require.NoError(t, os.WriteFile(initScript, []byte("// mirror\n"), 0o600))
		t.Setenv("GRADLE_USER_HOME", operatorHome)

		// when
		workspace := newWorkspace(t)

		// then
		gradleHome := filepath.Join(workspace.ToolingDir(), "gradle")
		content, err := os.ReadFile(filepath.Join(gradleHome, "gradle.properties"))
		require.NoError(t, err)
		assert.Equal(t, "systemProp.https.proxyHost=proxy\n", string(content))
		assert.FileExists(t, filepath.Join(gradleHome, "init.d", "mirror.gradle"))
		assert.NoFileExists(t, filepath.Join(gradleHome, "init.gradle"), "only what the operator has is linked")

		require.NoError(t, workspace.Remove())
		assert.FileExists(t, properties)
		assert.FileExists(t, initScript)
	})
}
