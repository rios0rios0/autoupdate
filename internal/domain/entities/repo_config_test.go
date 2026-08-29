package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

func TestRepoConfigIsSkipped(t *testing.T) {
	t.Parallel()

	t.Run("should return false for nil receiver", func(t *testing.T) {
		t.Parallel()

		// given
		var cfg *entities.RepoConfig

		// when
		result := cfg.IsSkipped()

		// then
		assert.False(t, result)
	})

	t.Run("should return false for zero-value config", func(t *testing.T) {
		t.Parallel()

		// given
		cfg := &entities.RepoConfig{}

		// when
		result := cfg.IsSkipped()

		// then
		assert.False(t, result)
	})

	t.Run("should return true when Skip is set", func(t *testing.T) {
		t.Parallel()

		// given
		cfg := &entities.RepoConfig{Skip: true, Reason: "fork; rebase manually"}

		// when
		result := cfg.IsSkipped()

		// then
		assert.True(t, result)
	})
}

func TestParseRepoConfig(t *testing.T) {
	t.Parallel()

	t.Run("should return zero-value config for empty input", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte("")

		// when
		cfg, err := entities.ParseRepoConfig(data)

		// then
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.False(t, cfg.IsSkipped())
		assert.Empty(t, cfg.Reason)
	})

	t.Run("should decode skip and reason", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte("skip: true\nreason: \"fork of upstream; rebase manually\"\n")

		// when
		cfg, err := entities.ParseRepoConfig(data)

		// then
		require.NoError(t, err)
		assert.True(t, cfg.IsSkipped())
		assert.Equal(t, "fork of upstream; rebase manually", cfg.Reason)
	})

	t.Run("should decode skip without reason", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte("skip: true\n")

		// when
		cfg, err := entities.ParseRepoConfig(data)

		// then
		require.NoError(t, err)
		assert.True(t, cfg.IsSkipped())
		assert.Empty(t, cfg.Reason)
	})

	t.Run("should ignore unknown keys for forward compatibility", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte("skip: false\nfuture_key: something\n")

		// when
		cfg, err := entities.ParseRepoConfig(data)

		// then
		require.NoError(t, err)
		assert.False(t, cfg.IsSkipped())
	})

	t.Run("should return error for malformed YAML", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte("skip: : not-yaml")

		// when
		_, err := entities.ParseRepoConfig(data)

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), entities.RepoConfigFile)
	})
}

func TestRepoConfigFileName(t *testing.T) {
	t.Parallel()

	// given/when/then
	assert.Equal(t, ".autoupdate.yaml", entities.RepoConfigFile)
}

func TestNarrowToProjectSchema(t *testing.T) {
	t.Parallel()

	t.Run("should keep the settings half and the opt-out marker", func(t *testing.T) {
		t.Parallel()

		// given
		config, err := entities.ParseRepoConfig([]byte(`
skip: false
reason: ''
exclude_forks: true
exclude_repos: ['*/sandbox']
cleanup_stale_branches: false
updaters:
  golang:
    enabled: false
`))
		require.NoError(t, err)

		// when -- the layer is what ApplyRepoOverlay folds, so applying it is the honest
		// way to see what survived the narrowing
		settings, err := entities.ApplyRepoOverlay(&entities.Settings{}, config)

		// then
		require.NoError(t, err)
		assert.True(t, settings.ExcludeForks)
		assert.Equal(t, []string{"*/sandbox"}, settings.ExcludeRepos)
		require.NotNil(t, settings.CleanupStaleBranches)
		assert.False(t, *settings.CleanupStaleBranches)
		assert.False(t, settings.Updaters["golang"].IsEnabled())
	})

	t.Run("should drop the operator-only keys", func(t *testing.T) {
		t.Parallel()

		// given -- narrowing rather than checking is the point: the document never reaches
		// a decoder that has a field for any of these
		base := &entities.Settings{
			GitHubAccessToken:     "operator-github",
			GpgKeyPath:            "operator-gpg",
			AggregateBranchPrefix: "chore/autoupdate-",
			Concurrency:           4,
			Providers: []entities.ProviderConfig{
				{Type: "github", Token: "operator-token", Organizations: []string{"my-org"}},
			},
		}
		config, err := entities.ParseRepoConfig([]byte(`
github_access_token: 'repo-github'
gpg_key_path: 'repo-gpg'
aggregate_branch_prefix: 'chore/'
concurrency: 64
providers:
  - type: 'github'
    token: 'repo-token'
    organizations: ['attacker']
`))
		require.NoError(t, err)

		// when
		settings, err := entities.ApplyRepoOverlay(base, config)

		// then
		require.NoError(t, err)
		assert.Equal(t, "operator-github", settings.GitHubAccessToken)
		assert.Equal(t, "operator-gpg", settings.GpgKeyPath)
		assert.Equal(t, "chore/autoupdate-", settings.AggregateBranchPrefix)
		assert.Equal(t, 4, settings.Concurrency)
		assert.Equal(t, "operator-token", settings.Providers[0].Token)
	})

	t.Run("should drop an unknown key rather than refusing to run", func(t *testing.T) {
		t.Parallel()

		// given
		config, err := entities.ParseRepoConfig([]byte("no_such_key: true\nskip: false\n"))

		// then -- forward compatibility: a file written for a newer AutoUpdate still parses
		require.NoError(t, err)
		assert.False(t, config.IsSkipped())
	})

	t.Run("should keep the skip marker working on its own", func(t *testing.T) {
		t.Parallel()

		// given
		config, err := entities.ParseRepoConfig([]byte("skip: true\nreason: 'frozen'\n"))

		// then
		require.NoError(t, err)
		assert.True(t, config.IsSkipped())
		assert.Equal(t, "frozen", config.Reason)
	})

	t.Run("should return an error when the document is not valid YAML", func(t *testing.T) {
		t.Parallel()

		// when
		config, err := entities.ParseRepoConfig([]byte("skip: [broken"))

		// then
		require.Error(t, err)
		assert.Nil(t, config)
	})
}

func TestApplyRepoOverlay(t *testing.T) {
	t.Parallel()

	t.Run("should return the base untouched when there is no file", func(t *testing.T) {
		t.Parallel()

		// given
		base := &entities.Settings{Concurrency: 4}

		// when
		settings, err := entities.ApplyRepoOverlay(base, nil)

		// then
		require.NoError(t, err)
		assert.Same(t, base, settings)
	})

	t.Run("should not mutate the settings the fan-out shares", func(t *testing.T) {
		t.Parallel()

		// given -- run mode processes an organization's repositories concurrently against
		// one *Settings, so an overlay that wrote through would leak one repository's
		// configuration into another's, nondeterministically
		enabled := true
		base := &entities.Settings{
			ExcludeForks: false,
			Updaters:     map[string]entities.UpdaterConfig{"golang": {Enabled: &enabled}},
		}

		first, err := entities.ParseRepoConfig([]byte("exclude_forks: true\n"))
		require.NoError(t, err)
		second, err := entities.ParseRepoConfig(
			[]byte("updaters:\n  golang:\n    enabled: false\n"),
		)
		require.NoError(t, err)

		// when
		firstSettings, err := entities.ApplyRepoOverlay(base, first)
		require.NoError(t, err)
		secondSettings, err := entities.ApplyRepoOverlay(base, second)
		require.NoError(t, err)

		// then
		assert.True(t, firstSettings.ExcludeForks)
		assert.False(t, secondSettings.ExcludeForks, "one repository must not see another's")
		assert.False(t, secondSettings.Updaters["golang"].IsEnabled())
		assert.True(t, firstSettings.Updaters["golang"].IsEnabled())
		assert.False(t, base.ExcludeForks, "the shared base must be untouched")
		assert.True(t, base.Updaters["golang"].IsEnabled())
	})
}
