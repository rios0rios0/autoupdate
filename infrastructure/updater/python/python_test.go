package python //nolint:testpackage // tests unexported functions

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/domain"
	testdoubles "github.com/rios0rios0/autoupdate/test"
)

func TestPythonUpdater_Name(t *testing.T) {
	t.Parallel()

	t.Run("should return python", func(t *testing.T) {
		t.Parallel()

		// given
		u := New()

		// when
		name := u.Name()

		// then
		assert.Equal(t, "python", name)
	})
}

func TestPythonUpdater_Detect(t *testing.T) {
	t.Parallel()

	t.Run("should detect repository with requirements.txt", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{"requirements.txt": true},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}
		u := New()

		// when
		result := u.Detect(ctx, provider, repo)

		// then
		assert.True(t, result)
	})

	t.Run("should detect repository with pyproject.toml", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{"pyproject.toml": true},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}
		u := New()

		// when
		result := u.Detect(ctx, provider, repo)

		// then
		assert.True(t, result)
	})

	t.Run("should not detect repository without Python files", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}
		u := New()

		// when
		result := u.Detect(ctx, provider, repo)

		// then
		assert.False(t, result)
	})
}

func TestParsePythonVersionFile(t *testing.T) {
	t.Parallel()

	t.Run("should extract version from .python-version content", func(t *testing.T) {
		t.Parallel()

		// given
		content := "3.12.8\n"

		// when
		version := parsePythonVersionFile(content)

		// then
		assert.Equal(t, "3.12.8", version)
	})

	t.Run("should handle version with leading/trailing whitespace", func(t *testing.T) {
		t.Parallel()

		// given
		content := "  3.13.1  \n"

		// when
		version := parsePythonVersionFile(content)

		// then
		assert.Equal(t, "3.13.1", version)
	})

	t.Run("should skip comment lines", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Python version\n3.12.8\n"

		// when
		version := parsePythonVersionFile(content)

		// then
		assert.Equal(t, "3.12.8", version)
	})

	t.Run("should return empty string for empty file", func(t *testing.T) {
		t.Parallel()

		// given
		content := "\n"

		// when
		version := parsePythonVersionFile(content)

		// then
		assert.Empty(t, version)
	})
}

func TestIsActiveRelease(t *testing.T) {
	t.Parallel()

	t.Run("should return true when EOL is false", func(t *testing.T) {
		t.Parallel()

		// given
		release := pythonRelease{Cycle: "3.13", Latest: "3.13.1", EOL: false}

		// when
		result := isActiveRelease(release)

		// then
		assert.True(t, result)
	})

	t.Run("should return false when EOL is true", func(t *testing.T) {
		t.Parallel()

		// given
		release := pythonRelease{Cycle: "2.7", Latest: "2.7.18", EOL: true}

		// when
		result := isActiveRelease(release)

		// then
		assert.False(t, result)
	})

	t.Run("should return true when EOL date is in the future", func(t *testing.T) {
		t.Parallel()

		// given
		release := pythonRelease{Cycle: "3.12", Latest: "3.12.8", EOL: "2099-10-01"}

		// when
		result := isActiveRelease(release)

		// then
		assert.True(t, result)
	})

	t.Run("should return false when EOL date is in the past", func(t *testing.T) {
		t.Parallel()

		// given
		release := pythonRelease{Cycle: "3.8", Latest: "3.8.20", EOL: "2020-01-01"}

		// when
		result := isActiveRelease(release)

		// then
		assert.False(t, result)
	})
}

func TestResolveVersionContext(t *testing.T) {
	t.Parallel()

	t.Run("should choose version-upgrade branch when .python-version is older", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{".python-version": true},
			FileContents: map[string]string{
				".python-version": "3.12.0\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := resolveVersionContext(ctx, provider, repo, "3.13.1")

		// then
		assert.Equal(t, "3.13.1", vCtx.LatestVersion)
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-python-3.13.1", vCtx.BranchName)
	})

	t.Run("should choose deps-only branch when .python-version matches latest", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{".python-version": true},
			FileContents: map[string]string{
				".python-version": "3.13.1\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := resolveVersionContext(ctx, provider, repo, "3.13.1")

		// then
		assert.Equal(t, "3.13.1", vCtx.LatestVersion)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-python-deps", vCtx.BranchName)
	})

	t.Run("should choose deps-only branch when no .python-version file", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := resolveVersionContext(ctx, provider, repo, "3.13.1")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-python-deps", vCtx.BranchName)
	})

	t.Run("should choose deps-only branch when latest version is empty", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{".python-version": true},
			FileContents: map[string]string{
				".python-version": "3.12.0\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := resolveVersionContext(ctx, provider, repo, "")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-python-deps", vCtx.BranchName)
	})
}

func TestPrepareChangelog(t *testing.T) {
	t.Parallel()

	t.Run("should return temp file path when CHANGELOG.md exists and version upgrade", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{"CHANGELOG.md": true},
			FileContents: map[string]string{
				"CHANGELOG.md": "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}
		vCtx := &versionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-python-3.13.1",
		}

		// when
		path := prepareChangelog(ctx, provider, repo, vCtx)

		// then
		assert.NotEmpty(t, path)
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "### Changed")
		assert.Contains(t, string(content), "- changed the Python version to `3.13.1`")
		os.Remove(path) // cleanup
	})

	t.Run("should return temp file with deps-only entry when not upgrading version", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{"CHANGELOG.md": true},
			FileContents: map[string]string{
				"CHANGELOG.md": "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}
		vCtx := &versionContext{
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-python-deps",
		}

		// when
		path := prepareChangelog(ctx, provider, repo, vCtx)

		// then
		assert.NotEmpty(t, path)
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "- changed the Python dependencies to their latest versions")
		os.Remove(path)
	})

	t.Run("should return empty string when CHANGELOG.md is absent", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}
		vCtx := &versionContext{
			LatestVersion:       "3.13.1",
			NeedsVersionUpgrade: true,
		}

		// when
		path := prepareChangelog(ctx, provider, repo, vCtx)

		// then
		assert.Empty(t, path)
	})
}

func TestFindPythonBinary(t *testing.T) {
	t.Parallel()

	t.Run("should find python binary on system", func(t *testing.T) {
		t.Parallel()

		// given - a system where Python is installed

		// when
		path, err := findPythonBinary()

		// then
		if err == nil {
			assert.NotEmpty(t, path)
			assert.Contains(t, path, "python")
		}
		// If Python is genuinely not installed, the error is expected
	})
}

func TestBuildUpgradeScript(t *testing.T) {
	t.Parallel()

	t.Run("should include clone, checkout, and pip upgrade commands", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			CloneURL:        "https://github.com/org/repo.git",
			DefaultBranch:   "main",
			BranchName:      "chore/upgrade-python-3.13.1",
			PythonVersion:   "3.13.1",
			AuthToken:       "token",
			ProviderName:    "github",
			HasRequirements: true,
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo")

		// then
		assert.Contains(t, script, "#!/bin/bash")
		assert.Contains(t, script, "set -euo pipefail")
		assert.Contains(t, script, "git clone")
		assert.Contains(t, script, "git checkout -b")
		assert.Contains(t, script, "pip install --upgrade pip")
		assert.Contains(t, script, "pip install --upgrade -r requirements.txt")
		assert.Contains(t, script, "pip freeze > requirements.txt")
		assert.Contains(t, script, "PYTHON_VERSION_UPDATED=true")
		assert.Contains(t, script, "PYTHON_VERSION_UPDATED=false")
		assert.Contains(t, script, "CHANGES_PUSHED=true")
		assert.Contains(t, script, "CHANGES_PUSHED=false")
	})

	t.Run("should configure GitHub git auth", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			ProviderName:    "github",
			HasRequirements: true,
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo")

		// then
		assert.Contains(t, script, "x-access-token")
		assert.Contains(t, script, "github.com")
	})

	t.Run("should configure Azure DevOps git auth", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			ProviderName:    "azuredevops",
			HasRequirements: true,
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo")

		// then
		assert.Contains(t, script, "dev.azure.com")
		assert.Contains(t, script, "pat:")
	})

	t.Run("should configure GitLab git auth", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			ProviderName:    "gitlab",
			HasRequirements: true,
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo")

		// then
		assert.Contains(t, script, "oauth2")
		assert.Contains(t, script, "gitlab.com")
	})

	t.Run("should include pyproject.toml upgrade when present", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			ProviderName: "github",
			HasPyproject: true,
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo")

		// then
		assert.Contains(t, script, "pyproject.toml")
		assert.Contains(t, script, "pip install --upgrade .")
	})

	t.Run("should include Dockerfile python image update section", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			ProviderName:    "github",
			HasRequirements: true,
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo")

		// then
		assert.Contains(t, script, "PYTHON_VERSION_CHANGED")
		assert.Contains(t, script, "python:${PYTHON_VERSION}")
	})
}

func TestBuildEnv(t *testing.T) {
	t.Parallel()

	t.Run("should include all required environment variables", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			AuthToken:     "my-token",
			CloneURL:      "https://example.com/repo.git",
			BranchName:    "upgrade-branch",
			PythonVersion: "3.13.1",
			DefaultBranch: "main",
			PythonBinary:  "/usr/bin/python3",
		}

		// when
		env := buildEnv(params, "/tmp/repo")

		// then
		assert.Contains(t, env, "AUTH_TOKEN=my-token")
		assert.Contains(t, env, "CLONE_URL=https://example.com/repo.git")
		assert.Contains(t, env, "BRANCH_NAME=upgrade-branch")
		assert.Contains(t, env, "PYTHON_VERSION=3.13.1")
		assert.Contains(t, env, "REPO_DIR=/tmp/repo")
		assert.Contains(t, env, "DEFAULT_BRANCH=main")
		assert.Contains(t, env, "PYTHON_BINARY=/usr/bin/python3")
	})

	t.Run("should include CHANGELOG_FILE when set", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			AuthToken:     "token",
			ChangelogFile: "/tmp/changelog-12345.md",
			DefaultBranch: "main",
		}

		// when
		env := buildEnv(params, "/tmp/repo")

		// then
		assert.Contains(t, env, "CHANGELOG_FILE=/tmp/changelog-12345.md")
	})

	t.Run("should not include CHANGELOG_FILE when empty", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			AuthToken:     "token",
			DefaultBranch: "main",
		}

		// when
		env := buildEnv(params, "/tmp/repo")

		// then
		for _, e := range env {
			assert.NotContains(t, e, "CHANGELOG_FILE")
		}
	})
}

func TestGeneratePRDescription(t *testing.T) {
	t.Parallel()

	t.Run("should include Python version upgrade in description", func(t *testing.T) {
		t.Parallel()

		// given
		pyVersion := "3.13.1"
		pyVersionUpdated := true

		// when
		desc := GeneratePRDescription(pyVersion, pyVersionUpdated)

		// then
		assert.Contains(t, desc, "## Summary")
		assert.Contains(t, desc, "upgrades the Python version to **3.13.1**")
		assert.Contains(t, desc, ".python-version")
		assert.Contains(t, desc, "pip install --upgrade")
		assert.Contains(t, desc, "pip freeze")
	})

	t.Run("should indicate deps-only update when version was already current", func(t *testing.T) {
		t.Parallel()

		// given
		pyVersion := "3.13.1"
		pyVersionUpdated := false

		// when
		desc := GeneratePRDescription(pyVersion, pyVersionUpdated)

		// then
		assert.Contains(t, desc, "## Summary")
		assert.Contains(t, desc, "updates all Python pip dependencies")
		assert.NotContains(t, desc, ".python-version")
	})

	t.Run("should include review checklist", func(t *testing.T) {
		t.Parallel()

		// given
		pyVersion := "3.13.1"

		// when
		desc := GeneratePRDescription(pyVersion, true)

		// then
		assert.Contains(t, desc, "### Review Checklist")
		assert.Contains(t, desc, "Verify build passes")
		assert.Contains(t, desc, "Verify tests pass")
		assert.Contains(t, desc, "requirements.txt")
	})

	t.Run("should include autoupdate attribution", func(t *testing.T) {
		t.Parallel()

		// given
		pyVersion := "3.13.1"

		// when
		desc := GeneratePRDescription(pyVersion, false)

		// then
		assert.Contains(t, desc, "autoupdate")
		assert.Contains(t, desc, "github.com/rios0rios0/autoupdate")
	})
}

func TestBuildLocalUpgradeScript(t *testing.T) {
	t.Parallel()

	t.Run("should not include clone or auth when no token is provided", func(t *testing.T) {
		t.Parallel()

		// given
		params := localUpgradeParams{
			BranchName:      "chore/upgrade-python-deps",
			PythonVersion:   "3.13.1",
			HasRequirements: true,
			PythonBinary:    "/usr/bin/python3",
		}

		// when
		script := buildLocalUpgradeScript(params)

		// then
		assert.Contains(t, script, "#!/bin/bash")
		assert.Contains(t, script, "set -euo pipefail")
		assert.NotContains(t, script, "git clone")
		assert.NotContains(t, script, "TEMP_GITCONFIG")
		assert.Contains(t, script, "git status --porcelain")
		assert.Contains(t, script, "uncommitted changes")
		assert.Contains(t, script, "git checkout -b")
		assert.Contains(t, script, "pip install --upgrade")
		assert.Contains(t, script, "CHANGES_PUSHED")
	})

	t.Run("should include auth when token is provided", func(t *testing.T) {
		t.Parallel()

		// given
		params := localUpgradeParams{
			BranchName:      "chore/upgrade-python-deps",
			PythonVersion:   "3.13.1",
			AuthToken:       "my-token",
			ProviderName:    "github",
			HasRequirements: true,
			PythonBinary:    "/usr/bin/python3",
		}

		// when
		script := buildLocalUpgradeScript(params)

		// then
		assert.Contains(t, script, "TEMP_GITCONFIG")
		assert.Contains(t, script, "x-access-token")
		assert.Contains(t, script, "GIT_CONFIG_GLOBAL")
		assert.NotContains(t, script, "git clone")
	})

	t.Run("should place Dockerfile update between pip upgrade and changelog", func(t *testing.T) {
		t.Parallel()

		// given
		params := localUpgradeParams{
			BranchName:      "chore/upgrade-python-deps",
			PythonVersion:   "3.13.1",
			HasRequirements: true,
			PythonBinary:    "/usr/bin/python3",
		}

		// when
		script := buildLocalUpgradeScript(params)

		// then
		pipIdx := strings.Index(script, "pip install --upgrade -r requirements.txt")
		dockerfileIdx := strings.Index(script, "Updating Dockerfile python image tags")
		changelogIdx := strings.Index(script, "Updating CHANGELOG.md")

		assert.Greater(t, dockerfileIdx, pipIdx, "Dockerfile update should come after pip upgrade")
		assert.Greater(t, changelogIdx, dockerfileIdx, "CHANGELOG update should come after Dockerfile update")
	})
}

func TestBuildLocalEnv(t *testing.T) {
	t.Parallel()

	t.Run("should include core variables without auth when no token", func(t *testing.T) {
		t.Parallel()

		// given
		params := localUpgradeParams{
			BranchName:    "chore/upgrade-python-deps",
			PythonVersion: "3.13.1",
			PythonBinary:  "/usr/bin/python3",
		}

		// when
		env := buildLocalEnv(params)

		// then
		assert.Contains(t, env, "BRANCH_NAME=chore/upgrade-python-deps")
		assert.Contains(t, env, "PYTHON_VERSION=3.13.1")
		assert.Contains(t, env, "PYTHON_BINARY=/usr/bin/python3")
	})

	t.Run("should include AUTH_TOKEN when token is provided", func(t *testing.T) {
		t.Parallel()

		// given
		params := localUpgradeParams{
			BranchName:    "branch",
			PythonVersion: "3.13.1",
			AuthToken:     "my-secret-token",
			PythonBinary:  "/usr/bin/python3",
		}

		// when
		env := buildLocalEnv(params)

		// then
		assert.Contains(t, env, "AUTH_TOKEN=my-secret-token")
		assert.Contains(t, env, "GIT_HTTPS_TOKEN=my-secret-token")
	})
}
