//go:build unit

package csharp_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	csUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/csharp"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestName(t *testing.T) {
	t.Parallel()

	t.Run("should return csharp as updater name", func(t *testing.T) {
		t.Parallel()

		// given
		updater := csUpdater.NewUpdaterRepository()

		// when
		name := updater.Name()

		// then
		assert.Equal(t, "csharp", name)
	})
}

func TestDetect(t *testing.T) {
	t.Parallel()

	t.Run("should return true when csproj files exist", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{{Path: "App.csproj"}}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := csUpdater.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.True(t, detected)
	})

	t.Run("should return false when no C# files exist", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := csUpdater.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.False(t, detected)
	})
}

func TestParseGlobalJSON(t *testing.T) {
	t.Parallel()

	t.Run("should extract SDK version from valid global.json", func(t *testing.T) {
		t.Parallel()

		// given
		content := `{"sdk":{"version":"8.0.11"}}`

		// when
		result := csUpdater.ParseGlobalJSON(content)

		// then
		assert.Equal(t, "8.0.11", result)
	})

	t.Run("should return empty when sdk field is missing", func(t *testing.T) {
		t.Parallel()

		// given
		content := `{"tools":{}}`

		// when
		result := csUpdater.ParseGlobalJSON(content)

		// then
		assert.Equal(t, "", result)
	})

	t.Run("should return empty for invalid JSON", func(t *testing.T) {
		t.Parallel()

		// given
		content := "{broken"

		// when
		result := csUpdater.ParseGlobalJSON(content)

		// then
		assert.Equal(t, "", result)
	})

	t.Run("should return empty for empty string", func(t *testing.T) {
		t.Parallel()

		// given
		content := ""

		// when
		result := csUpdater.ParseGlobalJSON(content)

		// then
		assert.Equal(t, "", result)
	})

	t.Run("should return empty when version is empty string", func(t *testing.T) {
		t.Parallel()

		// given
		content := `{"sdk":{"version":""}}`

		// when
		result := csUpdater.ParseGlobalJSON(content)

		// then
		assert.Equal(t, "", result)
	})
}

func TestResolveVersionContext(t *testing.T) {
	t.Parallel()

	t.Run("should detect version upgrade needed", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"global.json": true}).
			WithFileContents(map[string]string{
				"global.json": `{"sdk":{"version":"6.0.25"}}`,
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := csUpdater.ResolveVersionContext(t.Context(), provider, repo, "8.0.11")

		// then
		require.NotNil(t, vCtx)
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Contains(t, vCtx.BranchName, "8.0.11")
	})

	t.Run("should detect deps-only when version is current", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"global.json": true}).
			WithFileContents(map[string]string{
				"global.json": `{"sdk":{"version":"8.0.11"}}`,
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := csUpdater.ResolveVersionContext(t.Context(), provider, repo, "8.0.11")

		// then
		require.NotNil(t, vCtx)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Contains(t, vCtx.BranchName, "deps")
	})

	t.Run("should use deps branch when no global.json exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := csUpdater.ResolveVersionContext(t.Context(), provider, repo, "8.0.11")

		// then
		require.NotNil(t, vCtx)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-dotnet-deps", vCtx.BranchName)
	})

	t.Run("should use deps branch when latest version is empty", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"global.json": true}).
			WithFileContents(map[string]string{
				"global.json": `{"sdk":{"version":"6.0.25"}}`,
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := csUpdater.ResolveVersionContext(t.Context(), provider, repo, "")

		// then
		require.NotNil(t, vCtx)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-dotnet-deps", vCtx.BranchName)
	})
}

func TestBuildBatchDotnetScript(t *testing.T) {
	t.Parallel()

	t.Run("should produce a valid bash script with dotnet commands", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := csUpdater.BuildBatchDotnetScript()

		// then
		assert.True(t, strings.HasPrefix(script, "#!/bin/bash\n"))
		assert.Contains(t, script, "set -euo pipefail")
		assert.Contains(t, script, "DOTNET_VERSION_UPDATED")
		assert.Contains(t, script, "dotnet")
	})
}

func TestWriteDotnetUpgradeCommands(t *testing.T) {
	t.Parallel()

	t.Run("should contain jq and python3 fallback chain", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder

		// when
		csUpdater.WriteDotnetUpgradeCommands(&sb)
		result := sb.String()

		// then
		assert.Contains(t, result, "jq")
		assert.Contains(t, result, "python3")
		assert.Contains(t, result, "global.json")
		assert.Contains(t, result, "DOTNET_VERSION_UPDATED")
	})
}

func TestGeneratePRDescription(t *testing.T) {
	t.Parallel()

	t.Run("should include version info when SDK version was updated", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := csUpdater.GeneratePRDescription("8.0.11", true)

		// then
		assert.Contains(t, result, "8.0.11")
		assert.Contains(t, result, "global.json")
	})

	t.Run("should describe deps-only update when no version change", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := csUpdater.GeneratePRDescription("8.0.11", false)

		// then
		assert.Contains(t, result, "NuGet")
	})
}

func TestWriteGitAuth(t *testing.T) {
	t.Parallel()

	t.Run("should generate github auth config", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := csUpdater.UpgradeParamsExported{
			ProviderName: "github",
			AuthToken:    "ghp_token",
		}

		// when
		csUpdater.WriteGitAuth(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "x-access-token")
		assert.Contains(t, result, "github.com")
	})

	t.Run("should generate azuredevops auth config", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := csUpdater.UpgradeParamsExported{
			ProviderName: "azuredevops",
			AuthToken:    "ado_pat",
		}

		// when
		csUpdater.WriteGitAuth(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "dev.azure.com")
	})

	t.Run("should generate gitlab auth config", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := csUpdater.UpgradeParamsExported{
			ProviderName: "gitlab",
			AuthToken:    "gl_token",
		}

		// when
		csUpdater.WriteGitAuth(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "oauth2")
		assert.Contains(t, result, "gitlab.com")
	})
}
