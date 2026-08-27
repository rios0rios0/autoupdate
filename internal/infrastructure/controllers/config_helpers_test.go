package controllers_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/controllers"
)

// writeConfig creates a `.autoupdate.yaml` holding body in dir, and returns its path.
func writeConfig(t *testing.T, dir, body string) string {
	t.Helper()

	path := filepath.Join(dir, ".autoupdate.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return path
}

func TestResolveConfigPath(t *testing.T) {
	const globalSettings = "projects:\n  - path: 'https://example.com/x.git'\n"

	// The bug this ordering exists to prevent. `autoupdate .` runs with the target repository as
	// the working directory, and that repository may carry its own `.autoupdate.yaml` -- same
	// file name, different schema: a `skip`/`reason` opt-out rather than the global settings.
	// Reading it as the configuration yields no projects, no updaters and no tokens, and the run
	// carries on with downloaded defaults instead of reporting that it read the wrong file.
	t.Run("should prefer the home config over a repository opt-out file", func(t *testing.T) {
		// given
		home := t.TempDir()
		t.Setenv("HOME", home)
		expected := writeConfig(t, home, globalSettings)
		repo := t.TempDir()
		writeConfig(t, repo, "skip: true\nreason: 'opted out'\n")
		t.Chdir(repo)

		// when
		path, err := controllers.ResolveConfigPath("")

		// then
		require.NoError(t, err)
		assert.Equal(t, expected, path)
	})

	t.Run("should fall back to the working directory when the home has no config", func(t *testing.T) {
		// given
		t.Setenv("HOME", t.TempDir())
		work := t.TempDir()
		writeConfig(t, work, globalSettings)
		t.Chdir(work)

		// when
		path, err := controllers.ResolveConfigPath("")

		// then
		require.NoError(t, err)
		resolved, absErr := filepath.Abs(path)
		require.NoError(t, absErr)
		assert.Equal(t, filepath.Join(work, ".autoupdate.yaml"), resolved,
			"an operator with no home config has no operator-level settings to lose")
	})

	t.Run("should return the given path untouched when one is supplied", func(t *testing.T) {
		// given
		home := t.TempDir()
		t.Setenv("HOME", home)
		writeConfig(t, home, globalSettings)

		// when
		path, err := controllers.ResolveConfigPath("/explicit/path.yaml")

		// then
		require.NoError(t, err)
		assert.Equal(t, "/explicit/path.yaml", path, "an explicit --config must win over discovery")
	})

	t.Run("should report an error when no config is found anywhere", func(t *testing.T) {
		// given
		t.Setenv("HOME", t.TempDir())
		t.Chdir(t.TempDir())

		// when
		path, err := controllers.ResolveConfigPath("")

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no config file found")
		assert.Empty(t, path)
	})
}
