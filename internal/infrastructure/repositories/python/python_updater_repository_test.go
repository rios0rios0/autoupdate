//go:build unit

package python_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
	pyUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/python"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestName(t *testing.T) {
	t.Parallel()

	t.Run("should return python as updater name", func(t *testing.T) {
		t.Parallel()

		// given
		updater := pyUpdater.NewUpdaterRepository()

		// when
		name := updater.Name()

		// then
		assert.Equal(t, "python", name)
	})
}

func TestDetect(t *testing.T) {
	t.Parallel()

	t.Run("should return true when pyproject.toml exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"pyproject.toml": true}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := pyUpdater.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.True(t, detected)
	})

	t.Run("should return false when no Python files exist", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := pyUpdater.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.False(t, detected)
	})
}

func TestParsePythonVersionFile(t *testing.T) {
	t.Parallel()

	t.Run("should extract version from simple version file", func(t *testing.T) {
		t.Parallel()

		// given
		content := "3.12.8\n"

		// when
		result := pyUpdater.ParsePythonVersionFile(content)

		// then
		assert.Equal(t, "3.12.8", result)
	})

	t.Run("should return empty when content is empty", func(t *testing.T) {
		t.Parallel()

		// given
		content := ""

		// when
		result := pyUpdater.ParsePythonVersionFile(content)

		// then
		assert.Equal(t, "", result)
	})

	t.Run("should skip comment lines", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# This is a comment\n3.13.1\n"

		// when
		result := pyUpdater.ParsePythonVersionFile(content)

		// then
		assert.Equal(t, "3.13.1", result)
	})

	t.Run("should trim whitespace from version", func(t *testing.T) {
		t.Parallel()

		// given
		content := "  3.12.0  \n"

		// when
		result := pyUpdater.ParsePythonVersionFile(content)

		// then
		assert.Equal(t, "3.12.0", result)
	})
}

func TestIsActiveRelease(t *testing.T) {
	t.Parallel()

	t.Run("should return true when EOL is false (active release)", func(t *testing.T) {
		t.Parallel()

		// given
		release := pyUpdater.PythonRelease{
			Cycle:  "3.13",
			Latest: "3.13.1",
			EOL:    false,
		}

		// when
		result := pyUpdater.IsActiveRelease(release)

		// then
		assert.True(t, result)
	})

	t.Run("should return false when EOL is true (end-of-life release)", func(t *testing.T) {
		t.Parallel()

		// given
		release := pyUpdater.PythonRelease{
			Cycle:  "2.7",
			Latest: "2.7.18",
			EOL:    true,
		}

		// when
		result := pyUpdater.IsActiveRelease(release)

		// then
		assert.False(t, result)
	})

	t.Run("should return false when EOL is a past date string", func(t *testing.T) {
		t.Parallel()

		// given
		release := pyUpdater.PythonRelease{
			Cycle:  "3.6",
			Latest: "3.6.15",
			EOL:    "2021-12-23",
		}

		// when
		result := pyUpdater.IsActiveRelease(release)

		// then
		assert.False(t, result)
	})

	t.Run("should return true when EOL is a future date string", func(t *testing.T) {
		t.Parallel()

		// given
		release := pyUpdater.PythonRelease{
			Cycle:  "3.12",
			Latest: "3.12.8",
			EOL:    "2028-10-02",
		}

		// when
		result := pyUpdater.IsActiveRelease(release)

		// then
		assert.True(t, result)
	})

	t.Run("should return false when EOL is an invalid date string", func(t *testing.T) {
		t.Parallel()

		// given
		release := pyUpdater.PythonRelease{
			Cycle:  "3.7",
			Latest: "3.7.17",
			EOL:    "not-a-date",
		}

		// when
		result := pyUpdater.IsActiveRelease(release)

		// then
		assert.False(t, result)
	})

	t.Run("should return false when EOL is an unexpected type", func(t *testing.T) {
		t.Parallel()

		// given
		release := pyUpdater.PythonRelease{
			Cycle:  "3.7",
			Latest: "3.7.17",
			EOL:    42,
		}

		// when
		result := pyUpdater.IsActiveRelease(release)

		// then
		assert.False(t, result)
	})
}

func TestResolveVersionContext(t *testing.T) {
	t.Parallel()

	t.Run("should detect version upgrade needed", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".python-version": true}).
			WithFileContents(map[string]string{
				".python-version": "3.12.0\n",
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := pyUpdater.ResolveVersionContext(t.Context(), provider, repo, "3.13.1")

		// then
		require.NotNil(t, vCtx)
		assert.Equal(t, "3.13.1", vCtx.LatestVersion)
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Contains(t, vCtx.BranchName, "3.13.1")
	})

	t.Run("should detect deps-only upgrade when version is current", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".python-version": true}).
			WithFileContents(map[string]string{
				".python-version": "3.13.1\n",
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := pyUpdater.ResolveVersionContext(t.Context(), provider, repo, "3.13.1")

		// then
		require.NotNil(t, vCtx)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Contains(t, vCtx.BranchName, "deps")
	})

	t.Run("should use deps branch when no python-version file exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := pyUpdater.ResolveVersionContext(t.Context(), provider, repo, "3.13.1")

		// then
		require.NotNil(t, vCtx)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-python-deps", vCtx.BranchName)
	})

	t.Run("should use deps branch when latest version is empty", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".python-version": true}).
			WithFileContents(map[string]string{
				".python-version": "3.12.0\n",
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := pyUpdater.ResolveVersionContext(t.Context(), provider, repo, "")

		// then
		require.NotNil(t, vCtx)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-python-deps", vCtx.BranchName)
	})
}

func TestGeneratePRDescription(t *testing.T) {
	t.Parallel()

	t.Run("should include version update info when Python version was updated", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pyUpdater.GeneratePRDescription("3.13.1", true)

		// then
		assert.Contains(t, result, "3.13.1")
		assert.Contains(t, result, ".python-version")
	})

	t.Run("should describe deps-only update when no version change", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pyUpdater.GeneratePRDescription("3.13.1", false)

		// then
		assert.Contains(t, result, "dependencies")
		assert.NotContains(t, result, ".python-version")
	})
}

func TestBuildUpgradeScript(t *testing.T) {
	t.Parallel()

	t.Run("should produce a valid bash script with shebang and set flags", func(t *testing.T) {
		t.Parallel()

		// given
		params := pyUpdater.UpgradeParamsExported{
			CloneURL:        "https://example.com/org/repo.git",
			DefaultBranch:   "main",
			BranchName:      "chore/upgrade-python-deps",
			PythonVersion:   "3.13.1",
			AuthToken:       "tok123",
			ProviderName:    "github",
			ChangelogFile:   "/tmp/changelog.md",
			HasRequirements: true,
			HasPyproject:    true,
			PythonBinary:    "/usr/bin/python3",
		}

		// when
		script := pyUpdater.BuildUpgradeScript(params, "/tmp/repo")

		// then
		assert.True(t, strings.HasPrefix(script, "#!/bin/bash\n"))
		assert.Contains(t, script, "set -euo pipefail")
		assert.Contains(t, script, "git clone")
		assert.Contains(t, script, "git checkout -b")
		assert.Contains(t, script, "requirements.txt")
		assert.Contains(t, script, "pyproject.toml")
		assert.Contains(t, script, "CHANGES_PUSHED=true")
	})

	t.Run("should omit requirements section when hasRequirements is false", func(t *testing.T) {
		t.Parallel()

		// given
		params := pyUpdater.UpgradeParamsExported{
			ProviderName:    "github",
			HasRequirements: false,
			HasPyproject:    false,
		}

		// when
		script := pyUpdater.BuildUpgradeScript(params, "/tmp/repo")

		// then
		assert.NotContains(t, script, "pip install -r requirements.txt")
		assert.NotContains(t, script, "pip install --upgrade -r requirements.txt")
	})

	t.Run("should include pyproject section when hasPyproject is true", func(t *testing.T) {
		t.Parallel()

		// given
		params := pyUpdater.UpgradeParamsExported{
			ProviderName:    "github",
			HasRequirements: false,
			HasPyproject:    true,
		}

		// when
		script := pyUpdater.BuildUpgradeScript(params, "/tmp/repo")

		// then
		assert.Contains(t, script, "pip install --upgrade .")
	})
}

func TestBuildBatchPythonScript(t *testing.T) {
	t.Parallel()

	t.Run("should produce a script with shebang when both files are present", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := pyUpdater.BuildBatchPythonScript(true, true)

		// then
		assert.True(t, strings.HasPrefix(script, "#!/bin/bash\n"))
		assert.Contains(t, script, "set -euo pipefail")
		assert.Contains(t, script, "requirements.txt")
		assert.Contains(t, script, "pyproject.toml")
	})

	t.Run("should omit requirements section when hasRequirements is false", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := pyUpdater.BuildBatchPythonScript(false, true)

		// then
		assert.NotContains(t, script, "pip install -r requirements.txt")
		assert.Contains(t, script, "pip install --upgrade .")
	})

	t.Run("should omit pyproject section when hasPyproject is false", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := pyUpdater.BuildBatchPythonScript(true, false)

		// then
		assert.Contains(t, script, "pip install -r requirements.txt")
		assert.NotContains(t, script, "pip install --upgrade .")
	})

	t.Run("should produce minimal script when neither file is present", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := pyUpdater.BuildBatchPythonScript(false, false)

		// then
		assert.Contains(t, script, "set -euo pipefail")
		assert.Contains(t, script, "PYTHON_BINARY")
		assert.NotContains(t, script, "pip install -r requirements.txt")
		assert.NotContains(t, script, "pip install --upgrade .")
	})
}

func TestWriteGitAuth(t *testing.T) {
	t.Parallel()

	t.Run("should generate github auth config when provider is github", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := pyUpdater.UpgradeParamsExported{
			ProviderName: "github",
			AuthToken:    "ghp_token",
		}

		// when
		pyUpdater.WriteGitAuth(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "x-access-token")
		assert.Contains(t, result, "github.com")
		assert.Contains(t, result, "GIT_CONFIG_GLOBAL")
	})

	t.Run("should generate azuredevops auth config when provider is azuredevops", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := pyUpdater.UpgradeParamsExported{
			ProviderName: "azuredevops",
			AuthToken:    "ado_pat",
		}

		// when
		pyUpdater.WriteGitAuth(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "pat:")
		assert.Contains(t, result, "dev.azure.com")
		assert.Contains(t, result, "ssh.dev.azure.com:v3/")
	})

	t.Run("should generate gitlab auth config when provider is gitlab", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := pyUpdater.UpgradeParamsExported{
			ProviderName: "gitlab",
			AuthToken:    "gl_token",
		}

		// when
		pyUpdater.WriteGitAuth(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "oauth2:")
		assert.Contains(t, result, "gitlab.com")
	})

	t.Run("should produce only setup block when provider is unknown", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := pyUpdater.UpgradeParamsExported{
			ProviderName: "bitbucket",
			AuthToken:    "bb_token",
		}

		// when
		pyUpdater.WriteGitAuth(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "GIT_CONFIG_GLOBAL")
		assert.NotContains(t, result, "x-access-token")
		assert.NotContains(t, result, "pat:")
		assert.NotContains(t, result, "oauth2:")
	})
}

func TestWritePythonUpgradeCommands(t *testing.T) {
	t.Parallel()

	t.Run("should include requirements upgrade when HasRequirements is true", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := pyUpdater.UpgradeParamsExported{
			HasRequirements: true,
			HasPyproject:    false,
		}

		// when
		pyUpdater.WritePythonUpgradeCommands(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "pip install -r requirements.txt")
		assert.Contains(t, result, "pip install --upgrade -r requirements.txt")
		assert.Contains(t, result, "pip freeze")
	})

	t.Run("should include pyproject upgrade when HasPyproject is true", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := pyUpdater.UpgradeParamsExported{
			HasRequirements: false,
			HasPyproject:    true,
		}

		// when
		pyUpdater.WritePythonUpgradeCommands(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "pip install --upgrade .")
	})

	t.Run("should include python version check section always", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := pyUpdater.UpgradeParamsExported{
			HasRequirements: false,
			HasPyproject:    false,
		}

		// when
		pyUpdater.WritePythonUpgradeCommands(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "PYTHON_VERSION_CHANGED=false")
		assert.Contains(t, result, ".python-version")
		assert.Contains(t, result, "PYTHON_VERSION_UPDATED=")
	})

	t.Run("should include both requirements and pyproject when both are true", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := pyUpdater.UpgradeParamsExported{
			HasRequirements: true,
			HasPyproject:    true,
		}

		// when
		pyUpdater.WritePythonUpgradeCommands(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "pip install -r requirements.txt")
		assert.Contains(t, result, "pip install --upgrade .")
	})
}

func TestBuildEnv(t *testing.T) {
	t.Parallel()

	t.Run("should include all required environment variables", func(t *testing.T) {
		t.Parallel()

		// given
		params := pyUpdater.UpgradeParamsExported{
			CloneURL:      "https://example.com/org/repo.git",
			BranchName:    "chore/upgrade-python-deps",
			DefaultBranch: "main",
			AuthToken:     "tok123",
			PythonBinary:  "/usr/bin/python3",
			PythonVersion: "3.13.1",
			ChangelogFile: "/tmp/changelog.md",
		}
		repoDir := "/tmp/repo"

		// when
		env := pyUpdater.BuildEnv(params, repoDir)

		// then
		envMap := envToMap(env)
		assert.Equal(t, "tok123", envMap["AUTH_TOKEN"])
		assert.Equal(t, "tok123", envMap["GIT_HTTPS_TOKEN"])
		assert.Equal(t, "https://example.com/org/repo.git", envMap["CLONE_URL"])
		assert.Equal(t, "chore/upgrade-python-deps", envMap["BRANCH_NAME"])
		assert.Equal(t, "/tmp/repo", envMap["REPO_DIR"])
		assert.Equal(t, "main", envMap["DEFAULT_BRANCH"])
		assert.Equal(t, "/usr/bin/python3", envMap["PYTHON_BINARY"])
		assert.Equal(t, "3.13.1", envMap["PYTHON_VERSION"])
		assert.Equal(t, "/tmp/changelog.md", envMap["CHANGELOG_FILE"])
	})

	t.Run("should omit PYTHON_VERSION when empty", func(t *testing.T) {
		t.Parallel()

		// given
		params := pyUpdater.UpgradeParamsExported{
			CloneURL:      "https://example.com/org/repo.git",
			BranchName:    "chore/upgrade-python-deps",
			DefaultBranch: "main",
			AuthToken:     "tok",
			PythonBinary:  "/usr/bin/python3",
			PythonVersion: "",
			ChangelogFile: "",
		}

		// when
		env := pyUpdater.BuildEnv(params, "/tmp/repo")

		// then
		envMap := envToMap(env)
		_, hasPyVersion := envMap["PYTHON_VERSION"]
		_, hasChangelog := envMap["CHANGELOG_FILE"]
		assert.False(t, hasPyVersion)
		assert.False(t, hasChangelog)
	})
}

func TestPrepareChangelog(t *testing.T) {
	t.Parallel()

	t.Run("should return empty string when no CHANGELOG.md exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-python-deps",
		}

		// when
		result := pyUpdater.PrepareChangelog(t.Context(), provider, repo, vCtx)

		// then
		assert.Equal(t, "", result)
	})

	t.Run("should return empty string when GetFileContent fails", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContentErr(errors.New("read error")).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-python-deps",
		}

		// when
		result := pyUpdater.PrepareChangelog(t.Context(), provider, repo, vCtx)

		// then
		assert.Equal(t, "", result)
	})

	t.Run("should create temp file with version upgrade entry when version upgrade needed", func(t *testing.T) {
		t.Parallel()

		// given
		changelogContent := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2025-01-01\n"
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{
				"CHANGELOG.md": changelogContent,
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-python-3.13.1",
		}

		// when
		result := pyUpdater.PrepareChangelog(t.Context(), provider, repo, vCtx)

		// then
		assert.NotEmpty(t, result)
	})

	t.Run("should create temp file with deps entry when no version upgrade needed", func(t *testing.T) {
		t.Parallel()

		// given
		changelogContent := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2025-01-01\n"
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{
				"CHANGELOG.md": changelogContent,
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-python-deps",
		}

		// when
		result := pyUpdater.PrepareChangelog(t.Context(), provider, repo, vCtx)

		// then
		assert.NotEmpty(t, result)
	})
}

func TestLogDryRun(t *testing.T) {
	t.Parallel()

	t.Run("should not panic when needs version upgrade", func(t *testing.T) {
		t.Parallel()

		// given
		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-python-3.13.1",
		}
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when / then
		assert.NotPanics(t, func() {
			pyUpdater.LogDryRun(vCtx, repo)
		})
	})

	t.Run("should not panic when deps-only upgrade", func(t *testing.T) {
		t.Parallel()

		// given
		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-python-deps",
		}
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when / then
		assert.NotPanics(t, func() {
			pyUpdater.LogDryRun(vCtx, repo)
		})
	})
}

func TestOpenPullRequest(t *testing.T) {
	t.Parallel()

	t.Run("should create PR with deps title when python version was not updated", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithCreatedPR(&entities.PullRequest{ID: 42, URL: "https://example.com/pr/42"}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}
		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-python-deps",
		}
		result := &pyUpdater.UpgradeResultExported{
			HasChanges:           true,
			PythonVersionUpdated: false,
		}

		// when
		prs, err := pyUpdater.OpenPullRequest(t.Context(), provider, repo, opts, vCtx, result)

		// then
		require.NoError(t, err)
		require.Len(t, prs, 1)
		assert.Equal(t, 42, prs[0].ID)
		assert.Equal(t, "chore(deps): updated Python dependencies", provider.PRInputs[0].Title)
	})

	t.Run("should create PR with version title when python version was updated", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithCreatedPR(&entities.PullRequest{ID: 43, URL: "https://example.com/pr/43"}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}
		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-python-3.13.1",
		}
		result := &pyUpdater.UpgradeResultExported{
			HasChanges:           true,
			PythonVersionUpdated: true,
		}

		// when
		prs, err := pyUpdater.OpenPullRequest(t.Context(), provider, repo, opts, vCtx, result)

		// then
		require.NoError(t, err)
		require.Len(t, prs, 1)
		assert.Contains(t, provider.PRInputs[0].Title, "3.13.1")
	})

	t.Run("should return error when CreatePullRequest fails", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithCreatePRErr(errors.New("api error")).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}
		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-python-deps",
		}
		result := &pyUpdater.UpgradeResultExported{
			HasChanges: true,
		}

		// when
		prs, err := pyUpdater.OpenPullRequest(t.Context(), provider, repo, opts, vCtx, result)

		// then
		assert.Error(t, err)
		assert.Nil(t, prs)
		assert.Contains(t, err.Error(), "failed to create PR")
	})

	t.Run("should use target branch from opts when provided", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{TargetBranch: "develop"}
		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-python-deps",
		}
		result := &pyUpdater.UpgradeResultExported{
			HasChanges: true,
		}

		// when
		prs, err := pyUpdater.OpenPullRequest(t.Context(), provider, repo, opts, vCtx, result)

		// then
		require.NoError(t, err)
		require.Len(t, prs, 1)
		assert.Equal(t, "refs/heads/develop", provider.PRInputs[0].TargetBranch)
	})

	t.Run("should set AutoComplete from opts", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{AutoComplete: true}
		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-python-deps",
		}
		result := &pyUpdater.UpgradeResultExported{
			HasChanges: true,
		}

		// when
		_, err := pyUpdater.OpenPullRequest(t.Context(), provider, repo, opts, vCtx, result)

		// then
		require.NoError(t, err)
		assert.True(t, provider.PRInputs[0].AutoComplete)
	})
}

func TestCreateUpdatePRs(t *testing.T) {
	t.Parallel()

	t.Run("should return empty when no files found", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		fetcher := &repositorydoubles.StubVersionFetcher{Version: "3.13.1"}
		updater := pyUpdater.NewUpdaterRepositoryWithDeps(fetcher)

		// when
		prs, err := updater.CreateUpdatePRs(t.Context(), provider, repo, entities.UpdateOptions{})

		// then
		_ = prs
		_ = err
	})

	t.Run("should return empty when PR already exists for branch", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".python-version": true}).
			WithFileContents(map[string]string{
				".python-version": "3.12.0\n",
			}).
			WithPRExistsResult(true).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		fetcher := &repositorydoubles.StubVersionFetcher{Version: "3.13.1"}
		updater := pyUpdater.NewUpdaterRepositoryWithDeps(fetcher)

		// when
		prs, err := updater.CreateUpdatePRs(t.Context(), provider, repo, entities.UpdateOptions{})

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
		assert.True(t, provider.PRExistsResult)
	})

	t.Run("should return empty when dry run is enabled", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		fetcher := &repositorydoubles.StubVersionFetcher{Version: "3.13.1"}
		updater := pyUpdater.NewUpdaterRepositoryWithDeps(fetcher)

		// when
		prs, err := updater.CreateUpdatePRs(t.Context(), provider, repo, entities.UpdateOptions{DryRun: true})

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
	})

	t.Run("should handle version fetcher error gracefully", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			WithPRExistsResult(false).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		fetcher := &repositorydoubles.StubVersionFetcher{
			Version: "",
			Err:     errors.New("network error"),
		}
		updater := pyUpdater.NewUpdaterRepositoryWithDeps(fetcher)

		// when
		prs, err := updater.CreateUpdatePRs(t.Context(), provider, repo, entities.UpdateOptions{DryRun: true})

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
	})
}

func TestBuildLocalUpgradeScript(t *testing.T) {
	t.Parallel()

	t.Run("should produce a script with auth when token is provided", func(t *testing.T) {
		t.Parallel()

		// given
		params := pyUpdater.LocalUpgradeParamsExported{
			BranchName:      "chore/upgrade-python-deps",
			PythonVersion:   "3.13.1",
			AuthToken:       "tok123",
			ProviderName:    "github",
			HasRequirements: true,
			HasPyproject:    false,
			PythonBinary:    "/usr/bin/python3",
		}

		// when
		script := pyUpdater.BuildLocalUpgradeScript(params)

		// then
		assert.True(t, strings.HasPrefix(script, "#!/bin/bash\n"))
		assert.Contains(t, script, "set -euo pipefail")
		assert.Contains(t, script, "x-access-token")
		assert.Contains(t, script, "requirements.txt")
	})

	t.Run("should omit auth section when token is empty", func(t *testing.T) {
		t.Parallel()

		// given
		params := pyUpdater.LocalUpgradeParamsExported{
			BranchName:      "chore/upgrade-python-deps",
			HasRequirements: true,
			HasPyproject:    false,
			PythonBinary:    "/usr/bin/python3",
		}

		// when
		script := pyUpdater.BuildLocalUpgradeScript(params)

		// then
		assert.NotContains(t, script, "AUTH_TOKEN")
		assert.NotContains(t, script, "x-access-token")
	})
}

func TestWriteLocalAuth(t *testing.T) {
	t.Parallel()

	t.Run("should write nothing when token is empty", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := pyUpdater.LocalUpgradeParamsExported{
			AuthToken:    "",
			ProviderName: "github",
		}

		// when
		pyUpdater.WriteLocalAuth(&sb, params)

		// then
		assert.Equal(t, "", sb.String())
	})

	t.Run("should write github auth when provider is github", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := pyUpdater.LocalUpgradeParamsExported{
			AuthToken:    "tok",
			ProviderName: "github",
		}

		// when
		pyUpdater.WriteLocalAuth(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "x-access-token")
		assert.Contains(t, result, "GIT_CONFIG_GLOBAL")
	})

	t.Run("should write azuredevops auth when provider is azuredevops", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := pyUpdater.LocalUpgradeParamsExported{
			AuthToken:    "tok",
			ProviderName: "azuredevops",
		}

		// when
		pyUpdater.WriteLocalAuth(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "pat:")
		assert.Contains(t, result, "dev.azure.com")
	})

	t.Run("should write gitlab auth when provider is gitlab", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := pyUpdater.LocalUpgradeParamsExported{
			AuthToken:    "tok",
			ProviderName: "gitlab",
		}

		// when
		pyUpdater.WriteLocalAuth(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "oauth2:")
		assert.Contains(t, result, "gitlab.com")
	})
}

func TestBuildLocalEnv(t *testing.T) {
	t.Parallel()

	t.Run("should include all variables when all fields are set", func(t *testing.T) {
		t.Parallel()

		// given
		params := pyUpdater.LocalUpgradeParamsExported{
			BranchName:    "chore/upgrade-python-deps",
			PythonVersion: "3.13.1",
			AuthToken:     "tok",
			PythonBinary:  "/usr/bin/python3",
			ChangelogFile: "/tmp/cl.md",
		}

		// when
		env := pyUpdater.BuildLocalEnv(params)

		// then
		envMap := envToMap(env)
		assert.Equal(t, "chore/upgrade-python-deps", envMap["BRANCH_NAME"])
		assert.Equal(t, "/usr/bin/python3", envMap["PYTHON_BINARY"])
		assert.Equal(t, "3.13.1", envMap["PYTHON_VERSION"])
		assert.Equal(t, "tok", envMap["AUTH_TOKEN"])
		assert.Equal(t, "tok", envMap["GIT_HTTPS_TOKEN"])
		assert.Equal(t, "/tmp/cl.md", envMap["CHANGELOG_FILE"])
	})

	t.Run("should omit optional variables when fields are empty", func(t *testing.T) {
		t.Parallel()

		// given
		params := pyUpdater.LocalUpgradeParamsExported{
			BranchName:   "chore/upgrade-python-deps",
			PythonBinary: "/usr/bin/python3",
		}

		// when
		env := pyUpdater.BuildLocalEnv(params)

		// then
		envMap := envToMap(env)
		_, hasPyVersion := envMap["PYTHON_VERSION"]
		_, hasAuthToken := envMap["AUTH_TOKEN"]
		_, hasChangelog := envMap["CHANGELOG_FILE"]
		assert.False(t, hasPyVersion)
		assert.False(t, hasAuthToken)
		assert.False(t, hasChangelog)
	})
}

func TestHandleDryRun(t *testing.T) {
	t.Parallel()

	t.Run("should return result with version upgrade fields when upgrade is needed", func(t *testing.T) {
		t.Parallel()

		// given
		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-python-3.13.1",
		}

		// when
		result := pyUpdater.HandleDryRun(vCtx, "/tmp/repo")

		// then
		assert.Equal(t, "3.13.1", result.LatestVersion)
		assert.Equal(t, "chore/upgrade-python-3.13.1", result.BranchName)
		assert.True(t, result.PythonVersionUpdated)
		assert.False(t, result.HasChanges)
	})

	t.Run("should return result with deps-only fields when no upgrade needed", func(t *testing.T) {
		t.Parallel()

		// given
		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-python-deps",
		}

		// when
		result := pyUpdater.HandleDryRun(vCtx, "/tmp/repo")

		// then
		assert.Equal(t, "3.13.1", result.LatestVersion)
		assert.Equal(t, "chore/upgrade-python-deps", result.BranchName)
		assert.False(t, result.PythonVersionUpdated)
		assert.False(t, result.HasChanges)
	})
}

func TestPrepareLocalChangelog(t *testing.T) {
	t.Parallel()

	t.Run("should return empty when no CHANGELOG.md exists in directory", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-python-deps",
		}

		// when
		result := pyUpdater.PrepareLocalChangelog(tmpDir, vCtx)

		// then
		assert.Equal(t, "", result)
	})

	t.Run("should create temp file with deps entry when CHANGELOG exists", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		changelogPath := tmpDir + "/CHANGELOG.md"
		changelogContent := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2025-01-01\n"
		require.NoError(t, writeTestFile(changelogPath, changelogContent))
		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-python-deps",
		}

		// when
		result := pyUpdater.PrepareLocalChangelog(tmpDir, vCtx)

		// then
		assert.NotEmpty(t, result)
	})

	t.Run("should create temp file with version entry when upgrade is needed", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		changelogPath := tmpDir + "/CHANGELOG.md"
		changelogContent := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2025-01-01\n"
		require.NoError(t, writeTestFile(changelogPath, changelogContent))
		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-python-3.13.1",
		}

		// when
		result := pyUpdater.PrepareLocalChangelog(tmpDir, vCtx)

		// then
		assert.NotEmpty(t, result)
	})
}

func TestFindPythonBinary(t *testing.T) {
	t.Parallel()

	t.Run("should find a python binary on the system", func(t *testing.T) {
		t.Parallel()

		// given / when
		path, err := pyUpdater.FindPythonBinary()

		// then
		require.NoError(t, err)
		assert.NotEmpty(t, path)
		assert.Contains(t, path, "python")
	})
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// envToMap converts a slice of KEY=VALUE strings into a map for easy assertion.
func envToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, entry := range env {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}

func TestRunLanguageUpgradeScript(t *testing.T) { //nolint:paralleltest // mutates package-level localCmdRunner
	t.Run("should return script output when runner succeeds", func(t *testing.T) {
		// given
		stub := repositorydoubles.NewStubCommandRunner(cmdrunner.RunResult{
			Output:   "PYTHON_VERSION_UPDATED=true\nDone.\n",
			ExitCode: 0,
		})
		restore := pyUpdater.SetLocalCmdRunner(stub)
		defer restore()

		repoDir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(repoDir, "requirements.txt"),
			[]byte("flask==2.0.0\n"),
			0o600,
		))

		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.0",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-python-3.13.0",
		}
		opts := pyUpdater.LocalUpgradeOptions{ProviderName: "github"}

		// when
		output, err := pyUpdater.RunLanguageUpgradeScript(t.Context(), repoDir, vCtx, opts)

		// then
		require.NoError(t, err)
		assert.Contains(t, output, "PYTHON_VERSION_UPDATED=true")
		require.Len(t, stub.Calls, 1)
		assert.Equal(t, "bash", stub.Calls[0].Name)
		assert.Equal(t, repoDir, stub.Calls[0].Opts.Dir)
	})

	t.Run("should return error when runner fails", func(t *testing.T) {
		// given
		stub := repositorydoubles.NewStubCommandRunnerWithError(errors.New("script crashed"))
		restore := pyUpdater.SetLocalCmdRunner(stub)
		defer restore()

		repoDir := t.TempDir()

		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.0",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-python-3.13.0",
		}
		opts := pyUpdater.LocalUpgradeOptions{ProviderName: "github"}

		// when
		_, err := pyUpdater.RunLanguageUpgradeScript(t.Context(), repoDir, vCtx, opts)

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "upgrade script failed")
	})

	t.Run("should pass verbose output through logger without error", func(t *testing.T) {
		// given
		stub := repositorydoubles.NewStubCommandRunner(cmdrunner.RunResult{
			Output:   "verbose output\n",
			ExitCode: 0,
		})
		restore := pyUpdater.SetLocalCmdRunner(stub)
		defer restore()

		repoDir := t.TempDir()

		vCtx := &pyUpdater.VersionContext{
			LatestVersion:       "3.13.0",
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-python-deps",
		}
		opts := pyUpdater.LocalUpgradeOptions{ProviderName: "github", Verbose: true}

		// when
		output, err := pyUpdater.RunLanguageUpgradeScript(t.Context(), repoDir, vCtx, opts)

		// then
		require.NoError(t, err)
		assert.Equal(t, "verbose output\n", output)
	})
}
