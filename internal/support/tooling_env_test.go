package support_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/support"
)

// effectiveEnv reads env the way exec does: the last value of a duplicated key wins.
func effectiveEnv(env []string) map[string]string {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		if name, value, ok := strings.Cut(entry, "="); ok {
			values[name] = value
		}
	}
	return values
}

func TestScriptEnv(t *testing.T) {
	t.Parallel()

	t.Run("should return the process environment unchanged when no tooling directory is given", func(t *testing.T) {
		t.Parallel()

		// given / when
		env := support.ScriptEnv("")

		// then
		assert.Equal(t, os.Environ(), env)
	})

	t.Run(
		"should move every package manager's caches and temporary files into the tooling directory",
		func(t *testing.T) {
			t.Parallel()

			// given
			toolingDir := t.TempDir()

			// when
			env := effectiveEnv(support.ScriptEnv(toolingDir))

			// then
			for _, variable := range []string{
				"TMPDIR", "XDG_CACHE_HOME",
				"GOMODCACHE", "GOCACHE",
				"npm_config_cache", "PNPM_HOME", "YARN_GLOBAL_FOLDER", "COREPACK_HOME",
				"PIP_CACHE_DIR", "PDM_CACHE_DIR",
				"GRADLE_USER_HOME", support.MavenRepositoryVariable,
				"PUB_CACHE",
				"NUGET_PACKAGES", "NUGET_HTTP_CACHE_PATH", "NUGET_PLUGINS_CACHE_PATH",
				"BUNDLE_PATH", "BUNDLE_USER_CACHE",
			} {
				assert.True(t, strings.HasPrefix(env[variable], toolingDir+string(os.PathSeparator)),
					"%s should point into the tooling directory, got %q", variable, env[variable])
			}
			assert.Equal(t, "1", env["MSBUILDDISABLENODEREUSE"], "MSBuild must not keep worker nodes alive")
		},
	)

	t.Run("should leave the operator's configuration where the operator put it", func(t *testing.T) {
		t.Parallel()

		// given
		toolingDir := t.TempDir()

		// when
		env := effectiveEnv(support.ScriptEnv(toolingDir))

		// then
		for _, variable := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
			assert.Equal(t, os.Getenv(variable), env[variable], "%s must not be redirected", variable)
		}
		assert.NotContains(t, env, "YARN_CACHE_FOLDER",
			"it would move a zero-install Yarn project's committed cache out of the repository")
	})

	t.Run("should be honoured by the Go toolchain", func(t *testing.T) {
		t.Parallel()

		// given
		goBinary, err := exec.LookPath("go")
		if err != nil {
			t.Skip("go is not available on this platform")
		}
		toolingDir := t.TempDir()
		cmd := exec.CommandContext(t.Context(), goBinary, "env", "GOMODCACHE", "GOCACHE")
		cmd.Env = support.ScriptEnv(toolingDir)

		// when
		output, err := cmd.Output()

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{
			filepath.Join(toolingDir, "go", "mod"),
			filepath.Join(toolingDir, "go", "build"),
		}, strings.Fields(string(output)))
	})

	t.Run("should give the temporary files a script creates to the repository's workspace", func(t *testing.T) {
		t.Parallel()

		// given
		bash, err := exec.LookPath("bash")
		if err != nil {
			t.Skip("bash is not available on this platform")
		}
		workspace := newWorkspace(t)
		cmd := exec.CommandContext(t.Context(), bash, "-c", "mktemp -d")
		cmd.Env = support.ScriptEnv(workspace.ToolingDir())

		// when
		output, err := cmd.Output()

		// then
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(workspace.ToolingDir(), "tmp"), filepath.Dir(strings.TrimSpace(string(output))))
	})
}

// TestScriptEnvOverridesPresetCaches is not parallel: it presets the process
// environment with t.Setenv, the way a CI pipeline caching its packages does.
func TestScriptEnvOverridesPresetCaches(t *testing.T) {
	t.Run("should move a cache the process environment already points somewhere else", func(t *testing.T) {
		// given
		t.Setenv("GOMODCACHE", filepath.Join(t.TempDir(), "pipeline-cache"))
		t.Setenv("PIP_CACHE_DIR", filepath.Join(t.TempDir(), "pipeline-pip"))
		toolingDir := t.TempDir()

		// when
		env := effectiveEnv(support.ScriptEnv(toolingDir))

		// then
		assert.Equal(t, filepath.Join(toolingDir, "go", "mod"), env["GOMODCACHE"])
		assert.Equal(t, filepath.Join(toolingDir, "pip"), env["PIP_CACHE_DIR"])
	})
}
