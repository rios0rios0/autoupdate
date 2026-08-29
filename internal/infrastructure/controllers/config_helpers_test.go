package controllers_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/controllers"
)

// writeConfig creates a `.autoupdate.yaml` holding body in dir, and returns its path.
func writeConfig(t *testing.T, dir, body string) string {
	t.Helper()

	path := filepath.Join(dir, ".autoupdate.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return path
}

// errOffline stands in for an unreachable GitHub.
var errOffline = errors.New("offline")

// offlineFetch is the published-defaults fetch for a machine with no network. Every test
// below uses it: the layering takes bytes, so none of it needs a network.
func offlineFetch(string) ([]byte, error) { return nil, errOffline }

// providerSettings is the smallest configuration `autoupdate run` will accept.
const providerSettings = "providers:\n  - type: 'github'\n    token: 'tok'\n" +
	"    organizations: ['my-org']\n"

func TestResolveOperatorConfigPath(t *testing.T) {
	// The bug this ordering exists to prevent. `autoupdate .` runs with the target
	// repository as the working directory, and that repository may carry its own
	// `.autoupdate.yaml` -- same file name, narrower schema. Reading it as the operator's
	// configuration substitutes a project's settings for an operator's: no providers, no
	// tokens, and the keys the project layer bans honoured as though the operator wrote them.
	t.Run("should prefer the home config over a repository opt-out file", func(t *testing.T) {
		// given
		home := t.TempDir()
		t.Setenv("HOME", home)
		expected := writeConfig(t, home, providerSettings)
		repo := t.TempDir()
		writeConfig(t, repo, "skip: true\nreason: 'opted out'\n")
		t.Chdir(repo)

		// when
		path := controllers.ResolveOperatorConfigPath("")

		// then
		assert.Equal(t, expected, path)
	})

	t.Run("should not adopt the working directory's config", func(t *testing.T) {
		// given
		t.Setenv("HOME", t.TempDir())
		work := t.TempDir()
		writeConfig(t, work, providerSettings)
		t.Chdir(work)

		// when
		path := controllers.ResolveOperatorConfigPath("")

		// then
		assert.Empty(t, path,
			"a file in the working directory belongs to the project, and a project's config "+
				"is merged on top of the operator's rather than standing in for it")
	})

	t.Run("should return the given path untouched when one is supplied", func(t *testing.T) {
		// given
		home := t.TempDir()
		t.Setenv("HOME", home)
		writeConfig(t, home, providerSettings)

		// when
		path := controllers.ResolveOperatorConfigPath("/explicit/path.yaml")

		// then
		assert.Equal(t, "/explicit/path.yaml", path,
			"an explicit --config must win over discovery")
	})

	t.Run("should report no operator configuration when there is none", func(t *testing.T) {
		// given
		t.Setenv("HOME", t.TempDir())
		t.Chdir(t.TempDir())

		// when
		path := controllers.ResolveOperatorConfigPath("")

		// then -- no longer an error: the built-in defaults are the base of every run
		assert.Empty(t, path)
	})
}

func TestResolveConfigLayers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	t.Run("should run on the built-in defaults alone", func(t *testing.T) {
		// given -- no --config, nothing in $HOME, the published defaults unreachable, and
		// local mode, which takes its provider from the repository's own origin remote
		// when
		settings, err := controllers.ResolveWithFetch("", false, offlineFetch)

		// then
		require.NoError(t, err)
		require.NotNil(t, settings)
		assert.NotEmpty(t, settings.Updaters,
			"every updater must be configured with no configuration at all")
	})

	t.Run("should require a provider only in batch mode", func(t *testing.T) {
		// when
		_, localErr := controllers.ResolveWithFetch("", false, offlineFetch)
		_, batchErr := controllers.ResolveWithFetch("", true, offlineFetch)

		// then
		require.NoError(t, localErr)
		require.ErrorIs(t, batchErr, entities.ErrNoProvidersConfigured)
	})

	t.Run("should fold the operator's file onto the built-in defaults", func(t *testing.T) {
		// given
		configPath := writeConfig(t, t.TempDir(),
			providerSettings+"updaters:\n  golang:\n    enabled: false\n")

		// when
		settings, err := controllers.ResolveWithFetch(configPath, true, offlineFetch)

		// then
		require.NoError(t, err)
		assert.False(t, settings.Updaters["golang"].IsEnabled())
		assert.True(t, settings.Updaters["python"].IsEnabled(),
			"an updater the operator did not mention keeps the built-in default")
	})

	t.Run("should ignore a credential the published defaults try to set", func(t *testing.T) {
		// given -- bytes fetched over the network are not the operator speaking
		published := func(string) ([]byte, error) {
			return []byte("github_access_token: 'ghp_from_the_internet'\n"), nil
		}

		// when
		settings, err := controllers.ResolveWithFetch("", false, published)

		// then
		require.NoError(t, err)
		assert.Empty(t, settings.GitHubAccessToken)
	})

	t.Run("should return an error when the named config does not exist", func(t *testing.T) {
		// given
		missing := filepath.Join(t.TempDir(), "nonexistent.yaml")

		// when
		settings, err := controllers.ResolveWithFetch(missing, false, offlineFetch)

		// then
		require.Error(t, err, "a file the operator named by hand must be readable")
		assert.Nil(t, settings)
	})

	t.Run("should return an error when the aggregate prefix is unusable", func(t *testing.T) {
		// given -- caught at startup, before a single branch has been listed, let alone
		// deleted
		configPath := writeConfig(t, t.TempDir(),
			providerSettings+"aggregate_branch_prefix: 'chore/'\n")

		// when
		settings, err := controllers.ResolveWithFetch(configPath, true, offlineFetch)

		// then
		require.ErrorIs(t, err, entities.ErrAggregateBranchPrefixInvalid)
		assert.Nil(t, settings)
	})
}

func TestConfigLayerAssembly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	t.Run("should assemble the three operator-facing layers in order", func(t *testing.T) {
		// given
		configPath := writeConfig(t, t.TempDir(), providerSettings)
		published := func(string) ([]byte, error) { return []byte("# empty\n"), nil }

		// when
		names, err := controllers.LayerNamesWithFetch(configPath, published)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{
			entities.LayerBuiltInDefaults,
			entities.LayerPublishedDefaults,
			entities.LayerOperatorConfig,
		}, names)
	})

	t.Run("should omit the published defaults when they cannot be fetched", func(t *testing.T) {
		// given
		configPath := writeConfig(t, t.TempDir(), providerSettings)

		// when
		names, err := controllers.LayerNamesWithFetch(configPath, offlineFetch)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{
			entities.LayerBuiltInDefaults,
			entities.LayerOperatorConfig,
		}, names)
	})
}
