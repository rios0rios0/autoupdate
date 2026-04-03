//go:build unit

package java_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	javaUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/java"
	repositorydoubles "github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestName(t *testing.T) {
	t.Parallel()

	t.Run("should return java as updater name", func(t *testing.T) {
		t.Parallel()

		// given
		updater := javaUpdater.NewUpdaterRepository()

		// when
		name := updater.Name()

		// then
		assert.Equal(t, "java", name)
	})
}

func TestDetect(t *testing.T) {
	t.Parallel()

	t.Run("should return true when build.gradle exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"build.gradle": true}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := javaUpdater.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.True(t, detected)
	})

	t.Run("should return true when pom.xml exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"pom.xml": true}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := javaUpdater.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.True(t, detected)
	})

	t.Run("should return false when no Java files exist", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := javaUpdater.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.False(t, detected)
	})
}

func TestParseJavaVersionFile(t *testing.T) {
	t.Parallel()

	t.Run("should extract version from simple version file", func(t *testing.T) {
		t.Parallel()

		// given
		content := "21.0.5\n"

		// when
		result := javaUpdater.ParseJavaVersionFile(content)

		// then
		assert.Equal(t, "21.0.5", result)
	})

	t.Run("should return empty when content is empty", func(t *testing.T) {
		t.Parallel()

		// given
		content := ""

		// when
		result := javaUpdater.ParseJavaVersionFile(content)

		// then
		assert.Equal(t, "", result)
	})

	t.Run("should skip comment lines", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Java version\n21.0.5\n"

		// when
		result := javaUpdater.ParseJavaVersionFile(content)

		// then
		assert.Equal(t, "21.0.5", result)
	})

	t.Run("should trim whitespace from version", func(t *testing.T) {
		t.Parallel()

		// given
		content := "  21.0.5  \n"

		// when
		result := javaUpdater.ParseJavaVersionFile(content)

		// then
		assert.Equal(t, "21.0.5", result)
	})
}

func TestExtractMajorVersion(t *testing.T) {
	t.Parallel()

	t.Run("should extract major from full semver", func(t *testing.T) {
		t.Parallel()

		// given
		version := "21.0.5"

		// when
		result := javaUpdater.ExtractMajorVersion(version)

		// then
		assert.Equal(t, "21", result)
	})

	t.Run("should return version unchanged when no dot present", func(t *testing.T) {
		t.Parallel()

		// given
		version := "21"

		// when
		result := javaUpdater.ExtractMajorVersion(version)

		// then
		assert.Equal(t, "21", result)
	})

	t.Run("should return empty for empty input", func(t *testing.T) {
		t.Parallel()

		// given
		version := ""

		// when
		result := javaUpdater.ExtractMajorVersion(version)

		// then
		assert.Equal(t, "", result)
	})
}

func TestDetectLocalBuildSystem(t *testing.T) {
	t.Parallel()

	t.Run("should detect gradle when build.gradle exists", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "build.gradle"), []byte(""), 0o600))

		// when
		result := javaUpdater.DetectLocalBuildSystem(root)

		// then
		assert.Equal(t, "gradle", result)
	})

	t.Run("should detect gradle when build.gradle.kts exists", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "build.gradle.kts"), []byte(""), 0o600))

		// when
		result := javaUpdater.DetectLocalBuildSystem(root)

		// then
		assert.Equal(t, "gradle", result)
	})

	t.Run("should detect maven when pom.xml exists", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "pom.xml"), []byte(""), 0o600))

		// when
		result := javaUpdater.DetectLocalBuildSystem(root)

		// then
		assert.Equal(t, "maven", result)
	})

	t.Run("should default to gradle when no build files exist", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()

		// when
		result := javaUpdater.DetectLocalBuildSystem(root)

		// then
		assert.Equal(t, "gradle", result)
	})
}

func TestResolveVersionContext(t *testing.T) {
	t.Parallel()

	t.Run("should detect version upgrade needed", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".java-version": true}).
			WithFileContents(map[string]string{
				".java-version": "17.0.9\n",
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := javaUpdater.ResolveVersionContext(t.Context(), provider, repo, "21.0.5")

		// then
		require.NotNil(t, vCtx)
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-java-21", vCtx.BranchName)
	})

	t.Run("should detect deps-only when version is current", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".java-version": true}).
			WithFileContents(map[string]string{
				".java-version": "21.0.5\n",
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := javaUpdater.ResolveVersionContext(t.Context(), provider, repo, "21.0.5")

		// then
		require.NotNil(t, vCtx)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-java-deps", vCtx.BranchName)
	})

	t.Run("should use deps branch when no java-version file exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := javaUpdater.ResolveVersionContext(t.Context(), provider, repo, "21.0.5")

		// then
		require.NotNil(t, vCtx)
		assert.Equal(t, "chore/upgrade-java-deps", vCtx.BranchName)
	})

	t.Run("should use deps branch when latest version is empty", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".java-version": true}).
			WithFileContents(map[string]string{
				".java-version": "17.0.9\n",
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := javaUpdater.ResolveVersionContext(t.Context(), provider, repo, "")

		// then
		require.NotNil(t, vCtx)
		assert.Equal(t, "chore/upgrade-java-deps", vCtx.BranchName)
	})
}

func TestBuildBatchJavaScript(t *testing.T) {
	t.Parallel()

	t.Run("should produce gradle variant script", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := javaUpdater.BuildBatchJavaScript("gradle")

		// then
		assert.Contains(t, script, "#!/bin/bash")
		assert.Contains(t, script, "gradlew")
		assert.Contains(t, script, "JAVA_VERSION_UPDATED")
		assert.Contains(t, script, `BUILD_SYSTEM="gradle"`)
	})

	t.Run("should produce maven variant script", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := javaUpdater.BuildBatchJavaScript("maven")

		// then
		assert.Contains(t, script, "#!/bin/bash")
		assert.Contains(t, script, "mvn")
		assert.Contains(t, script, `BUILD_SYSTEM="maven"`)
	})
}

func TestGeneratePRDescription(t *testing.T) {
	t.Parallel()

	t.Run("should include version info for gradle project", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := javaUpdater.GeneratePRDescription("21.0.5", "gradle", true)

		// then
		assert.Contains(t, result, "21.0.5")
		assert.Contains(t, result, ".java-version")
	})

	t.Run("should describe deps-only update for maven project", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := javaUpdater.GeneratePRDescription("21.0.5", "maven", false)

		// then
		assert.Contains(t, result, "dependencies")
	})
}

func TestWriteGitAuth(t *testing.T) {
	t.Parallel()

	t.Run("should generate github auth config", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := javaUpdater.UpgradeParamsExported{
			ProviderName: "github",
			AuthToken:    "test-token",
		}

		// when
		javaUpdater.WriteGitAuth(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "x-access-token")
		assert.Contains(t, result, "github.com")
	})

	t.Run("should generate azuredevops auth config", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := javaUpdater.UpgradeParamsExported{
			ProviderName: "azuredevops",
			AuthToken:    "test-token",
		}

		// when
		javaUpdater.WriteGitAuth(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "dev.azure.com")
	})

	t.Run("should generate gitlab auth config", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := javaUpdater.UpgradeParamsExported{
			ProviderName: "gitlab",
			AuthToken:    "test-token",
		}

		// when
		javaUpdater.WriteGitAuth(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "oauth2")
		assert.Contains(t, result, "gitlab.com")
	})
}
