package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// operatorLayer builds the layer the operator's own configuration is applied as: the only
// one that may name a credential, and the only one decoded strictly.
func operatorLayer(content string) entities.ConfigLayer {
	//nolint:exhaustruct // Optional stays false: a file the operator named must be read
	return entities.ConfigLayer{
		Name:   entities.LayerOperatorConfig,
		Origin: "/home/operator/.autoupdate.yaml",
		Data:   []byte(content),
		Scope:  entities.ScopeOperator,
		Strict: true,
	}
}

// restrictedLayer builds the layer a target repository's configuration is applied as.
func restrictedLayer(content string) entities.ConfigLayer {
	//nolint:exhaustruct // Strict is false for a restricted layer by construction
	return entities.ConfigLayer{
		Name:     entities.LayerProjectConfig,
		Origin:   entities.RepoConfigFile,
		Data:     []byte(content),
		Scope:    entities.ScopeRestricted,
		Optional: true,
	}
}

func TestApplyLayer(t *testing.T) {
	t.Parallel()

	t.Run("should leave a key the layer omits exactly as it was", func(t *testing.T) {
		t.Parallel()

		// given
		base := &entities.Settings{Concurrency: 8, GitHubAccessToken: "ghp_base"}

		// when
		result, err := entities.ApplyLayer(base, operatorLayer("exclude_forks: true\n"))

		// then
		require.NoError(t, err)
		assert.True(t, result.ExcludeForks)
		assert.Equal(t, 8, result.Concurrency, "an omitted key is not a decision")
		assert.Equal(t, "ghp_base", result.GitHubAccessToken)
	})

	t.Run("should let a later layer set false over an earlier true", func(t *testing.T) {
		t.Parallel()

		// given -- this is the property that makes pointers unnecessary. yaml.v3 assigns
		// only the keys a document carries, so "absent" and "false" stay distinguishable
		// without ExcludeForks, ExcludeArchived or Concurrency changing type.
		base := &entities.Settings{ExcludeForks: true, ExcludeArchived: true}

		// when
		result, err := entities.ApplyLayer(
			base, operatorLayer("exclude_forks: false\nexclude_archived: false\n"),
		)

		// then
		require.NoError(t, err)
		assert.False(t, result.ExcludeForks)
		assert.False(t, result.ExcludeArchived)
	})

	t.Run("should honour an explicit zero concurrency", func(t *testing.T) {
		t.Parallel()

		// given
		base := &entities.Settings{Concurrency: 8}

		// when
		result, err := entities.ApplyLayer(base, operatorLayer("concurrency: 0\n"))

		// then
		require.NoError(t, err)
		assert.Equal(t, 0, result.Concurrency, "zero means the built-in default, deliberately")
	})

	t.Run("should treat a comments-only document as a layer with nothing to say", func(t *testing.T) {
		t.Parallel()

		// given -- the shipped defaults are largely comments, so an empty decode reporting
		// io.EOF must not read as a broken layer
		base := &entities.Settings{Concurrency: 4}

		// when
		result, err := entities.ApplyLayer(base, operatorLayer("# nothing but a comment\n"))

		// then
		require.NoError(t, err)
		assert.Equal(t, 4, result.Concurrency)
	})

	t.Run("should replace a slice wholesale", func(t *testing.T) {
		t.Parallel()

		// given
		base := &entities.Settings{ExcludeRepos: []string{"old/one", "old/two"}}

		// when
		result, err := entities.ApplyLayer(base, operatorLayer("exclude_repos: ['new/one']\n"))

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"new/one"}, result.ExcludeRepos)
	})

	t.Run("should deep-merge an updater instead of replacing it", func(t *testing.T) {
		t.Parallel()

		// given -- yaml.v3 decodes each map value into a fresh zero element, so without the
		// re-merge this layer would wipe the `enabled` the base set
		enabled := true
		base := &entities.Settings{
			Updaters: map[string]entities.UpdaterConfig{
				"golang": {Enabled: &enabled},
				"python": {Enabled: &enabled},
			},
		}

		// when
		result, err := entities.ApplyLayer(
			base, operatorLayer("updaters:\n  golang:\n    auto_complete: true\n"),
		)

		// then
		require.NoError(t, err)
		assert.True(t, result.Updaters["golang"].IsEnabled(), "the inherited field must survive")
		assert.True(t, result.Updaters["golang"].IsAutoComplete())
		assert.Contains(t, result.Updaters, "python", "an untouched updater must survive")
	})

	t.Run("should add an updater the base does not carry", func(t *testing.T) {
		t.Parallel()

		// given
		base := &entities.Settings{Updaters: map[string]entities.UpdaterConfig{}}

		// when
		result, err := entities.ApplyLayer(
			base, operatorLayer("updaters:\n  brandnew:\n    enabled: false\n"),
		)

		// then
		require.NoError(t, err)
		assert.False(t, result.Updaters["brandnew"].IsEnabled())
	})

	t.Run("should reject an unknown key in a strict layer", func(t *testing.T) {
		t.Parallel()

		// when
		result, err := entities.ApplyLayer(&entities.Settings{}, operatorLayer("no_such_key: true\n"))

		// then
		require.Error(t, err, "a typo in the operator's own file is theirs to hear about")
		assert.Nil(t, result)
	})

	t.Run("should ignore an unknown key in a restricted layer", func(t *testing.T) {
		t.Parallel()

		// when
		result, err := entities.ApplyLayer(&entities.Settings{}, restrictedLayer("no_such_key: true\n"))

		// then
		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("should ignore every operator-only key", func(t *testing.T) {
		t.Parallel()

		// given
		base := &entities.Settings{
			GitHubAccessToken:      "operator-github",
			GitLabAccessToken:      "operator-gitlab",
			AzureDevOpsAccessToken: "operator-azure",
			GpgKeyPath:             "operator-gpg",
			AggregateBranchPrefix:  "chore/autoupdate-",
			Concurrency:            4,
			Providers: []entities.ProviderConfig{
				{Type: "github", Token: "operator-token", Organizations: []string{"my-org"}},
			},
		}
		layer := restrictedLayer(`
github_access_token: 'repo-github'
gitlab_access_token: 'repo-gitlab'
azure_devops_access_token: 'repo-azure'
gpg_key_path: 'repo-gpg'
gpg_key_passphrase: 'repo-gpg-pass'
aggregate_branch_prefix: 'chore/'
concurrency: 64
providers:
  - type: 'github'
    token: 'repo-token'
    organizations: ['attacker']
`)

		// when
		result, err := entities.ApplyLayer(base, layer)

		// then
		require.NoError(t, err)
		assert.Equal(t, "operator-github", result.GitHubAccessToken)
		assert.Equal(t, "operator-gitlab", result.GitLabAccessToken)
		assert.Equal(t, "operator-azure", result.AzureDevOpsAccessToken)
		assert.Equal(t, "operator-gpg", result.GpgKeyPath)
		assert.Empty(t, result.GpgKeyPassphrase)
		assert.Equal(t, "chore/autoupdate-", result.AggregateBranchPrefix,
			"the prefix decides what stale-branch cleanup deletes")
		assert.Equal(t, 4, result.Concurrency,
			"the fan-out is decided before a repository's own file is read")
		assert.Equal(t, "operator-token", result.Providers[0].Token)
	})

	t.Run("should accept the settings a repository may speak for", func(t *testing.T) {
		t.Parallel()

		// when
		result, err := entities.ApplyLayer(&entities.Settings{}, restrictedLayer(`
cleanup_stale_branches: false
exclude_forks: true
exclude_archived: true
exclude_repos: ['*/sandbox']
updaters:
  golang:
    enabled: false
`))

		// then
		require.NoError(t, err)
		require.NotNil(t, result.CleanupStaleBranches)
		assert.False(t, *result.CleanupStaleBranches)
		assert.True(t, result.ExcludeForks)
		assert.True(t, result.ExcludeArchived)
		assert.Equal(t, []string{"*/sandbox"}, result.ExcludeRepos)
		assert.False(t, result.Updaters["golang"].IsEnabled())
	})

	t.Run("should leave the target untouched when the layer fails to decode", func(t *testing.T) {
		t.Parallel()

		// given
		base := &entities.Settings{Concurrency: 4, AggregateBranchPrefix: "chore/autoupdate-"}

		// when
		_, err := entities.ApplyLayer(base, operatorLayer("concurrency: 8\nbroken: [\n"))

		// then
		require.Error(t, err)
		assert.Equal(t, 4, base.Concurrency, "apply is atomic: nothing lands on a failure")
		assert.Equal(t, "chore/autoupdate-", base.AggregateBranchPrefix)
	})

	t.Run("should not write through to the caller's updaters map", func(t *testing.T) {
		t.Parallel()

		// given -- in run mode one *Settings is shared by every goroutine in the
		// per-organization fan-out, so writing through would leak one repository's
		// configuration into another's
		enabled := true
		base := &entities.Settings{
			Updaters: map[string]entities.UpdaterConfig{"golang": {Enabled: &enabled}},
		}

		// when
		result, err := entities.ApplyLayer(
			base, restrictedLayer("updaters:\n  golang:\n    enabled: false\n"),
		)

		// then
		require.NoError(t, err)
		assert.False(t, result.Updaters["golang"].IsEnabled())
		assert.True(t, base.Updaters["golang"].IsEnabled(), "the base must be untouched")
	})
}

func TestResolveSettings(t *testing.T) {
	t.Parallel()

	t.Run("should apply the layers in order", func(t *testing.T) {
		t.Parallel()

		// given
		//nolint:exhaustruct // the built-in defaults have no origin an operator can look at
		layers := []entities.ConfigLayer{
			{
				Name:   entities.LayerBuiltInDefaults,
				Data:   []byte("concurrency: 4\nexclude_forks: true\n"),
				Scope:  entities.ScopeRestricted,
				Strict: true,
			},
			operatorLayer("concurrency: 16\n"),
		}

		// when
		result, err := entities.ResolveSettings(layers)

		// then
		require.NoError(t, err)
		assert.Equal(t, 16, result.Concurrency, "the later layer wins")
		assert.True(t, result.ExcludeForks, "the earlier layer survives")
	})

	t.Run("should skip an optional layer that cannot be decoded", func(t *testing.T) {
		t.Parallel()

		// given
		//nolint:exhaustruct // Strict stays false: `main` may be newer than this binary
		published := entities.ConfigLayer{
			Name:     entities.LayerPublishedDefaults,
			Origin:   entities.DefaultConfigURL,
			Data:     []byte("this is not: [ yaml\n"),
			Scope:    entities.ScopeRestricted,
			Optional: true,
		}

		// when
		result, err := entities.ResolveSettings([]entities.ConfigLayer{
			operatorLayer("concurrency: 4\n"), published,
		})

		// then
		require.NoError(t, err, "an unreachable or broken remote must not stop a run")
		assert.Equal(t, 4, result.Concurrency)
	})

	t.Run("should fail when a required layer cannot be decoded", func(t *testing.T) {
		t.Parallel()

		// when
		result, err := entities.ResolveSettings([]entities.ConfigLayer{
			operatorLayer("this is not: [ yaml\n"),
		})

		// then
		require.Error(t, err, "a file the operator named must be readable")
		assert.Nil(t, result)
	})
}

// This cannot join TestResolveSettings above: it calls t.Parallel(), and t.Setenv is
// refused anywhere under a parallel ancestor.
func TestResolveSettingsResolvesTokens(t *testing.T) {
	// given -- finalisation runs once, over the folded configuration. Running it per layer
	// would resolve a token even when the next layer replaced it.
	t.Setenv("AUTOUPDATE_TEST_TOKEN", "ghp_from_env")

	// when
	result, err := entities.ResolveSettings([]entities.ConfigLayer{
		operatorLayer("github_access_token: '${AUTOUPDATE_TEST_TOKEN}'\n"),
	})

	// then
	require.NoError(t, err)
	assert.Equal(t, "ghp_from_env", result.GitHubAccessToken)
}
