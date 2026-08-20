//go:build unit

package golang_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	goUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

// writeModule creates a go.mod with the given directive under repoDir/relDir.
func writeModule(t *testing.T, repoDir, relDir, goVersion string) {
	t.Helper()

	dir := filepath.Join(repoDir, filepath.FromSlash(relDir))
	// A directory needs the owner execute (search) bit, so 0o700 (not the rule's
	// 0o600 file threshold) is the least-privilege mode; owner-only is sufficient.
	// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/"+strings.ReplaceAll(relDir, "/", "-")+"\n\ngo "+goVersion+"\n"),
		0o600,
	))
}

func TestModuleDirsFromPaths(t *testing.T) {
	t.Parallel()

	t.Run("should return the root module for a repository with only a root go.mod", func(t *testing.T) {
		t.Parallel()

		// given
		paths := []string{"go.mod"}

		// when
		dirs := goUpdater.ModuleDirsFromPaths(paths)

		// then
		assert.Equal(t, []string{"."}, dirs)
	})

	t.Run("should return the nested directory when the module is not at the root", func(t *testing.T) {
		t.Parallel()

		// given
		paths := []string{"tests/harness/go.mod"}

		// when
		dirs := goUpdater.ModuleDirsFromPaths(paths)

		// then
		assert.Equal(t, []string{"tests/harness"}, dirs)
	})

	t.Run("should order the root module first and the nested ones lexicographically", func(t *testing.T) {
		t.Parallel()

		// given
		paths := []string{"tools/go.mod", "go.mod", "examples/basic/go.mod"}

		// when
		dirs := goUpdater.ModuleDirsFromPaths(paths)

		// then
		assert.Equal(t, []string{".", "examples/basic", "tools"}, dirs)
	})

	t.Run("should keep the root module first even when a directory sorts before it", func(t *testing.T) {
		t.Parallel()

		// given a directory name that sorts before "." in byte order
		paths := []string{"go.mod", "-tools/go.mod"}

		// when
		dirs := goUpdater.ModuleDirsFromPaths(paths)

		// then
		assert.Equal(t, []string{".", "-tools"}, dirs)
	})

	t.Run("should strip the leading slash of absolute provider item paths", func(t *testing.T) {
		t.Parallel()

		// given paths in the absolute form Azure DevOps returns
		paths := []string{"/go.mod", "/tests/harness/go.mod", "/vendor/dep/go.mod"}

		// when
		dirs := goUpdater.ModuleDirsFromPaths(paths)

		// then
		assert.Equal(t, []string{".", "tests/harness"}, dirs)
	})

	t.Run("should skip vendored, fixture and hidden modules", func(t *testing.T) {
		t.Parallel()

		// given
		paths := []string{
			"go.mod",
			"vendor/example.com/dep/go.mod",
			"internal/testdata/broken/go.mod",
			"web/node_modules/pkg/go.mod",
			".cache/go.mod",
		}

		// when
		dirs := goUpdater.ModuleDirsFromPaths(paths)

		// then
		assert.Equal(t, []string{"."}, dirs)
	})

	t.Run("should ignore files that are not go.mod and de-duplicate repeats", func(t *testing.T) {
		t.Parallel()

		// given
		paths := []string{"go.sum", "tools/go.mod", "./tools/go.mod", "README.md"}

		// when
		dirs := goUpdater.ModuleDirsFromPaths(paths)

		// then
		assert.Equal(t, []string{"tools"}, dirs)
	})

	t.Run("should return no directories when the repository has no module", func(t *testing.T) {
		t.Parallel()

		// given
		paths := []string{"main.tf", "README.md"}

		// when
		dirs := goUpdater.ModuleDirsFromPaths(paths)

		// then
		assert.Empty(t, dirs)
	})
}

func TestDiscoverLocalModuleDirs(t *testing.T) {
	t.Parallel()

	t.Run("should find the root module and every nested module", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeModule(t, repoDir, ".", "1.24.0")
		writeModule(t, repoDir, "tests/harness", "1.23.1")

		// when
		dirs := goUpdater.DiscoverLocalModuleDirs(repoDir)

		// then
		assert.Equal(t, []string{".", "tests/harness"}, dirs)
	})

	t.Run("should find a nested module when the repository has no root module", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeModule(t, repoDir, "tests/harness", "1.23.1")

		// when
		dirs := goUpdater.DiscoverLocalModuleDirs(repoDir)

		// then
		assert.Equal(t, []string{"tests/harness"}, dirs)
	})

	t.Run("should skip vendored modules", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeModule(t, repoDir, ".", "1.24.0")
		writeModule(t, repoDir, "vendor/example.com/dep", "1.19")

		// when
		dirs := goUpdater.DiscoverLocalModuleDirs(repoDir)

		// then
		assert.Equal(t, []string{"."}, dirs)
	})

	t.Run("should skip modules inside hidden directories", func(t *testing.T) {
		t.Parallel()

		// given
		// The shared walker now descends into the repository's own hidden
		// directories so the pipeline updater can reach `.github/workflows/`.
		// Module discovery must not follow: `moduleDirsFromPaths` keeps dropping
		// hidden segments, because the generated upgrade script discovers modules
		// with `find ... -not -path '*/.*/*'` and the two sets must not diverge.
		repoDir := t.TempDir()
		writeModule(t, repoDir, ".", "1.24.0")
		writeModule(t, repoDir, ".github/tools", "1.19")

		// when
		dirs := goUpdater.DiscoverLocalModuleDirs(repoDir)

		// then
		assert.Equal(t, []string{"."}, dirs)
	})

	t.Run("should return no directories when the repository holds no Go module", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "main.tf"), []byte("# tf\n"), 0o600))

		// when
		dirs := goUpdater.DiscoverLocalModuleDirs(repoDir)

		// then
		assert.Empty(t, dirs)
	})
}

func TestDiscoverRemoteModuleDirs(t *testing.T) {
	t.Parallel()

	t.Run("should return module directories from the provider listing", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{
				{Path: "tests/harness/go.mod"},
				{Path: "go.mod"},
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		dirs := goUpdater.DiscoverRemoteModuleDirs(t.Context(), provider, repo)

		// then
		assert.Equal(t, []string{".", "tests/harness"}, dirs)
	})

	t.Run("should ignore directory entries returned by the provider", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{
				{Path: "tools", IsDir: true},
				{Path: "tools/go.mod"},
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		dirs := goUpdater.DiscoverRemoteModuleDirs(t.Context(), provider, repo)

		// then
		assert.Equal(t, []string{"tools"}, dirs)
	})

	t.Run("should return no directories when listing fails", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithListFileErr(errors.New("listing failed")).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		dirs := goUpdater.DiscoverRemoteModuleDirs(t.Context(), provider, repo)

		// then
		assert.Empty(t, dirs)
	})
}

func TestGoModPathFor(t *testing.T) {
	t.Parallel()

	t.Run("should return the bare manifest name for the root module", func(t *testing.T) {
		t.Parallel()

		// given
		dir := "."

		// when
		path := goUpdater.GoModPathFor(dir)

		// then
		assert.Equal(t, "go.mod", path)
	})

	t.Run("should join the manifest name onto a nested module directory", func(t *testing.T) {
		t.Parallel()

		// given
		dir := "tests/harness"

		// when
		path := goUpdater.GoModPathFor(dir)

		// then
		assert.Equal(t, "tests/harness/go.mod", path)
	})
}

// runUpgradeScript renders the Go upgrade commands and executes them against
// repoDir with a stubbed `go` binary, so the loop over modules is exercised
// for real without touching the network or the Go toolchain.
func runUpgradeScript(t *testing.T, repoDir, goVersion string) string {
	t.Helper()

	bashPath, lookErr := exec.LookPath("bash")
	if lookErr != nil {
		t.Skip("bash is not available on this platform")
	}

	var sb strings.Builder
	sb.WriteString("#!/bin/bash\nset -euo pipefail\n\n")
	goUpdater.WriteGoUpgradeCommands(&sb)
	sb.WriteString("echo \"AGGREGATED_CHANGE=$GO_VERSION_CHANGED\"\n")

	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "upgrade.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte(sb.String()), 0o700)) //nolint:gosec // test script must execute

	// A stub stands in for the real toolchain: the loop's job is to run the
	// commands in each module directory, not to resolve dependencies.
	stubPath := filepath.Join(scriptDir, "go-stub")
	require.NoError(t, os.WriteFile(stubPath, []byte("#!/bin/bash\necho \"stub $* in $(pwd)\"\n"), 0o700)) //nolint:gosec // test stub must execute

	cmd := exec.CommandContext(t.Context(), bashPath, scriptPath)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "GO_VERSION="+goVersion, "GO_BINARY="+stubPath)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "upgrade script failed:\n%s", output)

	return string(output)
}

// goDirectiveOf reads the go directive of the module at repoDir/relDir.
func goDirectiveOf(t *testing.T, repoDir, relDir string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(relDir), "go.mod"))
	require.NoError(t, err)

	return goUpdater.ParseGoDirective(string(data))
}

func TestWriteGoUpgradeCommands(t *testing.T) {
	t.Parallel()

	t.Run("should upgrade the root module and every nested module", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeModule(t, repoDir, ".", "1.24.0")
		writeModule(t, repoDir, "tests/harness", "1.23.1")

		// when
		output := runUpgradeScript(t, repoDir, "1.25.7")

		// then
		assert.Equal(t, "1.25.7", goDirectiveOf(t, repoDir, "."))
		assert.Equal(t, "1.25.7", goDirectiveOf(t, repoDir, "tests/harness"))
		assert.Contains(t, output, "GO_VERSION_UPDATED=true")
		assert.Contains(t, output, "AGGREGATED_CHANGE=true")
	})

	t.Run("should upgrade a nested module when the repository has no root module", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeModule(t, repoDir, "tests/harness", "1.23.1")

		// when
		output := runUpgradeScript(t, repoDir, "1.25.7")

		// then
		assert.Equal(t, "1.25.7", goDirectiveOf(t, repoDir, "tests/harness"))
		assert.Contains(t, output, "AGGREGATED_CHANGE=true")
	})

	t.Run("should run the toolchain inside each module directory", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeModule(t, repoDir, ".", "1.25.7")
		writeModule(t, repoDir, "tools", "1.25.7")

		// when
		output := runUpgradeScript(t, repoDir, "1.25.7")

		// then
		assert.Contains(t, output, "stub mod tidy in "+filepath.Join(repoDir, "tools"))
		assert.Contains(t, output, "AGGREGATED_CHANGE=false",
			"no directive changes when every module already matches the target")
	})

	t.Run("should leave vendored modules untouched", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeModule(t, repoDir, ".", "1.24.0")
		writeModule(t, repoDir, "vendor/example.com/dep", "1.19")

		// when
		runUpgradeScript(t, repoDir, "1.25.7")

		// then
		assert.Equal(t, "1.25.7", goDirectiveOf(t, repoDir, "."))
		assert.Equal(t, "1.19", goDirectiveOf(t, repoDir, "vendor/example.com/dep"))
	})

	t.Run("should keep upgrading other modules when one declares no go directive", func(t *testing.T) {
		t.Parallel()

		// given a module whose go.mod has no go directive, ordered before a stale one
		repoDir := t.TempDir()
		// A directory needs the owner execute (search) bit, so 0o700 (not the rule's
		// 0o600 file threshold) is the least-privilege mode; owner-only is sufficient.
		// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
		require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "broken"), 0o700))
		require.NoError(t, os.WriteFile(
			filepath.Join(repoDir, "broken", "go.mod"), []byte("module example.com/broken\n"), 0o600))
		writeModule(t, repoDir, "tools", "1.23.1")

		// when
		output := runUpgradeScript(t, repoDir, "1.25.7")

		// then
		assert.Contains(t, output, "no go directive found in go.mod")
		assert.Equal(t, "1.25.7", goDirectiveOf(t, repoDir, "tools"),
			"a module without a directive must not abort the modules after it")
	})

	t.Run("should upgrade a module directory whose name starts with a dash", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeModule(t, repoDir, "-tools", "1.23.1")

		// when
		runUpgradeScript(t, repoDir, "1.25.7")

		// then
		assert.Equal(t, "1.25.7", goDirectiveOf(t, repoDir, "-tools"))
	})

	t.Run("should exit cleanly when the repository holds no Go module", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "main.tf"), []byte("# tf\n"), 0o600))

		// when
		output := runUpgradeScript(t, repoDir, "1.25.7")

		// then
		assert.Contains(t, output, "no go.mod found in the repository")
		assert.Contains(t, output, "GO_VERSION_UPDATED=false")
	})
}

// moduleReader returns a reader over a directory-to-go-directive map.
func moduleReader(modules map[string]string) func(string) (string, error) {
	return func(dir string) (string, error) {
		version, ok := modules[dir]
		if !ok {
			return "", errors.New("no such module")
		}
		return "module example.com/m\n\ngo " + version + "\n", nil
	}
}

func TestWriteCommitAndPush(t *testing.T) {
	t.Parallel()

	t.Run("should keep the Go version in the commit subject", func(t *testing.T) {
		t.Parallel()

		// given the version-bump commit subject wraps the version in backticks,
		// which bash treats as command substitution unless they are escaped
		var sb strings.Builder
		goUpdater.WriteCommitAndPush(&sb)

		// when
		script := sb.String()

		// then
		assert.Contains(t, script, "\\`$GO_VERSION\\`")
		assert.NotContains(t, script, "to `$GO_VERSION`")
	})
}

func TestResolveVersionUpgradeNeed(t *testing.T) {
	t.Parallel()

	t.Run("should short-circuit on a stale root without scanning for modules", func(t *testing.T) {
		t.Parallel()

		// given
		scanned := false
		read := moduleReader(map[string]string{".": "1.24.0"})
		dirs := func() []string {
			scanned = true
			return nil
		}

		// when
		needed, sourceDir, found := goUpdater.ResolveVersionUpgradeNeed(read, dirs, "1.25.7")

		// then
		require.True(t, found)
		assert.True(t, needed)
		assert.Equal(t, ".", sourceDir)
		assert.False(t, scanned, "a stale root already settles the decision")
	})

	t.Run("should require an upgrade when only a nested module is behind", func(t *testing.T) {
		t.Parallel()

		// given the root is already current but a nested module is not
		read := moduleReader(map[string]string{".": "1.25.7", "tools": "1.23.1"})
		dirs := func() []string { return []string{".", "tools"} }

		// when
		needed, sourceDir, found := goUpdater.ResolveVersionUpgradeNeed(read, dirs, "1.25.7")

		// then
		require.True(t, found)
		assert.True(t, needed, "the script bumps every module, so the decision must span them all")
		assert.Equal(t, "tools", sourceDir)
	})

	t.Run("should not require an upgrade when every module is current", func(t *testing.T) {
		t.Parallel()

		// given
		read := moduleReader(map[string]string{".": "1.25.7", "tools": "1.25.7"})
		dirs := func() []string { return []string{".", "tools"} }

		// when
		needed, _, found := goUpdater.ResolveVersionUpgradeNeed(read, dirs, "1.25.7")

		// then
		require.True(t, found)
		assert.False(t, needed)
	})

	t.Run("should decide from a nested module when the root has no go.mod", func(t *testing.T) {
		t.Parallel()

		// given
		read := moduleReader(map[string]string{"tests/harness": "1.23.1"})
		dirs := func() []string { return []string{"tests/harness"} }

		// when
		needed, sourceDir, found := goUpdater.ResolveVersionUpgradeNeed(read, dirs, "1.25.7")

		// then
		require.True(t, found)
		assert.True(t, needed)
		assert.Equal(t, "tests/harness", sourceDir)
	})

	t.Run("should report not found when no module can be read", func(t *testing.T) {
		t.Parallel()

		// given
		read := moduleReader(map[string]string{})
		dirs := func() []string { return []string{"tests/harness"} }

		// when
		_, _, found := goUpdater.ResolveVersionUpgradeNeed(read, dirs, "1.25.7")

		// then
		assert.False(t, found)
	})
}
