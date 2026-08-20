//go:build unit

package csharp_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	csUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/csharp"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
	"github.com/rios0rios0/autoupdate/test/infrastructure/scriptrunner"
)

// dotnetDowngradeRepo is the repository every case in this file resolves against.
var dotnetDowngradeRepo = entities.Repository{ //nolint:gochecknoglobals // shared test fixture
	Organization: "org",
	Name:         "repo",
}

func globalJSONWith(version string) string {
	return "{\n  \"sdk\": {\n    \"version\": \"" + version + "\"\n  }\n}\n"
}

func TestDotnetDockerfileTagIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	runDockerfileBlock := func(t *testing.T, repoDir, dotnetVersion string) {
		t.Helper()

		var sb strings.Builder
		csUpdater.WriteDockerfileUpdate(&sb)
		scriptrunner.Run(t, repoDir, sb.String(), scriptrunner.Options{
			Env: map[string]string{
				"DOTNET_VERSION":         dotnetVersion,
				"DOTNET_VERSION_CHANGED": "true",
			},
		})
	}

	t.Run("should keep base images newer than the SDK being rolled out", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, "Dockerfile",
			"FROM mcr.microsoft.com/dotnet/sdk:10.0 AS build\n"+
				"FROM mcr.microsoft.com/dotnet/aspnet:10.0 AS runtime\n")

		// when
		runDockerfileBlock(t, repoDir, "8.0.404")

		// then
		content := scriptrunner.ReadFile(t, repoDir, "Dockerfile")
		assert.Contains(t, content, "mcr.microsoft.com/dotnet/sdk:10.0")
		assert.Contains(t, content, "mcr.microsoft.com/dotnet/aspnet:10.0")
	})

	t.Run("should upgrade base images older than the SDK being rolled out", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, "Dockerfile",
			"FROM mcr.microsoft.com/dotnet/sdk:6.0 AS build\n"+
				"FROM mcr.microsoft.com/dotnet/runtime:6.0 AS runtime\n")

		// when
		runDockerfileBlock(t, repoDir, "8.0.404")

		// then
		content := scriptrunner.ReadFile(t, repoDir, "Dockerfile")
		assert.Contains(t, content, "mcr.microsoft.com/dotnet/sdk:8.0")
		assert.Contains(t, content, "mcr.microsoft.com/dotnet/runtime:8.0")
	})
}

func TestDotnetVersionContextIsNeverADowngrade(t *testing.T) {
	t.Parallel()

	t.Run("should report no version upgrade when global.json is ahead of the feed", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"global.json": true}).
			WithFileContents(map[string]string{"global.json": globalJSONWith("10.0.100")}).
			BuildSpy()

		// when
		vCtx := csUpdater.ResolveVersionContext(t.Context(), provider, dotnetDowngradeRepo, "8.0.404")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
	})

	t.Run("should report no version upgrade when a release replaces a newer pre-release", func(t *testing.T) {
		t.Parallel()

		// given — an SDK preview is deliberately ahead of the stable channel
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"global.json": true}).
			WithFileContents(map[string]string{"global.json": globalJSONWith("9.0.100-rc.2.24474.11")}).
			BuildSpy()

		// when
		vCtx := csUpdater.ResolveVersionContext(t.Context(), provider, dotnetDowngradeRepo, "8.0.404")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
	})

	t.Run("should report a version upgrade when global.json is behind the feed", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"global.json": true}).
			WithFileContents(map[string]string{"global.json": globalJSONWith("6.0.400")}).
			BuildSpy()

		// when
		vCtx := csUpdater.ResolveVersionContext(t.Context(), provider, dotnetDowngradeRepo, "8.0.404")

		// then
		assert.True(t, vCtx.NeedsVersionUpgrade)
	})
}
