//go:build unit

package python_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	pyUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/python"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
	"github.com/rios0rios0/autoupdate/test/infrastructure/scriptrunner"
)

// pythonDowngradeRepo is the repository every case in this file resolves against.
var pythonDowngradeRepo = entities.Repository{ //nolint:gochecknoglobals // shared test fixture
	Organization: "org",
	Name:         "repo",
}

func TestPythonDockerfileTagIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	runDockerfileBlock := func(t *testing.T, repoDir, pythonVersion string) {
		t.Helper()

		var sb strings.Builder
		pyUpdater.WriteDockerfileUpdate(&sb)
		scriptrunner.Run(t, repoDir, sb.String(), scriptrunner.Options{
			Env: map[string]string{
				"PYTHON_VERSION":         pythonVersion,
				"PYTHON_VERSION_CHANGED": "true",
			},
		})
	}

	t.Run("should keep a base image newer than the version being rolled out", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, "Dockerfile", "FROM python:3.14-slim\n")

		// when
		runDockerfileBlock(t, repoDir, "3.13.2")

		// then
		assert.Contains(t, scriptrunner.ReadFile(t, repoDir, "Dockerfile"), "FROM python:3.14-slim")
	})

	t.Run("should upgrade a base image older than the version being rolled out", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, "Dockerfile", "FROM python:3.11-slim\n")

		// when
		runDockerfileBlock(t, repoDir, "3.13.2")

		// then
		assert.Contains(t, scriptrunner.ReadFile(t, repoDir, "Dockerfile"), "FROM python:3.13.2-slim")
	})
}

func TestPythonVersionContextIsNeverADowngrade(t *testing.T) {
	t.Parallel()

	t.Run("should report no version upgrade when the pin is ahead of the stable series", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".python-version": true}).
			WithFileContents(map[string]string{".python-version": "3.14.0\n"}).
			BuildSpy()

		// when
		vCtx := pyUpdater.ResolveVersionContext(t.Context(), provider, pythonDowngradeRepo, "3.13.2")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
	})

	t.Run("should report a version upgrade when the pin is behind the stable series", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".python-version": true}).
			WithFileContents(map[string]string{".python-version": "3.11.9\n"}).
			BuildSpy()

		// when
		vCtx := pyUpdater.ResolveVersionContext(t.Context(), provider, pythonDowngradeRepo, "3.13.2")

		// then
		assert.True(t, vCtx.NeedsVersionUpgrade)
	})
}

func TestPythonVersionBlockUsesTheSharedGuard(t *testing.T) {
	t.Parallel()

	t.Run("should compare the target against the pin rather than test inequality", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		pyUpdater.WritePythonUpgradeCommands(&sb, pyUpdater.UpgradeParamsExported{
			Project: pyUpdater.NewPythonProject(false, false, false, false),
		})
		script := sb.String()

		// then
		assert.Contains(t, script,
			"autoupdate_version_is_newer \"$PYTHON_VERSION\" \"$CURRENT_PY_VERSION\"")
		assert.NotContains(t, script, "\"$CURRENT_PY_VERSION\" != \"$PYTHON_VERSION\"")
	})
}
