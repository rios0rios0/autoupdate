//go:build unit

package python_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	pyUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/python"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

// The tests in this file guard one property: an update run must upgrade a
// repository with the dependency manager that repository already uses, and must
// never leave behind the manifests of a different one. A pip/requirements.txt
// project that came back from a run carrying a pyproject.toml would have been
// migrated to another package manager by an automated dependency bump nobody
// reviewed as such.

func TestNewPythonProject(t *testing.T) {
	t.Parallel()

	t.Run("should keep pip when the repository only has a requirements.txt", func(t *testing.T) {
		t.Parallel()

		// given / when
		project := pyUpdater.NewPythonProject(true, false, false)

		// then
		assert.Equal(t, "pip", project.Toolchain())
		assert.False(t, project.UsesPDM())
	})

	t.Run("should keep pip when PDM markers exist but there is no pyproject.toml", func(t *testing.T) {
		t.Parallel()

		// given / when
		project := pyUpdater.NewPythonProject(true, false, true)

		// then
		assert.Equal(t, "pip", project.Toolchain())
		assert.False(t, project.UsesPDM())
	})

	t.Run("should select PDM only when a pyproject.toml backs the markers", func(t *testing.T) {
		t.Parallel()

		// given / when
		project := pyUpdater.NewPythonProject(false, true, true)

		// then
		assert.Equal(t, "pdm", project.Toolchain())
		assert.True(t, project.UsesPDM())
	})

	t.Run("should keep pip when a pyproject.toml carries no PDM markers", func(t *testing.T) {
		t.Parallel()

		// given / when
		project := pyUpdater.NewPythonProject(true, true, false)

		// then
		assert.Equal(t, "pip", project.Toolchain())
		assert.False(t, project.UsesPDM())
	})
}

func TestDetectLocalProject(t *testing.T) {
	t.Parallel()

	t.Run("should report a pip project when only a requirements.txt is present", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeFile(t, repoDir, "requirements.txt", "requests==2.31.0\n")

		// when
		project := pyUpdater.DetectLocalProject(repoDir)

		// then
		assert.Equal(t, "pip", project.Toolchain())
		assert.True(t, project.HasRequirements)
		assert.False(t, project.HasPyproject)
	})

	t.Run("should stay on pip when a stray pdm.lock has no pyproject.toml beside it", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeFile(t, repoDir, "requirements.txt", "requests==2.31.0\n")
		writeFile(t, repoDir, "pdm.lock", "[metadata]\n")

		// when
		project := pyUpdater.DetectLocalProject(repoDir)

		// then
		assert.Equal(t, "pip", project.Toolchain())
	})

	t.Run("should report a PDM project when the pyproject declares the tool.pdm table", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeFile(t, repoDir, "pyproject.toml", "[project]\nname = \"demo\"\n[tool.pdm]\n")

		// when
		project := pyUpdater.DetectLocalProject(repoDir)

		// then
		assert.Equal(t, "pdm", project.Toolchain())
	})

	t.Run("should report a pip project when the pyproject has no PDM markers", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeFile(t, repoDir, "requirements.txt", "requests==2.31.0\n")
		writeFile(t, repoDir, "pyproject.toml", "[project]\nname = \"demo\"\n")

		// when
		project := pyUpdater.DetectLocalProject(repoDir)

		// then
		assert.Equal(t, "pip", project.Toolchain())
	})
}

func TestDetectRemoteProject(t *testing.T) {
	t.Parallel()

	repo := entities.Repository{Organization: "org", Name: "repo"}

	t.Run("should report a pip project when the remote only has a requirements.txt", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"requirements.txt": true}).
			BuildSpy()

		// when
		project := pyUpdater.DetectRemoteProject(t.Context(), provider, repo)

		// then
		assert.Equal(t, "pip", project.Toolchain())
		assert.True(t, project.HasRequirements)
		assert.False(t, project.HasPyproject)
	})

	t.Run("should stay on pip when the remote pdm.lock has no pyproject.toml beside it", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"requirements.txt": true, "pdm.lock": true}).
			BuildSpy()

		// when
		project := pyUpdater.DetectRemoteProject(t.Context(), provider, repo)

		// then
		assert.Equal(t, "pip", project.Toolchain())
	})

	t.Run("should report a PDM project when the remote pyproject declares PDM", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"pyproject.toml": true}).
			WithFileContents(map[string]string{"pyproject.toml": "[tool.pdm]\n"}).
			BuildSpy()

		// when
		project := pyUpdater.DetectRemoteProject(t.Context(), provider, repo)

		// then
		assert.Equal(t, "pdm", project.Toolchain())
	})
}

func TestPipProjectKeepsItsPackageManager(t *testing.T) {
	t.Parallel()

	pipProject := pyUpdater.NewPythonProject(true, false, false)

	t.Run("should never invoke PDM in the batch script of a requirements-only project", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := pyUpdater.BuildBatchPythonScript(true, false, false)

		// then
		assert.Contains(t, script, "pip install --upgrade -r requirements.txt")
		assertNoPDMCommands(t, script)
	})

	t.Run("should never invoke PDM when a pdm.lock exists without a pyproject.toml", func(t *testing.T) {
		t.Parallel()

		// given / when — PDM markers are present, the pyproject.toml is not
		script := pyUpdater.BuildBatchPythonScript(true, false, true)

		// then
		assertNoPDMCommands(t, script)
	})

	t.Run("should never invoke PDM in the clone-based script of a pip project", func(t *testing.T) {
		t.Parallel()

		// given
		params := pyUpdater.UpgradeParamsExported{ProviderName: "github", Project: pipProject}

		// when
		script := pyUpdater.BuildUpgradeScript(params, "/tmp/repo")

		// then
		assertNoPDMCommands(t, script)
	})

	t.Run("should never invoke PDM in the local script of a pip project", func(t *testing.T) {
		t.Parallel()

		// given
		params := pyUpdater.LocalUpgradeParamsExported{
			BranchName:   "chore/upgrade-python-deps",
			Project:      pipProject,
			PythonBinary: "/usr/bin/python3",
		}

		// when
		script := pyUpdater.BuildLocalUpgradeScript(params)

		// then
		assertNoPDMCommands(t, script)
	})

	t.Run("should guard every pip script against a manifest the upgrade invents", func(t *testing.T) {
		t.Parallel()

		// given / when
		scripts := map[string]string{
			"batch": pyUpdater.BuildBatchPythonScript(true, false, false),
			"clone": pyUpdater.BuildUpgradeScript(
				pyUpdater.UpgradeParamsExported{ProviderName: "github", Project: pipProject},
				"/tmp/repo",
			),
			"local": pyUpdater.BuildLocalUpgradeScript(
				pyUpdater.LocalUpgradeParamsExported{Project: pipProject, PythonBinary: "/usr/bin/python3"},
			),
		}

		// then
		for mode, script := range scripts {
			assert.Contains(t, script, "PYPROJECT_EXISTED=false", mode)
			assert.Contains(t, script, "PDM_LOCK_EXISTED=false", mode)
			assert.Contains(t, script, "rm -f \"pyproject.toml\"", mode)
			assert.Contains(t, script, "rm -f \"pdm.lock\"", mode)
		}
	})

	t.Run("should not guard a PDM project, whose manifests are its own", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := pyUpdater.BuildBatchPythonScript(false, true, true)

		// then
		assert.Contains(t, script, "pdm update --update-all --no-sync")
		assert.NotContains(t, script, "rm -f \"pyproject.toml\"")
	})

	t.Run("should report pip for a dry run over a repository carrying a stray pdm.lock", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeFile(t, repoDir, "requirements.txt", "requests==2.31.0\n")
		writeFile(t, repoDir, "pdm.lock", "[metadata]\n")
		vCtx := &pyUpdater.VersionContext{
			LatestVersion: "3.13.1",
			BranchName:    "chore/upgrade-python-deps",
		}

		// when
		result := pyUpdater.HandleDryRun(vCtx, repoDir)

		// then
		assert.Equal(t, "pip", result.Toolchain)
	})
}

// TestManifestGuardBehaviour runs the generated guard through bash so the
// property is proven by execution, not only by the shape of the script.
func TestManifestGuardBehaviour(t *testing.T) {
	t.Parallel()

	t.Run("should delete a pyproject.toml that appeared during the upgrade", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeFile(t, repoDir, "requirements.txt", "requests==2.31.0\n")

		// when — the fake upgrade creates the manifest the guard must undo
		output := runGuardScript(t, repoDir, "touch pyproject.toml\ntouch pdm.lock\n")

		// then
		assert.NoFileExists(t, filepath.Join(repoDir, "pyproject.toml"))
		assert.NoFileExists(t, filepath.Join(repoDir, "pdm.lock"))
		assert.Contains(t, output, "WARNING: the upgrade created pyproject.toml")
		assert.Contains(t, output, "WARNING: the upgrade created pdm.lock")
	})

	t.Run("should leave a pyproject.toml the repository already owned", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeFile(t, repoDir, "pyproject.toml", "[project]\nname = \"demo\"\n")

		// when
		output := runGuardScript(t, repoDir, "echo 'dependencies = []' >> pyproject.toml\n")

		// then
		assert.FileExists(t, filepath.Join(repoDir, "pyproject.toml"))
		assert.NotContains(t, output, "WARNING")
	})

	t.Run("should leave a repository that gained no manifest untouched", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeFile(t, repoDir, "requirements.txt", "requests==2.31.0\n")

		// when
		output := runGuardScript(t, repoDir, "echo 'urllib3==2.2.1' >> requirements.txt\n")

		// then
		assert.NoFileExists(t, filepath.Join(repoDir, "pyproject.toml"))
		assert.NotContains(t, output, "WARNING")
	})
}

// --- helpers ---

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

// assertNoPDMCommands fails when the script would run PDM in any form. The
// installation step is checked too: it is what puts PDM on the PATH.
func assertNoPDMCommands(t *testing.T, script string) {
	t.Helper()
	assert.NotContains(t, script, "pdm update")
	assert.NotContains(t, script, "pip install --upgrade pdm")
	assert.NotContains(t, script, "command -v pdm")
}

// runGuardScript executes the snapshot/restore pair the pip path emits, with
// `upgrade` standing in for whatever the real upgrade commands did to the
// working tree in between.
func runGuardScript(t *testing.T, repoDir, upgrade string) string {
	t.Helper()

	var sb strings.Builder
	sb.WriteString("#!/bin/bash\nset -euo pipefail\n\n")
	pyUpdater.WriteManifestSnapshot(&sb)
	sb.WriteString(upgrade)
	pyUpdater.WriteManifestRestore(&sb)

	scriptPath := filepath.Join(t.TempDir(), "guard.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte(sb.String()), 0o700)) //nolint:gosec // test script must be executable

	cmd := exec.CommandContext(t.Context(), "bash", scriptPath)
	cmd.Dir = repoDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "guard script failed:\n%s", output)

	return string(output)
}
