package entities_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

//go:fix inline
func TestIsEnabled(t *testing.T) {
	t.Parallel()

	t.Run("should return true when Enabled is nil", func(t *testing.T) {
		t.Parallel()

		// given
		cfg := entities.UpdaterConfig{}

		// when
		result := cfg.IsEnabled()

		// then
		assert.True(t, result)
	})

	t.Run("should return true when Enabled is true", func(t *testing.T) {
		t.Parallel()

		// given
		cfg := entities.UpdaterConfig{Enabled: new(true)}

		// when
		result := cfg.IsEnabled()

		// then
		assert.True(t, result)
	})

	t.Run("should return false when Enabled is false", func(t *testing.T) {
		t.Parallel()

		// given
		cfg := entities.UpdaterConfig{Enabled: new(false)}

		// when
		result := cfg.IsEnabled()

		// then
		assert.False(t, result)
	})
}

func TestIsAutoComplete(t *testing.T) {
	t.Parallel()

	t.Run("should return false when AutoComplete is nil", func(t *testing.T) {
		t.Parallel()

		// given
		cfg := entities.UpdaterConfig{}

		// when
		result := cfg.IsAutoComplete()

		// then
		assert.False(t, result)
	})

	t.Run("should return true when AutoComplete is true", func(t *testing.T) {
		t.Parallel()

		// given
		cfg := entities.UpdaterConfig{AutoComplete: new(true)}

		// when
		result := cfg.IsAutoComplete()

		// then
		assert.True(t, result)
	})

	t.Run("should return false when AutoComplete is false", func(t *testing.T) {
		t.Parallel()

		// given
		cfg := entities.UpdaterConfig{AutoComplete: new(false)}

		// when
		result := cfg.IsAutoComplete()

		// then
		assert.False(t, result)
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
		err := entities.ValidateSettings(settings, true)

		// then
		assert.NoError(t, err)
	})

	t.Run("should return error when no providers configured", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{}

		// when
		err := entities.ValidateSettings(settings, true)

		// then
		require.Error(t, err)
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
		err := entities.ValidateSettings(settings, true)

		// then
		require.Error(t, err)
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
		err := entities.ValidateSettings(settings, true)

		// then
		require.Error(t, err)
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
		err := entities.ValidateSettings(settings, true)

		// then
		require.Error(t, err)
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
				"contososecurity/frontend/*",
				"opensearch-dashboards",
			},
		}

		// when
		err := entities.ValidateSettings(settings, true)

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
		err := entities.ValidateSettings(settings, true)

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
		err := entities.ValidateSettings(settings, true)

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
		t.Parallel()

		// given
		defaults := map[string]entities.UpdaterConfig{
			"terraform": {Enabled: new(true), AutoComplete: new(false)},
			"golang":    {Enabled: new(true), AutoComplete: new(false)},
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
		t.Parallel()

		// given
		defaults := map[string]entities.UpdaterConfig{
			"terraform": {Enabled: new(true), AutoComplete: new(false)},
		}
		overrides := map[string]entities.UpdaterConfig{
			"terraform": {Enabled: new(false)},
		}

		// when
		result := entities.MergeUpdatersConfig(defaults, overrides)

		// then
		assert.False(t, result["terraform"].IsEnabled())
		assert.False(t, result["terraform"].IsAutoComplete())
	})

	t.Run("should override auto_complete when user provides non-nil value", func(t *testing.T) {
		t.Parallel()

		// given
		defaults := map[string]entities.UpdaterConfig{
			"terraform": {Enabled: new(true), AutoComplete: new(false)},
		}
		overrides := map[string]entities.UpdaterConfig{
			"terraform": {AutoComplete: new(true)},
		}

		// when
		result := entities.MergeUpdatersConfig(defaults, overrides)

		// then
		assert.True(t, result["terraform"].IsEnabled())
		assert.True(t, result["terraform"].IsAutoComplete())
	})

	t.Run("should override target_branch when user provides non-empty value", func(t *testing.T) {
		t.Parallel()

		// given
		defaults := map[string]entities.UpdaterConfig{
			"terraform": {Enabled: new(true)},
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
		t.Parallel()

		// given
		defaults := map[string]entities.UpdaterConfig{
			"golang": {Enabled: new(true), AutoComplete: new(false), TargetBranch: "main"},
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
		t.Parallel()

		// given
		defaults := map[string]entities.UpdaterConfig{
			"terraform": {Enabled: new(true)},
		}
		overrides := map[string]entities.UpdaterConfig{
			"custom": {Enabled: new(true), TargetBranch: "main"},
		}

		// when
		result := entities.MergeUpdatersConfig(defaults, overrides)

		// then
		assert.Len(t, result, 2)
		assert.True(t, result["custom"].IsEnabled())
		assert.Equal(t, "main", result["custom"].TargetBranch)
	})

	t.Run("should keep default updater untouched when not in overrides", func(t *testing.T) {
		t.Parallel()

		// given
		defaults := map[string]entities.UpdaterConfig{
			"terraform": {Enabled: new(true), AutoComplete: new(false)},
			"golang":    {Enabled: new(true), AutoComplete: new(true), TargetBranch: "main"},
		}
		overrides := map[string]entities.UpdaterConfig{
			"terraform": {AutoComplete: new(true)},
		}

		// when
		result := entities.MergeUpdatersConfig(defaults, overrides)

		// then
		assert.True(t, result["terraform"].IsAutoComplete())
		assert.True(t, result["golang"].IsAutoComplete())
		assert.Equal(t, "main", result["golang"].TargetBranch)
	})
}

func TestValidateAggregateBranchPrefix(t *testing.T) {
	t.Parallel()

	// The prefix is not only what new branches are named after -- it is the argument to a
	// destructive operation. Stale-branch cleanup deletes every remote branch starting with
	// it and closes the pull request attached to each, so a prefix wider than the operator
	// meant does not produce a confusing branch name, it deletes other people's work. An
	// operator's typo is as capable of that as a hostile repository would be.
	accepted := []string{
		"",                  // unset means the default, which is valid by construction
		"chore/autoupdate-", // AutoUpdate's own
		"chore/bump-",       // AutoBump's, in the same namespace
		"deps/autoupdate-",
		"a/b",
	}
	for _, prefix := range accepted {
		t.Run("should accept "+strconv.Quote(prefix), func(t *testing.T) {
			t.Parallel()

			// when
			err := entities.ValidateAggregateBranchPrefix(prefix)

			// then
			assert.NoError(t, err)
		})
	}

	rejected := map[string]string{
		"an empty prefix matches every branch":          "   ",
		"a bare name can escape the namespace":          "autoupdate-",
		"a protected branch name":                       "main",
		"another protected branch name":                 "MASTER",
		"a bare namespace sweeps every tool's branches": "chore/",
		"a refs/ prefix silently matches nothing":       "refs/heads/autoupdate-",
		"a name git will not accept":                    "chore/autoupdate ",
		"a double slash":                                "chore//autoupdate-",
		"a parent traversal":                            "chore/../autoupdate-",
		"a leading dash":                                "-chore/autoupdate-",
		"a leading slash":                               "/chore/autoupdate-",
		"a .lock suffix":                                "chore/autoupdate.lock",
		"a glob character":                              "chore/autoupdate-*",
		"a control character":                           "chore/autoupdate-\x01",
	}
	for reason, prefix := range rejected {
		t.Run("should reject "+reason, func(t *testing.T) {
			t.Parallel()

			// when
			err := entities.ValidateAggregateBranchPrefix(prefix)

			// then
			require.ErrorIs(t, err, entities.ErrAggregateBranchPrefixInvalid,
				"prefix %q must be rejected", prefix)
		})
	}

	t.Run("should accept the default prefix", func(t *testing.T) {
		t.Parallel()

		// when
		err := entities.ValidateAggregateBranchPrefix(entities.DefaultAggregateBranchPrefix)

		// then
		assert.NoError(t, err, "the default must satisfy the rules it is offered as the fix for")
	})
}
