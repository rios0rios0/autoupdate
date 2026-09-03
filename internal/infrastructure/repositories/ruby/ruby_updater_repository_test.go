package ruby_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	rbUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/ruby"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestName(t *testing.T) {
	t.Parallel()

	t.Run("should return ruby as updater name", func(t *testing.T) {
		t.Parallel()

		// given
		updater := rbUpdater.NewUpdaterRepository()

		// when
		name := updater.Name()

		// then
		assert.Equal(t, "ruby", name)
	})
}

func TestDetect(t *testing.T) {
	t.Parallel()

	t.Run("should return true when Gemfile exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"Gemfile": true}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := rbUpdater.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.True(t, detected)
	})

	t.Run("should return false when no Ruby files exist", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := rbUpdater.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.False(t, detected)
	})
}

func TestParseRubyVersionFile(t *testing.T) {
	t.Parallel()

	t.Run("should extract version from simple version file", func(t *testing.T) {
		t.Parallel()

		// given
		content := "3.3.6\n"

		// when
		result := rbUpdater.ParseRubyVersionFile(content)

		// then
		assert.Equal(t, "3.3.6", result)
	})

	t.Run("should return empty when content is empty", func(t *testing.T) {
		t.Parallel()

		// given
		content := ""

		// when
		result := rbUpdater.ParseRubyVersionFile(content)

		// then
		assert.Empty(t, result)
	})

	t.Run("should skip comment lines", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# comment\n3.3.6\n"

		// when
		result := rbUpdater.ParseRubyVersionFile(content)

		// then
		assert.Equal(t, "3.3.6", result)
	})

	t.Run("should trim whitespace from version", func(t *testing.T) {
		t.Parallel()

		// given
		content := "  3.3.6  \n"

		// when
		result := rbUpdater.ParseRubyVersionFile(content)

		// then
		assert.Equal(t, "3.3.6", result)
	})
}

func TestResolveVersionContext(t *testing.T) {
	t.Parallel()

	t.Run("should detect version upgrade needed", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".ruby-version": true}).
			WithFileContents(map[string]string{
				".ruby-version": "3.2.0\n",
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := rbUpdater.ResolveVersionContext(t.Context(), provider, repo, "3.3.6", true)

		// then
		require.NotNil(t, vCtx)
		assert.Equal(t, "3.3.6", vCtx.LatestVersion)
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Contains(t, vCtx.BranchName, "3.3.6")
	})

	t.Run("should detect deps-only when version is current", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".ruby-version": true}).
			WithFileContents(map[string]string{
				".ruby-version": "3.3.6\n",
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := rbUpdater.ResolveVersionContext(t.Context(), provider, repo, "3.3.6", true)

		// then
		require.NotNil(t, vCtx)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Contains(t, vCtx.BranchName, "deps")
	})

	t.Run("should use deps branch when no ruby-version file exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := rbUpdater.ResolveVersionContext(t.Context(), provider, repo, "3.3.6", true)

		// then
		require.NotNil(t, vCtx)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-ruby-deps", vCtx.BranchName)
	})

	t.Run("should use deps branch when latest version is empty", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".ruby-version": true}).
			WithFileContents(map[string]string{
				".ruby-version": "3.2.0\n",
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := rbUpdater.ResolveVersionContext(t.Context(), provider, repo, "", true)

		// then
		require.NotNil(t, vCtx)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-ruby-deps", vCtx.BranchName)
	})
}

func TestGeneratePRDescription(t *testing.T) {
	t.Parallel()

	t.Run("should include version info when Ruby version was updated", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := rbUpdater.GeneratePRDescription("3.3.6", true)

		// then
		assert.Contains(t, result, "3.3.6")
		assert.Contains(t, result, ".ruby-version")
	})

	t.Run("should describe deps-only update when no version change", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := rbUpdater.GeneratePRDescription("3.3.6", false)

		// then
		assert.Contains(t, result, "dependencies")
	})
}

func TestBuildBatchRubyScript(t *testing.T) {
	t.Parallel()

	t.Run("should produce a valid bash script with ruby commands", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := rbUpdater.BuildBatchRubyScript(true)

		// then
		assert.True(t, strings.HasPrefix(script, "#!/bin/bash\n"))
		assert.Contains(t, script, "set -euo pipefail")
		assert.Contains(t, script, "bundle update")
		assert.Contains(t, script, "RUBY_VERSION_UPDATED")
	})
}

func TestBuildUpgradeScript(t *testing.T) {
	t.Parallel()

	t.Run("should produce valid upgrade script with git operations", func(t *testing.T) {
		t.Parallel()

		// given
		params := rbUpdater.UpgradeParamsExported{
			CloneURL:      "https://github.com/org/repo.git",
			DefaultBranch: "main",
			BranchName:    "chore/upgrade-ruby-3.3.6",
			RubyVersion:   "3.3.6",
			AuthToken:     "token",
			ProviderName:  "github",
		}

		// when
		script := rbUpdater.BuildUpgradeScript(params, "/tmp/repo", true)

		// then
		assert.True(t, strings.HasPrefix(script, "#!/bin/bash\n"))
		assert.Contains(t, script, "git clone")
		assert.Contains(t, script, "git checkout -b")
		assert.Contains(t, script, "CHANGES_PUSHED")
	})
}

func TestWriteGitAuth(t *testing.T) {
	t.Parallel()

	t.Run("should generate github auth config", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := rbUpdater.UpgradeParamsExported{
			ProviderName: "github",
			AuthToken:    "ghp_token",
		}

		// when
		rbUpdater.WriteGitAuth(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "x-access-token")
		assert.Contains(t, result, "github.com")
	})

	t.Run("should generate azuredevops auth config", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := rbUpdater.UpgradeParamsExported{
			ProviderName: "azuredevops",
			AuthToken:    "ado_pat",
		}

		// when
		rbUpdater.WriteGitAuth(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "dev.azure.com")
	})

	t.Run("should generate gitlab auth config", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := rbUpdater.UpgradeParamsExported{
			ProviderName: "gitlab",
			AuthToken:    "gl_token",
		}

		// when
		rbUpdater.WriteGitAuth(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "oauth2")
		assert.Contains(t, result, "gitlab.com")
	})
}

// TestRubyGemMajorMode covers the bundler ceiling. Bundler has no flag meaning
// "raise the Gemfile bounds", and this deliberately does not rewrite one — but
// it does have a cap on the bump it will take, which is the direction that was
// missing: an unconstrained `gem "rails"` crossed majors whatever the key said.
func TestRubyGemMajorMode(t *testing.T) {
	t.Parallel()

	t.Run("should let bundler take a major when allowed", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder

		// when
		rbUpdater.WriteRubyUpgradeCommands(&sb, true)

		// then -- --major is bundler's default, so it is left off rather than
		// spelled out
		script := sb.String()
		assert.Contains(t, script, "bundle update 2>&1")
		assert.NotContains(t, script, "--minor")
	})

	t.Run("should widen the Gemfile before resolving when allowed", func(t *testing.T) {
		t.Parallel()

		// given -- no bundler flag can raise the Gemfile's own bounds, so a
		// `~> 6.0` would otherwise hold the gem on 6.x whatever the key says
		var sb strings.Builder

		// when
		rbUpdater.WriteRubyUpgradeCommands(&sb, true)

		// then -- and the widening has to precede the resolution, or the
		// lockfile is resolved against the ceiling that was just removed
		script := sb.String()
		assert.Contains(t, script, "autoupdate_relax_gemfile_constraints Gemfile")
		assert.Less(t,
			strings.Index(script, "autoupdate_relax_gemfile_constraints Gemfile"),
			strings.Index(script, "bundle update"),
			"the Gemfile must be widened before bundle update runs")
	})

	t.Run("should not touch the Gemfile when majors are refused", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder

		// when
		rbUpdater.WriteRubyUpgradeCommands(&sb, false)

		// then -- refusing is bundler's own ceiling, and needs no manifest edit
		script := sb.String()
		assert.NotContains(t, script, "autoupdate_relax_gemfile_constraints")
		assert.Contains(t, script, "bundle update --minor")
	})

	t.Run("should cap bundler below a major when refused", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder

		// when
		rbUpdater.WriteRubyUpgradeCommands(&sb, false)

		// then
		assert.Contains(t, sb.String(), "bundle update --minor")
	})
}

// TestResolveVersionContextMajorMode covers the `.ruby-version` pin under a
// refusal, and that the refusal reaches the script that rewrites the file.
//
// The pin is rewritten by support.VersionPinUpdateScript from
// TARGET_RUBY_VERSION, and the script's only gate is
// `autoupdate_version_is_newer`, which has no major-version concept. Withholding
// the value is how the refusal crosses the Go/bash seam.
func TestResolveVersionContextMajorMode(t *testing.T) {
	t.Parallel()

	newProvider := func() *repositorydoubles.SpyProviderRepository {
		return repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".ruby-version": true}).
			WithFileContents(map[string]string{".ruby-version": "3.2.0\n"}).
			BuildSpy()
	}
	repo := entities.Repository{
		Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main",
	}

	t.Run("should hold a major bump when majors are refused", func(t *testing.T) {
		t.Parallel()

		// given / when -- 3.2.0 pinned, 4.0.0 offered
		vCtx := rbUpdater.ResolveVersionContext(t.Context(), newProvider(), repo, "4.0.0", false)

		// then
		require.NotNil(t, vCtx)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Empty(t, rbUpdater.RubyVersionFor(vCtx),
			"a refused version must not reach the script that rewrites .ruby-version")
	})

	t.Run("should take a minor bump when majors are refused", func(t *testing.T) {
		t.Parallel()

		// given / when
		vCtx := rbUpdater.ResolveVersionContext(t.Context(), newProvider(), repo, "3.3.6", false)

		// then
		require.NotNil(t, vCtx)
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "3.3.6", rbUpdater.RubyVersionFor(vCtx))
	})
}
