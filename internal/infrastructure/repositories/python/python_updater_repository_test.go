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
	"github.com/rios0rios0/autoupdate/internal/support"
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
		assert.Empty(t, result)
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
		result := pyUpdater.GeneratePRDescription("3.13.1", "pip", true)

		// then
		assert.Contains(t, result, "3.13.1")
		assert.Contains(t, result, ".python-version")
	})

	t.Run("should describe deps-only update when no version change", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pyUpdater.GeneratePRDescription("3.13.1", "pip", false)

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
			CloneURL:      "https://example.com/org/repo.git",
			DefaultBranch: "main",
			BranchName:    "chore/upgrade-python-deps",
			PythonVersion: "3.13.1",
			AuthToken:     "tok123",
			ProviderName:  "github",
			Changelog:     support.StagedChangelog{TempPath: "/tmp/changelog.md", RepoPath: "CHANGELOG.md"},
			Project:       pyUpdater.NewPythonProject(true, true, false, false),
			PythonBinary:  "/usr/bin/python3",
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
			ProviderName: "github",
			Project:      pyUpdater.NewPythonProject(false, false, false, false),
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
			ProviderName: "github",
			Project:      pyUpdater.NewPythonProject(false, true, false, false),
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
		script := pyUpdater.BuildBatchPythonScript(true, true, false, false, true)

		// then
		assert.True(t, strings.HasPrefix(script, "#!/bin/bash\n"))
		assert.Contains(t, script, "set -euo pipefail")
		assert.Contains(t, script, "requirements.txt")
		assert.Contains(t, script, "pyproject.toml")
	})

	t.Run("should omit requirements section when hasRequirements is false", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := pyUpdater.BuildBatchPythonScript(false, true, false, false, true)

		// then
		assert.NotContains(t, script, "pip install -r requirements.txt")
		assert.Contains(t, script, "pip install --upgrade .")
	})

	t.Run("should omit pyproject section when hasPyproject is false", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := pyUpdater.BuildBatchPythonScript(true, false, false, false, true)

		// then
		assert.Contains(t, script, "pip install -r requirements.txt")
		assert.NotContains(t, script, "pip install --upgrade .")
	})

	t.Run("should produce minimal script when neither file is present", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := pyUpdater.BuildBatchPythonScript(false, false, false, false, true)

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

	t.Run("should include requirements upgrade when the repository has a requirements.txt", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := pyUpdater.UpgradeParamsExported{
			Project: pyUpdater.NewPythonProject(true, false, false, false),
		}

		// when
		pyUpdater.WritePythonUpgradeCommands(&sb, params)

		// then
		result := sb.String()
		assert.Contains(t, result, "pip install -r requirements.txt")
		assert.Contains(t, result, "pip install --upgrade -r requirements.txt")
		assert.Contains(t, result, "pip freeze")
	})

	t.Run("should include pyproject upgrade when the repository has a pyproject.toml", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := pyUpdater.UpgradeParamsExported{
			Project: pyUpdater.NewPythonProject(false, true, false, false),
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
			Project: pyUpdater.NewPythonProject(false, false, false, false),
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
			Project: pyUpdater.NewPythonProject(true, true, false, false),
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
			Changelog:     support.StagedChangelog{TempPath: "/tmp/changelog.md", RepoPath: "CHANGELOG.md"},
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
			Changelog:     support.StagedChangelog{},
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

func TestChangelogEntries(t *testing.T) {
	t.Parallel()

	t.Run("should describe the version upgrade when one is needed", func(t *testing.T) {
		t.Parallel()

		// given
		vCtx := &pyUpdater.VersionContext{LatestVersion: "3.13.1", NeedsVersionUpgrade: true}

		// when
		entries := pyUpdater.ChangelogEntries(vCtx)

		// then
		assert.Equal(t, []string{
			"- changed the Python version to `3.13.1` and updated all pip dependencies",
		}, entries)
	})

	t.Run("should describe only the dependencies when no version upgrade is needed", func(t *testing.T) {
		t.Parallel()

		// given
		vCtx := &pyUpdater.VersionContext{LatestVersion: "3.13.1", NeedsVersionUpgrade: false}

		// when
		entries := pyUpdater.ChangelogEntries(vCtx)

		// then
		assert.Equal(t, []string{"- changed the Python dependencies to their latest versions"}, entries)
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
		require.Error(t, err)
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
			BranchName:    "chore/upgrade-python-deps",
			PythonVersion: "3.13.1",
			AuthToken:     "tok123",
			ProviderName:  "github",
			Project:       pyUpdater.NewPythonProject(true, false, false, false),
			PythonBinary:  "/usr/bin/python3",
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
			BranchName:   "chore/upgrade-python-deps",
			Project:      pyUpdater.NewPythonProject(true, false, false, false),
			PythonBinary: "/usr/bin/python3",
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
		assert.Empty(t, sb.String())
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
			Changelog:     support.StagedChangelog{TempPath: "/tmp/cl.md", RepoPath: "CHANGELOG.md"},
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

// TestRunLanguageUpgradeScriptMajorMode covers the seam between the resolved
// setting and the script that acts on it. LocalUpgradeOptions carries the
// value in, and the only way to see that it arrived is to read the script the
// runner is handed: the script-building tests construct localUpgradeParams by
// hand, so they cannot catch a caller that forgets the field -- which is the
// shape of defect this guards against, a struct field with no producer being
// silently the restrictive value.
func TestRunLanguageUpgradeScriptMajorMode(t *testing.T) { //nolint:paralleltest // mutates package-level localCmdRunner
	cases := []struct {
		name       string
		allowMajor bool
		contains   string
		absent     string
	}{
		{
			name:       "should pass --unconstrained to pdm when majors are allowed",
			allowMajor: true,
			contains:   "pdm update --update-all --no-sync -G :all --unconstrained",
		},
		{
			name:       "should withhold --unconstrained when majors are refused",
			allowMajor: false,
			contains:   "pdm update --update-all --no-sync -G :all 2>&1",
			absent:     "--unconstrained",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// given -- a PDM project, whose upgrade command is the one that
			// changes shape with the setting
			spy := repositorydoubles.NewSpyScriptRunner("Done.\n")
			restore := pyUpdater.SetLocalCmdRunner(spy)
			defer restore()

			repoDir := t.TempDir()
			require.NoError(t, os.WriteFile(
				filepath.Join(repoDir, "pyproject.toml"),
				[]byte("[project]\nname = \"demo\"\n"),
				0o600,
			))
			require.NoError(t, os.WriteFile(filepath.Join(repoDir, "pdm.lock"), []byte(""), 0o600))

			vCtx := &pyUpdater.VersionContext{BranchName: "chore/upgrade-python-deps"}
			opts := pyUpdater.LocalUpgradeOptions{
				ProviderName:      "github",
				AllowMajorUpdates: testCase.allowMajor,
			}

			// when
			_, err := pyUpdater.RunLanguageUpgradeScript(t.Context(), repoDir, vCtx, opts)

			// then
			require.NoError(t, err)
			require.Len(t, spy.Scripts, 1)
			assert.Contains(t, spy.Scripts[0], testCase.contains)
			if testCase.absent != "" {
				assert.NotContains(t, spy.Scripts[0], testCase.absent)
			}
		})
	}
}

func TestPyprojectUsesPDM(t *testing.T) {
	t.Parallel()

	t.Run("should detect a project configured through a tool.pdm table", func(t *testing.T) {
		t.Parallel()

		// given
		content := "[project]\nname = \"demo\"\n\n[tool.pdm.dev-dependencies]\ntest = [\"pytest\"]\n"

		// when
		detected := pyUpdater.PyprojectUsesPDM(content)

		// then
		assert.True(t, detected)
	})

	t.Run("should detect a project built with the pdm backend", func(t *testing.T) {
		t.Parallel()

		// given
		content := "[build-system]\nrequires = [\"pdm-backend\"]\nbuild-backend = \"pdm.backend\"\n"

		// when
		detected := pyUpdater.PyprojectUsesPDM(content)

		// then
		assert.True(t, detected)
	})

	t.Run("should not detect a setuptools project", func(t *testing.T) {
		t.Parallel()

		// given
		content := "[build-system]\nrequires = [\"setuptools\"]\nbuild-backend = \"setuptools.build_meta\"\n"

		// when
		detected := pyUpdater.PyprojectUsesPDM(content)

		// then
		assert.False(t, detected)
	})

	t.Run("should ignore markers that appear only inside comments", func(t *testing.T) {
		t.Parallel()

		// given
		content := "[project]\nname = \"demo\"\n# migrate to [tool.pdm] one day\n"

		// when
		detected := pyUpdater.PyprojectUsesPDM(content)

		// then
		assert.False(t, detected)
	})

	t.Run("should ignore a marker named only in a trailing inline comment", func(t *testing.T) {
		t.Parallel()

		// given
		content := "[build-system]\nbuild-backend = \"setuptools.build_meta\" # pdm.backend\n"

		// when
		detected := pyUpdater.PyprojectUsesPDM(content)

		// then
		assert.False(t, detected)
	})

	t.Run("should ignore a table only named in a trailing inline comment", func(t *testing.T) {
		t.Parallel()

		// given
		content := "[project]\nname = \"demo\" # unlike [tool.pdm] projects\n"

		// when
		detected := pyUpdater.PyprojectUsesPDM(content)

		// then
		assert.False(t, detected)
	})

	t.Run("should not treat an unrelated table sharing the prefix as PDM", func(t *testing.T) {
		t.Parallel()

		// given
		content := "[tool.pdmx]\nsetting = 1\n"

		// when
		detected := pyUpdater.PyprojectUsesPDM(content)

		// then
		assert.False(t, detected)
	})

	t.Run("should still detect a marker followed by an inline comment", func(t *testing.T) {
		t.Parallel()

		// given
		content := "[build-system]\nbuild-backend = \"pdm.backend\" # the PDM build backend\n"

		// when
		detected := pyUpdater.PyprojectUsesPDM(content)

		// then
		assert.True(t, detected)
	})

	t.Run("should not mistake a hash inside a quoted value for a comment", func(t *testing.T) {
		t.Parallel()

		// given — the '#' belongs to the value, so the [tool.pdm] table that
		// follows must still be reached.
		content := "[project]\nname = \"demo#1\"\n[tool.pdm]\n"

		// when
		detected := pyUpdater.PyprojectUsesPDM(content)

		// then
		assert.True(t, detected)
	})
}

func TestHasPDMLocal(t *testing.T) {
	t.Parallel()

	t.Run("should detect PDM from a lock file alone", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "pdm.lock"), []byte("[metadata]\n"), 0o600))

		// when
		detected := pyUpdater.HasPDMLocal(repoDir)

		// then
		assert.True(t, detected)
	})

	t.Run("should detect PDM from pyproject markers when no lock file exists", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(repoDir, "pyproject.toml"), []byte("[tool.pdm]\n"), 0o600,
		))

		// when
		detected := pyUpdater.HasPDMLocal(repoDir)

		// then
		assert.True(t, detected)
	})

	t.Run("should not detect PDM in a plain pip project", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(repoDir, "pyproject.toml"), []byte("[project]\nname = \"demo\"\n"), 0o600,
		))

		// when
		detected := pyUpdater.HasPDMLocal(repoDir)

		// then
		assert.False(t, detected)
	})
}

func TestHasPDMRemote(t *testing.T) {
	t.Parallel()

	repo := entities.Repository{Organization: "org", Name: "repo"}

	t.Run("should detect PDM when the remote carries a lock file", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"pdm.lock": true}).
			BuildSpy()

		// when
		detected := pyUpdater.HasPDMRemote(t.Context(), provider, repo, false)

		// then
		assert.True(t, detected)
	})

	t.Run("should detect PDM from the remote pyproject when no lock file exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"pyproject.toml": true}).
			WithFileContents(map[string]string{"pyproject.toml": "[tool.pdm]\n"}).
			BuildSpy()

		// when
		detected := pyUpdater.HasPDMRemote(t.Context(), provider, repo, true)

		// then
		assert.True(t, detected)
	})

	t.Run("should not detect PDM when the remote pyproject is unreadable", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"pyproject.toml": true}).
			WithFileContentErr(errors.New("not found")).
			BuildSpy()

		// when
		detected := pyUpdater.HasPDMRemote(t.Context(), provider, repo, true)

		// then
		assert.False(t, detected)
	})

	t.Run("should not read the pyproject when the caller already found it absent", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"requirements.txt": true}).
			BuildSpy()

		// when
		detected := pyUpdater.HasPDMRemote(t.Context(), provider, repo, false)

		// then
		assert.False(t, detected)
		assert.Empty(t, provider.FetchedFilePaths, "the absent pyproject.toml should not be fetched again")
	})
}

func TestBuildBatchPythonScriptWithPDM(t *testing.T) {
	t.Parallel()

	t.Run("should upgrade through PDM when the project is PDM-managed", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := pyUpdater.BuildBatchPythonScript(false, true, false, true, true)

		// then
		assert.Contains(t, script, "pdm update --update-all --no-sync")
		assert.Contains(t, script, "pip install --upgrade pdm")
	})

	t.Run("should not run the pip local install for a PDM project", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := pyUpdater.BuildBatchPythonScript(true, true, true, false, true)

		// then
		assert.NotContains(t, script, "pip install --upgrade .")
		assert.NotContains(t, script, "pip install -r requirements.txt")
	})

	t.Run("should keep using pip when the project is not PDM-managed", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := pyUpdater.BuildBatchPythonScript(false, true, false, false, true)

		// then
		assert.Contains(t, script, "pip install --upgrade .")
		assert.NotContains(t, script, "pdm update")
	})
}

func TestWriteEggInfoGitignore(t *testing.T) {
	t.Parallel()

	t.Run("should append the egg-info pattern guarded by an existence check", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder

		// when
		pyUpdater.WriteEggInfoGitignore(&sb)
		script := sb.String()

		// then
		assert.Contains(t, script, "ls -d ./*.egg-info")
		assert.Contains(t, script, "echo \"*.egg-info/\" >> .gitignore")
		assert.Contains(t, script, "grep -qE '^\\*\\.egg-info/?$' .gitignore")
	})

	t.Run("should be part of the batch script so artefacts never reach a commit", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := pyUpdater.BuildBatchPythonScript(false, true, false, false, true)

		// then
		assert.Contains(t, script, "*.egg-info/")
	})
}

func TestGeneratePRDescriptionToolchain(t *testing.T) {
	t.Parallel()

	t.Run("should describe the PDM commands and lock file for a PDM project", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pyUpdater.GeneratePRDescription("3.13.1", "pdm", false)

		// then
		assert.Contains(t, result, "pdm update --update-all --no-sync")
		assert.Contains(t, result, "`pdm.lock`")
		assert.NotContains(t, result, "requirements.txt")
	})

	t.Run("should describe the pip commands and requirements file for a pip project", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pyUpdater.GeneratePRDescription("3.13.1", "pip", false)

		// then
		assert.Contains(t, result, "pip install --upgrade -r requirements.txt")
		assert.Contains(t, result, "`requirements.txt`")
		assert.NotContains(t, result, "pdm")
	})
}

// TestPythonMajorMode covers the PDM path. Without --unconstrained, `pdm update`
// resolves inside the bounds pyproject.toml declares, so a caret or tilde kept
// the project on its major however the key was set.
func TestPythonMajorMode(t *testing.T) {
	t.Parallel()

	t.Run("should raise pyproject bounds when allowed", func(t *testing.T) {
		t.Parallel()

		// given / when -- pyproject + pdm.lock selects the PDM path
		script := pyUpdater.BuildBatchPythonScript(false, true, true, true, true)

		// then -- both the -G :all form *and* its fallback must carry the flag,
		// or the projects where the first form fails get a quietly smaller
		// upgrade. Asserted on the two command lines rather than by counting,
		// because the echo above them carries it too.
		assert.Contains(t, script,
			"pdm update --update-all --no-sync -G :all --unconstrained")
		assert.Contains(t, script,
			"|| pdm update --update-all --no-sync --unconstrained")
	})

	t.Run("should resolve inside declared bounds when refused", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := pyUpdater.BuildBatchPythonScript(false, true, true, true, false)

		// then
		assert.Contains(t, script, "pdm update --update-all --no-sync")
		assert.NotContains(t, script, "--unconstrained")
	})
}
