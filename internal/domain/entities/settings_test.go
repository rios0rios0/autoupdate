//go:build unit

package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

func boolPtr(v bool) *bool { return &v }

func TestIsEnabled(t *testing.T) {
	t.Parallel()

	t.Run("should return true when Enabled is nil", func(t *testing.T) {
		// given
		cfg := entities.UpdaterConfig{}

		// when
		result := cfg.IsEnabled()

		// then
		assert.True(t, result)
	})

	t.Run("should return true when Enabled is true", func(t *testing.T) {
		// given
		cfg := entities.UpdaterConfig{Enabled: boolPtr(true)}

		// when
		result := cfg.IsEnabled()

		// then
		assert.True(t, result)
	})

	t.Run("should return false when Enabled is false", func(t *testing.T) {
		// given
		cfg := entities.UpdaterConfig{Enabled: boolPtr(false)}

		// when
		result := cfg.IsEnabled()

		// then
		assert.False(t, result)
	})
}

func TestIsAutoComplete(t *testing.T) {
	t.Parallel()

	t.Run("should return false when AutoComplete is nil", func(t *testing.T) {
		// given
		cfg := entities.UpdaterConfig{}

		// when
		result := cfg.IsAutoComplete()

		// then
		assert.False(t, result)
	})

	t.Run("should return true when AutoComplete is true", func(t *testing.T) {
		// given
		cfg := entities.UpdaterConfig{AutoComplete: boolPtr(true)}

		// when
		result := cfg.IsAutoComplete()

		// then
		assert.True(t, result)
	})

	t.Run("should return false when AutoComplete is false", func(t *testing.T) {
		// given
		cfg := entities.UpdaterConfig{AutoComplete: boolPtr(false)}

		// when
		result := cfg.IsAutoComplete()

		// then
		assert.False(t, result)
	})
}

func TestMergeUpdatersConfig(t *testing.T) {
	t.Parallel()

	t.Run("should keep all defaults when overrides is empty", func(t *testing.T) {
		// given
		defaults := map[string]entities.UpdaterConfig{
			"terraform": {Enabled: boolPtr(true), AutoComplete: boolPtr(false)},
			"golang":    {Enabled: boolPtr(true), AutoComplete: boolPtr(false)},
		}
		overrides := map[string]entities.UpdaterConfig{}

		// when
		result := entities.MergeUpdatersConfig(defaults, overrides)

		// then
		assert.Len(t, result, 2)
		assert.True(t, result["terraform"].IsEnabled())
		assert.False(t, result["terraform"].IsAutoComplete())
		assert.True(t, result["golang"].IsEnabled())
	})

	t.Run("should override enabled when user provides non-nil value", func(t *testing.T) {
		// given
		defaults := map[string]entities.UpdaterConfig{
			"terraform": {Enabled: boolPtr(true), AutoComplete: boolPtr(false)},
		}
		overrides := map[string]entities.UpdaterConfig{
			"terraform": {Enabled: boolPtr(false)},
		}

		// when
		result := entities.MergeUpdatersConfig(defaults, overrides)

		// then
		assert.False(t, result["terraform"].IsEnabled())
		assert.False(t, result["terraform"].IsAutoComplete())
	})

	t.Run("should override auto_complete when user provides non-nil value", func(t *testing.T) {
		// given
		defaults := map[string]entities.UpdaterConfig{
			"terraform": {Enabled: boolPtr(true), AutoComplete: boolPtr(false)},
		}
		overrides := map[string]entities.UpdaterConfig{
			"terraform": {AutoComplete: boolPtr(true)},
		}

		// when
		result := entities.MergeUpdatersConfig(defaults, overrides)

		// then
		assert.True(t, result["terraform"].IsEnabled())
		assert.True(t, result["terraform"].IsAutoComplete())
	})

	t.Run("should override target_branch when user provides non-empty value", func(t *testing.T) {
		// given
		defaults := map[string]entities.UpdaterConfig{
			"terraform": {Enabled: boolPtr(true)},
		}
		overrides := map[string]entities.UpdaterConfig{
			"terraform": {TargetBranch: "develop"},
		}

		// when
		result := entities.MergeUpdatersConfig(defaults, overrides)

		// then
		assert.Equal(t, "develop", result["terraform"].TargetBranch)
		assert.True(t, result["terraform"].IsEnabled())
	})

	t.Run("should keep default fields when user provides only target_branch", func(t *testing.T) {
		// given
		defaults := map[string]entities.UpdaterConfig{
			"golang": {Enabled: boolPtr(true), AutoComplete: boolPtr(false), TargetBranch: "main"},
		}
		overrides := map[string]entities.UpdaterConfig{
			"golang": {TargetBranch: "develop"},
		}

		// when
		result := entities.MergeUpdatersConfig(defaults, overrides)

		// then
		assert.True(t, result["golang"].IsEnabled())
		assert.False(t, result["golang"].IsAutoComplete())
		assert.Equal(t, "develop", result["golang"].TargetBranch)
	})

	t.Run("should add new updater not present in defaults", func(t *testing.T) {
		// given
		defaults := map[string]entities.UpdaterConfig{
			"terraform": {Enabled: boolPtr(true)},
		}
		overrides := map[string]entities.UpdaterConfig{
			"custom": {Enabled: boolPtr(true), TargetBranch: "main"},
		}

		// when
		result := entities.MergeUpdatersConfig(defaults, overrides)

		// then
		assert.Len(t, result, 2)
		assert.True(t, result["custom"].IsEnabled())
		assert.Equal(t, "main", result["custom"].TargetBranch)
	})

	t.Run("should keep default updater untouched when not in overrides", func(t *testing.T) {
		// given
		defaults := map[string]entities.UpdaterConfig{
			"terraform": {Enabled: boolPtr(true), AutoComplete: boolPtr(false)},
			"golang":    {Enabled: boolPtr(true), AutoComplete: boolPtr(true), TargetBranch: "main"},
		}
		overrides := map[string]entities.UpdaterConfig{
			"terraform": {AutoComplete: boolPtr(true)},
		}

		// when
		result := entities.MergeUpdatersConfig(defaults, overrides)

		// then
		assert.True(t, result["terraform"].IsAutoComplete())
		assert.True(t, result["golang"].IsAutoComplete())
		assert.Equal(t, "main", result["golang"].TargetBranch)
	})
}
