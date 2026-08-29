package configs_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/configs"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// TestEmbeddedDefaults guards the one layer that cannot fail gracefully.
//
// The built-in defaults are the base of every run, are compiled into the binary, and are
// the same bytes served from `main` to every installed binary. A mistake in that file is
// not a configuration problem an operator can fix -- it ships.
func TestEmbeddedDefaults(t *testing.T) {
	t.Parallel()

	//nolint:exhaustruct // the built-in defaults have no origin an operator can look at
	layer := entities.ConfigLayer{
		Name:   entities.LayerBuiltInDefaults,
		Data:   configs.Default,
		Scope:  entities.ScopeOperator,
		Strict: true,
	}

	t.Run("should decode strictly", func(t *testing.T) {
		t.Parallel()

		// when -- ScopeOperator + Strict is the harshest reading there is, so a key this
		// binary does not know is caught here rather than in the field
		settings, err := entities.ApplyLayer(&entities.Settings{}, layer)

		// then
		require.NoError(t, err)
		require.NotNil(t, settings)
	})

	t.Run("should carry no credentials", func(t *testing.T) {
		t.Parallel()

		// given
		settings, err := entities.ApplyLayer(&entities.Settings{}, layer)
		require.NoError(t, err)

		// then
		assert.Empty(t, settings.GitHubAccessToken)
		assert.Empty(t, settings.GitLabAccessToken)
		assert.Empty(t, settings.AzureDevOpsAccessToken)
		assert.Empty(t, settings.GpgKeyPath)
		assert.Empty(t, settings.GpgKeyPassphrase)
	})

	t.Run("should name no organizations to scan", func(t *testing.T) {
		t.Parallel()

		// given
		settings, err := entities.ApplyLayer(&entities.Settings{}, layer)
		require.NoError(t, err)

		// then -- a `providers` block here would make an operator with no configuration of
		// their own, and a GITHUB_TOKEN in their environment, pass validation and have
		// `autoupdate run` walk an organization nobody asked for
		assert.Empty(t, settings.Providers)
	})

	t.Run("should aim no branch deletion", func(t *testing.T) {
		t.Parallel()

		// given
		settings, err := entities.ApplyLayer(&entities.Settings{}, layer)
		require.NoError(t, err)

		// then
		assert.Empty(t, settings.AggregateBranchPrefix,
			"the prefix decides what stale-branch cleanup deletes, so the shipped defaults "+
				"must leave it to the operator")
	})

	t.Run("should enable every updater it ships", func(t *testing.T) {
		t.Parallel()

		// given
		settings, err := entities.ApplyLayer(&entities.Settings{}, layer)
		require.NoError(t, err)

		// then
		require.NotEmpty(t, settings.Updaters)
		for name, updater := range settings.Updaters {
			assert.Truef(t, updater.IsEnabled(), "updater %q must ship enabled", name)
			assert.Falsef(t, updater.IsAutoComplete(),
				"updater %q must not auto-complete its pull requests by default", name)
		}
	})
}
