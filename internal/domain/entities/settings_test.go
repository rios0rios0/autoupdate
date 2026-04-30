//go:build unit

package entities_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestNewSettings(t *testing.T) {
	t.Parallel()

	t.Run("should return error for non-existent file", func(t *testing.T) {
		t.Parallel()

		// given
		path := "/tmp/non-existent-config-file.yaml"

		// when
		_, err := entities.NewSettings(path)

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read config file")
	})

	t.Run("should return error for invalid YAML", func(t *testing.T) {
		t.Parallel()

		// given
		tmpFile := t.TempDir() + "/bad.yaml"
		require.NoError(t, os.WriteFile(tmpFile, []byte("{invalid yaml: [}"), 0o600))

		// when
		_, err := entities.NewSettings(tmpFile)

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse config file")
	})

	t.Run("should return error for invalid settings", func(t *testing.T) {
		t.Parallel()

		// given
		tmpFile := t.TempDir() + "/empty.yaml"
		require.NoError(t, os.WriteFile(tmpFile, []byte("exclude_forks: true\n"), 0o600))

		// when
		_, err := entities.NewSettings(tmpFile)

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least one provider")
	})
}

func TestDecodeSettings(t *testing.T) {
	t.Parallel()

	t.Run("should decode valid YAML in lenient mode", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte(`
providers:
  - type: github
    token: my-token
    organizations:
      - my-org
updaters:
  terraform:
    enabled: true
`)

		// when
		settings, err := entities.DecodeSettings(data, false)

		// then
		assert.NoError(t, err)
		assert.Len(t, settings.Providers, 1)
		assert.Equal(t, "github", settings.Providers[0].Type)
		assert.True(t, settings.Updaters["terraform"].IsEnabled())
	})

	t.Run("should return error for unknown fields in strict mode", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte(`
providers:
  - type: github
    token: my-token
    organizations:
      - my-org
unknown_field: value
`)

		// when
		_, err := entities.DecodeSettings(data, true)

		// then
		assert.Error(t, err)
	})

	t.Run("should ignore unknown fields in lenient mode", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte(`
providers:
  - type: github
    token: my-token
    organizations:
      - my-org
unknown_field: value
`)

		// when
		settings, err := entities.DecodeSettings(data, false)

		// then
		assert.NoError(t, err)
		assert.Len(t, settings.Providers, 1)
	})

	t.Run("should return error for invalid YAML", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte(`{invalid yaml: [}`)

		// when
		_, err := entities.DecodeSettings(data, false)

		// then
		assert.Error(t, err)
	})
}

func TestValidateSettings(t *testing.T) {
	t.Parallel()

	t.Run("should return nil for valid settings", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{
			Providers: []entities.ProviderConfig{
				{Type: "github", Token: "tok", Organizations: []string{"org"}},
			},
		}

		// when
		err := entities.ValidateSettings(settings)

		// then
		assert.NoError(t, err)
	})

	t.Run("should return error when no providers configured", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{}

		// when
		err := entities.ValidateSettings(settings)

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "at least one provider")
	})

	t.Run("should return error when provider type is missing", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{
			Providers: []entities.ProviderConfig{
				{Token: "tok", Organizations: []string{"org"}},
			},
		}

		// when
		err := entities.ValidateSettings(settings)

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "type is required")
	})

	t.Run("should return error when provider token is missing", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{
			Providers: []entities.ProviderConfig{
				{Type: "github", Organizations: []string{"org"}},
			},
		}

		// when
		err := entities.ValidateSettings(settings)

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "token is required")
	})

	t.Run("should return error when provider organizations are empty", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{
			Providers: []entities.ProviderConfig{
				{Type: "github", Token: "tok"},
			},
		}

		// when
		err := entities.ValidateSettings(settings)

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "organizations must have at least one entry")
	})

	t.Run("should accept valid exclude_repos patterns", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{
			Providers: []entities.ProviderConfig{
				{Type: "github", Token: "tok", Organizations: []string{"org"}},
			},
			ExcludeRepos: []string{
				"rios0rios0/autoupdate",
				"*/oui",
				"zestsecurity/frontend/*",
				"opensearch-dashboards",
			},
		}

		// when
		err := entities.ValidateSettings(settings)

		// then
		assert.NoError(t, err)
	})

	t.Run("should ignore blank exclude_repos entries", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{
			Providers: []entities.ProviderConfig{
				{Type: "github", Token: "tok", Organizations: []string{"org"}},
			},
			ExcludeRepos: []string{"", "   ", "rios0rios0/autoupdate"},
		}

		// when
		err := entities.ValidateSettings(settings)

		// then
		assert.NoError(t, err)
	})

	t.Run("should return error for invalid glob patterns in exclude_repos", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{
			Providers: []entities.ProviderConfig{
				{Type: "github", Token: "tok", Organizations: []string{"org"}},
			},
			ExcludeRepos: []string{"valid/repo", "bad/[unclosed"},
		}

		// when
		err := entities.ValidateSettings(settings)

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exclude_repos[1]")
		assert.Contains(t, err.Error(), "bad/[unclosed")
	})
}

func TestInsertChangelogEntry(t *testing.T) {
	t.Parallel()

	t.Run("should insert entries under Unreleased section", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n"
		entries := []string{"- added new feature X"}

		// when
		result := entities.InsertChangelogEntry(content, entries)

		// then
		assert.Contains(t, result, "- added new feature X")
		assert.Contains(t, result, "[Unreleased]")
	})

	t.Run("should return content unchanged when no Unreleased section exists", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Changelog\n\n## [1.0.0] - 2026-01-01\n"
		entries := []string{"- added something"}

		// when
		result := entities.InsertChangelogEntry(content, entries)

		// then
		assert.Equal(t, content, result)
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
