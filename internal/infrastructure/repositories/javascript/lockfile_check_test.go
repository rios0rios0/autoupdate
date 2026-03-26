//go:build unit

package javascript_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/javascript"
)

// initGitRepo creates a bare-minimum git repo in dir with an initial commit
// containing the given files (path -> content).
func initGitRepo(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v failed: %s", args, out)
	}

	run("init")
	run("config", "user.name", "test")
	run("config", "user.email", "test@test.com")
	run("config", "commit.gpgsign", "false")

	for path, content := range files {
		full := filepath.Join(dir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
	}

	run("add", "-A")
	run("commit", "-m", "initial commit")
}

// packageLockWithVersion returns a minimal package-lock.json with the given
// root version and a single dependency.
func packageLockWithVersion(version, depVersion string) string {
	return `{
  "name": "test-project",
  "version": "` + version + `",
  "lockfileVersion": 3,
  "requires": true,
  "packages": {
    "": {
      "name": "test-project",
      "version": "` + version + `",
      "dependencies": {
        "lodash": "^4.17.21"
      }
    },
    "node_modules/lodash": {
      "version": "` + depVersion + `",
      "resolved": "https://registry.npmjs.org/lodash/-/lodash-` + depVersion + `.tgz",
      "integrity": "sha512-abc123"
    }
  }
}
`
}

func TestHasOnlyLockfileVersionChanges(t *testing.T) {
	t.Parallel()

	t.Run("should return true when only package-lock.json version fields changed", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()
		initGitRepo(t, dir, map[string]string{
			"package.json":      `{"name":"test-project","version":"1.0.3"}`,
			"package-lock.json": packageLockWithVersion("1.0.3", "4.17.21"),
		})
		// Simulate: autobump changed package.json to 1.0.4, then npm update
		// synced package-lock.json version — but no dependency changed.
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "package-lock.json"),
			[]byte(packageLockWithVersion("1.0.4", "4.17.21")),
			0o600,
		))

		// when
		result := javascript.HasOnlyLockfileVersionChanges(t.Context(), dir)

		// then
		assert.True(t, result)
	})

	t.Run("should return false when a dependency version also changed", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()
		initGitRepo(t, dir, map[string]string{
			"package.json":      `{"name":"test-project","version":"1.0.3"}`,
			"package-lock.json": packageLockWithVersion("1.0.3", "4.17.21"),
		})
		// Both the project version AND a dependency changed.
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "package-lock.json"),
			[]byte(packageLockWithVersion("1.0.4", "4.17.22")),
			0o600,
		))

		// when
		result := javascript.HasOnlyLockfileVersionChanges(t.Context(), dir)

		// then
		assert.False(t, result)
	})

	t.Run("should return true when only package-lock.json version fields and CHANGELOG.md changed", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()
		initGitRepo(t, dir, map[string]string{
			"package.json":      `{"name":"test-project","version":"1.0.3"}`,
			"package-lock.json": packageLockWithVersion("1.0.3", "4.17.21"),
			"CHANGELOG.md":      "# Changelog\n",
		})
		// Simulate: cosmetic lockfile version sync + auto-generated changelog update.
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "package-lock.json"),
			[]byte(packageLockWithVersion("1.0.4", "4.17.21")),
			0o600,
		))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "CHANGELOG.md"),
			[]byte("# Changelog\n\n## [Unreleased]\n"),
			0o600,
		))

		// when
		result := javascript.HasOnlyLockfileVersionChanges(t.Context(), dir)

		// then
		assert.True(t, result)
	})

	t.Run("should return false when non-lockfile files also changed", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()
		initGitRepo(t, dir, map[string]string{
			"package.json":      `{"name":"test-project","version":"1.0.3"}`,
			"package-lock.json": packageLockWithVersion("1.0.3", "4.17.21"),
			".nvmrc":            "20.0.0",
		})
		// Only the lockfile version changed, but .nvmrc was also modified.
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "package-lock.json"),
			[]byte(packageLockWithVersion("1.0.4", "4.17.21")),
			0o600,
		))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, ".nvmrc"),
			[]byte("22.0.0"),
			0o600,
		))

		// when
		result := javascript.HasOnlyLockfileVersionChanges(t.Context(), dir)

		// then
		assert.False(t, result)
	})

	t.Run("should return false when there are no changes at all", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()
		initGitRepo(t, dir, map[string]string{
			"package.json":      `{"name":"test-project","version":"1.0.3"}`,
			"package-lock.json": packageLockWithVersion("1.0.3", "4.17.21"),
		})

		// when
		result := javascript.HasOnlyLockfileVersionChanges(t.Context(), dir)

		// then
		assert.False(t, result)
	})
}

func TestIsPackageLockOnlyVersionSync(t *testing.T) {
	t.Parallel()

	t.Run("should return true when only root and packages root version differ", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()
		initGitRepo(t, dir, map[string]string{
			"package-lock.json": packageLockWithVersion("1.0.0", "4.17.21"),
		})
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "package-lock.json"),
			[]byte(packageLockWithVersion("1.0.1", "4.17.21")),
			0o600,
		))

		// when
		result := javascript.IsPackageLockOnlyVersionSync(t.Context(), dir)

		// then
		assert.True(t, result)
	})

	t.Run("should return false when dependency integrity hash changed", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()
		original := packageLockWithVersion("1.0.0", "4.17.21")
		initGitRepo(t, dir, map[string]string{
			"package-lock.json": original,
		})
		// Change the integrity hash (simulates a real dependency update).
		modified := `{
  "name": "test-project",
  "version": "1.0.1",
  "lockfileVersion": 3,
  "requires": true,
  "packages": {
    "": {
      "name": "test-project",
      "version": "1.0.1",
      "dependencies": {
        "lodash": "^4.17.21"
      }
    },
    "node_modules/lodash": {
      "version": "4.17.21",
      "resolved": "https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz",
      "integrity": "sha512-DIFFERENT"
    }
  }
}
`
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "package-lock.json"),
			[]byte(modified),
			0o600,
		))

		// when
		result := javascript.IsPackageLockOnlyVersionSync(t.Context(), dir)

		// then
		assert.False(t, result)
	})
}

func TestRevertWorkingTreeChanges(t *testing.T) {
	t.Parallel()

	t.Run("should discard all unstaged modifications", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()
		original := packageLockWithVersion("1.0.0", "4.17.21")
		initGitRepo(t, dir, map[string]string{
			"package-lock.json": original,
		})
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "package-lock.json"),
			[]byte(packageLockWithVersion("1.0.1", "4.17.21")),
			0o600,
		))

		// when
		javascript.RevertWorkingTreeChanges(t.Context(), dir)

		// then
		content, err := os.ReadFile(filepath.Join(dir, "package-lock.json"))
		require.NoError(t, err)
		assert.Equal(t, original, string(content))
	})
}
