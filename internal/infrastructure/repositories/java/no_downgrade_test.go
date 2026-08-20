//go:build unit

package java_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	javaUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/java"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
	"github.com/rios0rios0/autoupdate/test/infrastructure/scriptrunner"
)

// javaDowngradeRepo is the repository every case in this file resolves against.
var javaDowngradeRepo = entities.Repository{ //nolint:gochecknoglobals // shared test fixture
	Organization: "org",
	Name:         "repo",
}

func runJavaVersionBlock(t *testing.T, repoDir, javaVersion string) string {
	t.Helper()

	var sb strings.Builder
	javaUpdater.WriteJavaUpgradeCommands(&sb, javaUpdater.UpgradeParamsExported{})

	return scriptrunner.Run(t, repoDir, sb.String(), scriptrunner.Options{
		Env:   map[string]string{"JAVA_VERSION": javaVersion, "BUILD_SYSTEM": "gradle"},
		Stubs: []string{"gradle", "mvn", "java"},
	})
}

func TestJavaVersionPinIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	t.Run("should keep the pin when the repository runs a JDK newer than the LTS", func(t *testing.T) {
		t.Parallel()

		// given — the feed reports the newest LTS, this repository is past it
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, ".java-version", "25\n")

		// when
		output := runJavaVersionBlock(t, repoDir, "21.0.5")

		// then
		assert.Equal(t, "25", scriptrunner.Trimmed(t, repoDir, ".java-version"))
		assert.Contains(t, output, "JAVA_VERSION_UPDATED=false")
	})

	t.Run("should raise the pin when the repository is behind the LTS", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, ".java-version", "17\n")

		// when
		output := runJavaVersionBlock(t, repoDir, "21.0.5")

		// then
		assert.Equal(t, "21.0.5", scriptrunner.Trimmed(t, repoDir, ".java-version"))
		assert.Contains(t, output, "JAVA_VERSION_UPDATED=true")
	})
}

func TestJavaDockerfileTagIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	runDockerfileBlock := func(t *testing.T, repoDir, javaVersion string) {
		t.Helper()

		var sb strings.Builder
		javaUpdater.WriteDockerfileUpdate(&sb)
		scriptrunner.Run(t, repoDir, sb.String(), scriptrunner.Options{
			Env: map[string]string{
				"JAVA_VERSION":         javaVersion,
				"JAVA_VERSION_CHANGED": "true",
			},
		})
	}

	t.Run("should keep a base image newer than the version being rolled out", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, "Dockerfile", "FROM eclipse-temurin:25-jdk\n")

		// when
		runDockerfileBlock(t, repoDir, "21.0.5")

		// then
		assert.Contains(t, scriptrunner.ReadFile(t, repoDir, "Dockerfile"), "FROM eclipse-temurin:25-jdk")
	})

	t.Run("should upgrade a base image older than the version being rolled out", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, "Dockerfile", "FROM eclipse-temurin:17-jdk\n")

		// when
		runDockerfileBlock(t, repoDir, "21.0.5")

		// then
		assert.Contains(t, scriptrunner.ReadFile(t, repoDir, "Dockerfile"), "FROM eclipse-temurin:21-jdk")
	})
}

func TestJavaVersionContextIsNeverADowngrade(t *testing.T) {
	t.Parallel()

	t.Run("should report no version upgrade when the pin is ahead of the LTS", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".java-version": true}).
			WithFileContents(map[string]string{".java-version": "25\n"}).
			BuildSpy()

		// when
		vCtx := javaUpdater.ResolveVersionContext(t.Context(), provider, javaDowngradeRepo, "21.0.5")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
	})

	t.Run("should report a version upgrade when the pin is behind the LTS", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".java-version": true}).
			WithFileContents(map[string]string{".java-version": "17\n"}).
			BuildSpy()

		// when
		vCtx := javaUpdater.ResolveVersionContext(t.Context(), provider, javaDowngradeRepo, "21.0.5")

		// then
		assert.True(t, vCtx.NeedsVersionUpgrade)
	})
}
