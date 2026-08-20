//go:build unit

package javascript_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	jsUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/javascript"
	"github.com/rios0rios0/autoupdate/test/infrastructure/scriptrunner"
)

// nodeVersionFiles are the two pins the upgrade script rewrites in step.
var nodeVersionFiles = []string{".nvmrc", ".node-version"} //nolint:gochecknoglobals // shared test fixture

// runNodeVersionBlock executes the emitted Node.js version block against a
// repository, with the package manager stubbed out so the run stays hermetic.
func runNodeVersionBlock(t *testing.T, repoDir, nodeVersion string) string {
	t.Helper()

	return scriptrunner.Run(t, repoDir, jsUpdater.WriteJSUpgradeCommands(jsUpdater.UpgradeParams{}),
		scriptrunner.Options{
			Env:   map[string]string{"NODE_VERSION": nodeVersion, "PACKAGE_MANAGER": "npm"},
			Stubs: []string{"npm", "pnpm", "yarn"},
		})
}

func TestNodeVersionPinIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	t.Run("should keep the pin when the repository tracks a release newer than the LTS line", func(t *testing.T) {
		t.Parallel()

		// given — the repository is on a current release; the feed reports LTS
		repoDir := t.TempDir()
		for _, file := range nodeVersionFiles {
			scriptrunner.WriteFile(t, repoDir, file, "26.7.0\n")
		}

		// when
		output := runNodeVersionBlock(t, repoDir, "24.19.0")

		// then
		for _, file := range nodeVersionFiles {
			assert.Equal(t, "26.7.0", scriptrunner.Trimmed(t, repoDir, file),
				"%s was rewritten backwards", file)
		}
		assert.Contains(t, output, "NODE_VERSION_UPDATED=false")
		assert.NotContains(t, output, "NODE_VERSION_UPDATED=true")
	})

	t.Run("should raise the pin when the repository is behind the latest release", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		for _, file := range nodeVersionFiles {
			scriptrunner.WriteFile(t, repoDir, file, "20.11.1\n")
		}

		// when
		output := runNodeVersionBlock(t, repoDir, "24.19.0")

		// then
		for _, file := range nodeVersionFiles {
			assert.Equal(t, "24.19.0", scriptrunner.Trimmed(t, repoDir, file))
		}
		assert.Contains(t, output, "NODE_VERSION_UPDATED=true")
	})

	t.Run("should keep the pin when it already names the latest release", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, ".nvmrc", "24.19.0\n")

		// when
		output := runNodeVersionBlock(t, repoDir, "24.19.0")

		// then
		assert.Equal(t, "24.19.0", scriptrunner.Trimmed(t, repoDir, ".nvmrc"))
		assert.Contains(t, output, "NODE_VERSION_UPDATED=false")
	})

	t.Run("should keep an alias pin that names no version at all", func(t *testing.T) {
		t.Parallel()

		// given — "lts/*" is a deliberate choice, not a stale version number
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, ".nvmrc", "lts/*\n")

		// when
		output := runNodeVersionBlock(t, repoDir, "24.19.0")

		// then
		assert.Equal(t, "lts/*", scriptrunner.Trimmed(t, repoDir, ".nvmrc"))
		assert.Contains(t, output, "NODE_VERSION_UPDATED=false")
	})
}

func TestNodeDockerfileTagIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	runDockerfileBlock := func(t *testing.T, repoDir, nodeVersion string) string {
		t.Helper()

		return scriptrunner.Run(t, repoDir, jsUpdater.WriteDockerfileUpdate(), scriptrunner.Options{
			Env: map[string]string{"NODE_VERSION": nodeVersion, "NODE_VERSION_CHANGED": "true"},
		})
	}

	t.Run("should keep a base image newer than the version being rolled out", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, "Dockerfile", "FROM node:26-alpine\nRUN npm ci\n")

		// when
		runDockerfileBlock(t, repoDir, "24.19.0")

		// then
		assert.Contains(t, scriptrunner.ReadFile(t, repoDir, "Dockerfile"), "FROM node:26-alpine")
	})

	t.Run("should upgrade a base image older than the version being rolled out", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, "Dockerfile", "FROM node:20-alpine\nRUN npm ci\n")

		// when
		runDockerfileBlock(t, repoDir, "24.19.0")

		// then
		assert.Contains(t, scriptrunner.ReadFile(t, repoDir, "Dockerfile"), "FROM node:24.19.0-alpine")
	})

	t.Run("should keep every stage when any one of them is already ahead", func(t *testing.T) {
		t.Parallel()

		// given — rewriting the older stage alone would still leave the file
		// claiming an upgrade while moving the runtime stage backwards
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, "Dockerfile",
			"FROM node:20-alpine AS build\nFROM node:26-alpine AS runtime\n")

		// when
		runDockerfileBlock(t, repoDir, "24.19.0")

		// then
		content := scriptrunner.ReadFile(t, repoDir, "Dockerfile")
		assert.Contains(t, content, "FROM node:20-alpine AS build")
		assert.Contains(t, content, "FROM node:26-alpine AS runtime")
	})

	t.Run("should leave a Dockerfile pinning no numeric node tag alone", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, "Dockerfile", "FROM node:alpine\n")

		// when
		runDockerfileBlock(t, repoDir, "24.19.0")

		// then
		assert.Equal(t, "FROM node:alpine\n", scriptrunner.ReadFile(t, repoDir, "Dockerfile"))
	})
}

func TestNodeVersionBlockUsesTheSharedGuard(t *testing.T) {
	t.Parallel()

	t.Run("should compare the target against the pin rather than test inequality", func(t *testing.T) {
		t.Parallel()

		// given
		script := jsUpdater.WriteJSUpgradeCommands(jsUpdater.UpgradeParams{})

		// when
		usesGuard := strings.Contains(script,
			"autoupdate_version_is_newer \"$NODE_VERSION\" \"$CURRENT_NODE_VERSION\"")

		// then
		assert.True(t, usesGuard)
		assert.NotContains(t, script, "\"$CURRENT_NODE_VERSION\" != \"$NODE_VERSION\"")
	})
}
