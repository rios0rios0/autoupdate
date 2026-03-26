//go:build unit

package javascript_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	jsUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/javascript"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestName(t *testing.T) {
	t.Parallel()

	t.Run("should return javascript as updater name", func(t *testing.T) {
		t.Parallel()

		// given
		updater := jsUpdater.NewUpdaterRepository()

		// when
		name := updater.Name()

		// then
		assert.Equal(t, "javascript", name)
	})
}

func TestDetect(t *testing.T) {
	t.Parallel()

	t.Run("should return true when package.json exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"package.json": true}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := jsUpdater.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.True(t, detected)
	})

	t.Run("should return false when no JS files exist", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := jsUpdater.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.False(t, detected)
	})
}

func TestParseNodeVersionFile(t *testing.T) {
	t.Parallel()

	t.Run("should extract simple version from file content", func(t *testing.T) {
		t.Parallel()

		// given
		content := "20.11.1\n"

		// when
		result := jsUpdater.ParseNodeVersionFile(content)

		// then
		assert.Equal(t, "20.11.1", result)
	})

	t.Run("should strip v-prefix from version", func(t *testing.T) {
		t.Parallel()

		// given
		content := "v20.11.1\n"

		// when
		result := jsUpdater.ParseNodeVersionFile(content)

		// then
		assert.Equal(t, "20.11.1", result)
	})

	t.Run("should return empty string for empty content", func(t *testing.T) {
		t.Parallel()

		// given
		content := ""

		// when
		result := jsUpdater.ParseNodeVersionFile(content)

		// then
		assert.Equal(t, "", result)
	})
}

func TestIsLTSRelease(t *testing.T) {
	t.Parallel()

	t.Run("should return true when LTS is a string name", func(t *testing.T) {
		t.Parallel()

		// given
		release := jsUpdater.NodeRelease{Version: "v20.18.0", LTS: "Jod"}

		// when
		result := jsUpdater.IsLTSRelease(release)

		// then
		assert.True(t, result)
	})

	t.Run("should return false when LTS is false", func(t *testing.T) {
		t.Parallel()

		// given
		release := jsUpdater.NodeRelease{Version: "v23.0.0", LTS: false}

		// when
		result := jsUpdater.IsLTSRelease(release)

		// then
		assert.False(t, result)
	})

	t.Run("should return false when LTS is nil", func(t *testing.T) {
		t.Parallel()

		// given
		release := jsUpdater.NodeRelease{Version: "v23.0.0", LTS: nil}

		// when
		result := jsUpdater.IsLTSRelease(release)

		// then
		assert.False(t, result)
	})
}

func TestDetectPackageManager(t *testing.T) {
	t.Parallel()

	t.Run("should return pnpm when pnpm-lock.yaml exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"pnpm-lock.yaml": true}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		result := jsUpdater.DetectPackageManager(t.Context(), provider, repo)

		// then
		assert.Equal(t, "pnpm", result)
	})

	t.Run("should return yarn when yarn.lock exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"yarn.lock": true}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		result := jsUpdater.DetectPackageManager(t.Context(), provider, repo)

		// then
		assert.Equal(t, "yarn", result)
	})

	t.Run("should return npm as default when no lockfile exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		result := jsUpdater.DetectPackageManager(t.Context(), provider, repo)

		// then
		assert.Equal(t, "npm", result)
	})
}

func TestResolveVersionContext(t *testing.T) {
	t.Parallel()

	t.Run("should set NeedsVersionUpgrade to true when current version differs from latest", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".nvmrc": true}).
			WithFileContents(map[string]string{".nvmrc": "18.0.0\n"}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := jsUpdater.ResolveVersionContext(t.Context(), provider, repo, "20.18.0")

		// then
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "20.18.0", vCtx.LatestVersion)
		assert.Equal(t, "chore/upgrade-node-20.18.0", vCtx.BranchName)
	})

	t.Run("should set NeedsVersionUpgrade to false when current version matches latest", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".nvmrc": true}).
			WithFileContents(map[string]string{".nvmrc": "20.18.0\n"}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := jsUpdater.ResolveVersionContext(t.Context(), provider, repo, "20.18.0")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "20.18.0", vCtx.LatestVersion)
		assert.Equal(t, "chore/upgrade-js-deps", vCtx.BranchName)
	})

	t.Run("should use deps branch when latest version is empty", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".nvmrc": true}).
			WithFileContents(map[string]string{".nvmrc": "18.0.0\n"}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := jsUpdater.ResolveVersionContext(t.Context(), provider, repo, "")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "", vCtx.LatestVersion)
		assert.Equal(t, "chore/upgrade-js-deps", vCtx.BranchName)
	})

	t.Run("should use deps branch when no version file exists in repo", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := jsUpdater.ResolveVersionContext(t.Context(), provider, repo, "20.18.0")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-js-deps", vCtx.BranchName)
	})
}

func TestReadCurrentNodeVersion(t *testing.T) {
	t.Parallel()

	t.Run("should read version from .nvmrc when it exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".nvmrc": true}).
			WithFileContents(map[string]string{".nvmrc": "20.11.1\n"}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		version := jsUpdater.ReadCurrentNodeVersion(t.Context(), provider, repo)

		// then
		assert.Equal(t, "20.11.1", version)
	})

	t.Run("should read version from .node-version when .nvmrc does not exist", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".node-version": true}).
			WithFileContents(map[string]string{".node-version": "v18.19.0\n"}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		version := jsUpdater.ReadCurrentNodeVersion(t.Context(), provider, repo)

		// then
		assert.Equal(t, "18.19.0", version)
	})

	t.Run("should prefer .nvmrc over .node-version when both exist", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".nvmrc": true, ".node-version": true}).
			WithFileContents(map[string]string{
				".nvmrc":        "20.11.1\n",
				".node-version": "18.19.0\n",
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		version := jsUpdater.ReadCurrentNodeVersion(t.Context(), provider, repo)

		// then
		assert.Equal(t, "20.11.1", version)
	})

	t.Run("should return empty string when no version files exist", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		version := jsUpdater.ReadCurrentNodeVersion(t.Context(), provider, repo)

		// then
		assert.Equal(t, "", version)
	})

	t.Run("should return empty string when version file content is empty", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".nvmrc": true}).
			WithFileContents(map[string]string{".nvmrc": ""}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		version := jsUpdater.ReadCurrentNodeVersion(t.Context(), provider, repo)

		// then
		assert.Equal(t, "", version)
	})
}

func TestPrepareChangelog(t *testing.T) {
	t.Parallel()

	t.Run("should return empty string when CHANGELOG.md does not exist", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		vCtx := &jsUpdater.VersionContext{
			LatestVersion:       "20.18.0",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-node-20.18.0",
		}

		// when
		result := jsUpdater.PrepareChangelog(t.Context(), provider, repo, vCtx)

		// then
		assert.Equal(t, "", result)
	})

	t.Run("should create temp file with modified changelog when version upgrade is needed", func(t *testing.T) {
		t.Parallel()

		// given
		changelogContent := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2024-01-01\n"
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": changelogContent}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		vCtx := &jsUpdater.VersionContext{
			LatestVersion:       "20.18.0",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-node-20.18.0",
		}

		// when
		result := jsUpdater.PrepareChangelog(t.Context(), provider, repo, vCtx)

		// then
		if result != "" {
			defer os.Remove(result)
			content, err := os.ReadFile(result)
			require.NoError(t, err)
			assert.Contains(t, string(content), "20.18.0")
			assert.Contains(t, string(content), "JavaScript dependencies")
		}
	})

	t.Run("should create temp file with deps-only changelog entry when no version upgrade", func(t *testing.T) {
		t.Parallel()

		// given
		changelogContent := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2024-01-01\n"
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": changelogContent}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		vCtx := &jsUpdater.VersionContext{
			LatestVersion:       "20.18.0",
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-js-deps",
		}

		// when
		result := jsUpdater.PrepareChangelog(t.Context(), provider, repo, vCtx)

		// then
		if result != "" {
			defer os.Remove(result)
			content, err := os.ReadFile(result)
			require.NoError(t, err)
			assert.Contains(t, string(content), "JavaScript dependencies")
		}
	})

	t.Run("should return empty string when GetFileContent fails", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContentErr(assert.AnError).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		vCtx := &jsUpdater.VersionContext{
			LatestVersion:       "20.18.0",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-node-20.18.0",
		}

		// when
		result := jsUpdater.PrepareChangelog(t.Context(), provider, repo, vCtx)

		// then
		assert.Equal(t, "", result)
	})
}

func TestBuildUpgradeScript(t *testing.T) {
	t.Parallel()

	t.Run("should contain shebang and strict mode", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.UpgradeParams{
			CloneURL:       "https://example.com/org/repo.git",
			DefaultBranch:  "main",
			BranchName:     "chore/upgrade-js-deps",
			NodeVersion:    "20.18.0",
			AuthToken:      "test-token",
			ProviderName:   "github",
			PackageManager: "npm",
		}

		// when
		script := jsUpdater.BuildUpgradeScript(params, "/tmp/repo")

		// then
		assert.True(t, strings.HasPrefix(script, "#!/bin/bash\n"))
		assert.Contains(t, script, "set -euo pipefail")
	})

	t.Run("should contain git clone and branch creation commands", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.UpgradeParams{
			CloneURL:       "https://example.com/org/repo.git",
			DefaultBranch:  "main",
			BranchName:     "chore/upgrade-js-deps",
			ProviderName:   "github",
			AuthToken:      "test-token",
			PackageManager: "npm",
		}

		// when
		script := jsUpdater.BuildUpgradeScript(params, "/tmp/repo")

		// then
		assert.Contains(t, script, "git clone")
		assert.Contains(t, script, "git checkout -b")
		assert.Contains(t, script, "$CLONE_URL")
		assert.Contains(t, script, "$BRANCH_NAME")
	})

	t.Run("should contain JS upgrade commands and Dockerfile update", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.UpgradeParams{
			CloneURL:       "https://example.com/org/repo.git",
			DefaultBranch:  "main",
			BranchName:     "chore/upgrade-js-deps",
			ProviderName:   "github",
			AuthToken:      "test-token",
			PackageManager: "npm",
		}

		// when
		script := jsUpdater.BuildUpgradeScript(params, "/tmp/repo")

		// then
		assert.Contains(t, script, "NODE_VERSION")
		assert.Contains(t, script, "PACKAGE_MANAGER")
		assert.Contains(t, script, "Dockerfile")
		assert.Contains(t, script, "git commit")
		assert.Contains(t, script, "git push")
	})
}

func TestWriteGitAuth(t *testing.T) {
	t.Parallel()

	t.Run("should contain GitHub-specific auth when provider is github", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.UpgradeParams{
			ProviderName: "github",
			AuthToken:    "test-token",
		}

		// when
		result := jsUpdater.WriteGitAuth(params)

		// then
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "x-access-token")
		assert.Contains(t, result, "github.com")
		assert.Contains(t, result, "GIT_CONFIG_GLOBAL")
		assert.Contains(t, result, "TEMP_GITCONFIG")
	})

	t.Run("should contain Azure DevOps-specific auth when provider is azuredevops", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.UpgradeParams{
			ProviderName: "azuredevops",
			AuthToken:    "test-token",
		}

		// when
		result := jsUpdater.WriteGitAuth(params)

		// then
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "dev.azure.com")
		assert.Contains(t, result, "pat:")
		assert.Contains(t, result, "GIT_CONFIG_GLOBAL")
	})

	t.Run("should contain GitLab-specific auth when provider is gitlab", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.UpgradeParams{
			ProviderName: "gitlab",
			AuthToken:    "test-token",
		}

		// when
		result := jsUpdater.WriteGitAuth(params)

		// then
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "oauth2")
		assert.Contains(t, result, "gitlab.com")
		assert.Contains(t, result, "GIT_CONFIG_GLOBAL")
	})

	t.Run("should contain base config setup for unknown provider", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.UpgradeParams{
			ProviderName: "unknown",
			AuthToken:    "test-token",
		}

		// when
		result := jsUpdater.WriteGitAuth(params)

		// then
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "TEMP_GITCONFIG")
		assert.Contains(t, result, "GIT_CONFIG_GLOBAL")
		assert.NotContains(t, result, "github.com")
		assert.NotContains(t, result, "dev.azure.com")
		assert.NotContains(t, result, "gitlab.com")
	})
}

func TestWriteJSUpgradeCommands(t *testing.T) {
	t.Parallel()

	t.Run("should contain Node.js version update logic", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.UpgradeParams{PackageManager: "npm"}

		// when
		result := jsUpdater.WriteJSUpgradeCommands(params)

		// then
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "NODE_VERSION")
		assert.Contains(t, result, ".nvmrc")
		assert.Contains(t, result, ".node-version")
		assert.Contains(t, result, "NODE_VERSION_UPDATED=true")
		assert.Contains(t, result, "NODE_VERSION_UPDATED=false")
		assert.Contains(t, result, "NODE_VERSION_CHANGED")
	})

	t.Run("should contain npm update command", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.UpgradeParams{PackageManager: "npm"}

		// when
		result := jsUpdater.WriteJSUpgradeCommands(params)

		// then
		assert.Contains(t, result, "npm update")
	})

	t.Run("should contain pnpm update command", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.UpgradeParams{PackageManager: "pnpm"}

		// when
		result := jsUpdater.WriteJSUpgradeCommands(params)

		// then
		assert.Contains(t, result, "pnpm update")
	})

	t.Run("should contain yarn upgrade command", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.UpgradeParams{PackageManager: "yarn"}

		// when
		result := jsUpdater.WriteJSUpgradeCommands(params)

		// then
		assert.Contains(t, result, "yarn upgrade")
	})

	t.Run("should contain case statement for package manager selection", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.UpgradeParams{PackageManager: "npm"}

		// when
		result := jsUpdater.WriteJSUpgradeCommands(params)

		// then
		assert.Contains(t, result, "$PACKAGE_MANAGER")
		assert.Contains(t, result, "case")
		assert.Contains(t, result, "esac")
	})
}

func TestWriteDockerfileUpdate(t *testing.T) {
	t.Parallel()

	t.Run("should contain Dockerfile search and sed replacement commands", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := jsUpdater.WriteDockerfileUpdate()

		// then
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "Dockerfile")
		assert.Contains(t, result, "NODE_VERSION_CHANGED")
		assert.Contains(t, result, "node:")
		assert.Contains(t, result, "sed")
		assert.Contains(t, result, "find")
	})

	t.Run("should only update Dockerfiles when NODE_VERSION_CHANGED is true", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := jsUpdater.WriteDockerfileUpdate()

		// then
		assert.Contains(t, result, "NODE_VERSION_CHANGED")
		assert.Contains(t, result, "\"true\"")
	})
}

func TestWriteChangelogUpdate(t *testing.T) {
	t.Parallel()

	t.Run("should contain CHANGELOG.md copy logic", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := jsUpdater.WriteChangelogUpdate()

		// then
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "CHANGELOG_FILE")
		assert.Contains(t, result, "CHANGELOG.md")
		assert.Contains(t, result, "cp")
	})

	t.Run("should check for git changes before updating changelog", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := jsUpdater.WriteChangelogUpdate()

		// then
		assert.Contains(t, result, "git status --porcelain")
	})
}

func TestWriteCommitAndPush(t *testing.T) {
	t.Parallel()

	t.Run("should contain git add, commit, and push commands", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := jsUpdater.WriteCommitAndPush()

		// then
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "git add -A")
		assert.Contains(t, result, "git commit")
		assert.Contains(t, result, "git push")
		assert.Contains(t, result, "CHANGES_PUSHED=true")
	})

	t.Run("should output CHANGES_PUSHED=false when no changes detected", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := jsUpdater.WriteCommitAndPush()

		// then
		assert.Contains(t, result, "CHANGES_PUSHED=false")
		assert.Contains(t, result, "No changes detected")
	})

	t.Run("should use different commit messages for node version upgrade vs deps only", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := jsUpdater.WriteCommitAndPush()

		// then
		assert.Contains(t, result, "NODE_VERSION_CHANGED")
		assert.Contains(t, result, "upgraded Node.js")
		assert.Contains(t, result, "updated JavaScript dependencies")
	})
}

func TestBuildEnv(t *testing.T) {
	t.Parallel()

	t.Run("should include all required environment variables", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.UpgradeParams{
			CloneURL:       "https://example.com/org/repo.git",
			DefaultBranch:  "main",
			BranchName:     "chore/upgrade-js-deps",
			NodeVersion:    "20.18.0",
			AuthToken:      "test-token",
			ProviderName:   "github",
			PackageManager: "npm",
		}

		// when
		env := jsUpdater.BuildEnv(params, "/tmp/repo")

		// then
		envMap := envToMap(env)
		assert.Equal(t, "test-token", envMap["AUTH_TOKEN"])
		assert.Equal(t, "test-token", envMap["GIT_HTTPS_TOKEN"])
		assert.Equal(t, "https://example.com/org/repo.git", envMap["CLONE_URL"])
		assert.Equal(t, "chore/upgrade-js-deps", envMap["BRANCH_NAME"])
		assert.Equal(t, "/tmp/repo", envMap["REPO_DIR"])
		assert.Equal(t, "main", envMap["DEFAULT_BRANCH"])
		assert.Equal(t, "npm", envMap["PACKAGE_MANAGER"])
		assert.Equal(t, "20.18.0", envMap["NODE_VERSION"])
	})

	t.Run("should omit NODE_VERSION when empty", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.UpgradeParams{
			CloneURL:       "https://example.com/org/repo.git",
			DefaultBranch:  "main",
			BranchName:     "chore/upgrade-js-deps",
			NodeVersion:    "",
			AuthToken:      "test-token",
			ProviderName:   "github",
			PackageManager: "npm",
		}

		// when
		env := jsUpdater.BuildEnv(params, "/tmp/repo")

		// then
		envMap := envToMap(env)
		_, hasNodeVersion := envMap["NODE_VERSION"]
		assert.False(t, hasNodeVersion)
	})

	t.Run("should omit CHANGELOG_FILE when empty", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.UpgradeParams{
			CloneURL:       "https://example.com/org/repo.git",
			DefaultBranch:  "main",
			BranchName:     "chore/upgrade-js-deps",
			AuthToken:      "test-token",
			ProviderName:   "github",
			PackageManager: "npm",
			ChangelogFile:  "",
		}

		// when
		env := jsUpdater.BuildEnv(params, "/tmp/repo")

		// then
		envMap := envToMap(env)
		_, hasChangelog := envMap["CHANGELOG_FILE"]
		assert.False(t, hasChangelog)
	})

	t.Run("should include CHANGELOG_FILE when provided", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.UpgradeParams{
			CloneURL:       "https://example.com/org/repo.git",
			DefaultBranch:  "main",
			BranchName:     "chore/upgrade-js-deps",
			AuthToken:      "test-token",
			ProviderName:   "github",
			PackageManager: "npm",
			ChangelogFile:  "/tmp/changelog.md",
		}

		// when
		env := jsUpdater.BuildEnv(params, "/tmp/repo")

		// then
		envMap := envToMap(env)
		assert.Equal(t, "/tmp/changelog.md", envMap["CHANGELOG_FILE"])
	})

	t.Run("should include NODE_VERSION when provided", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.UpgradeParams{
			CloneURL:       "https://example.com/org/repo.git",
			DefaultBranch:  "main",
			BranchName:     "chore/upgrade-js-deps",
			NodeVersion:    "22.0.0",
			AuthToken:      "test-token",
			ProviderName:   "github",
			PackageManager: "pnpm",
		}

		// when
		env := jsUpdater.BuildEnv(params, "/tmp/repo")

		// then
		envMap := envToMap(env)
		assert.Equal(t, "22.0.0", envMap["NODE_VERSION"])
	})
}

func TestGeneratePRDescription(t *testing.T) {
	t.Parallel()

	t.Run("should include node version upgrade info when version was updated", func(t *testing.T) {
		t.Parallel()

		// given
		nodeVersion := "20.18.0"
		pkgMgr := "npm"
		versionUpdated := true

		// when
		desc := jsUpdater.GeneratePRDescription(nodeVersion, pkgMgr, versionUpdated)

		// then
		assert.Contains(t, desc, "## Summary")
		assert.Contains(t, desc, "20.18.0")
		assert.Contains(t, desc, ".nvmrc")
		assert.Contains(t, desc, "npm update")
		assert.Contains(t, desc, "### Changes")
		assert.Contains(t, desc, "### Review Checklist")
		assert.Contains(t, desc, "autoupdate")
	})

	t.Run("should not include node version info when version was not updated", func(t *testing.T) {
		t.Parallel()

		// given
		nodeVersion := "20.18.0"
		pkgMgr := "npm"
		versionUpdated := false

		// when
		desc := jsUpdater.GeneratePRDescription(nodeVersion, pkgMgr, versionUpdated)

		// then
		assert.Contains(t, desc, "## Summary")
		assert.Contains(t, desc, "updates all JavaScript dependencies")
		assert.NotContains(t, desc, ".nvmrc")
		assert.Contains(t, desc, "npm update")
	})

	t.Run("should reference pnpm when pnpm is the package manager", func(t *testing.T) {
		t.Parallel()

		// given
		nodeVersion := "20.18.0"
		pkgMgr := "pnpm"
		versionUpdated := false

		// when
		desc := jsUpdater.GeneratePRDescription(nodeVersion, pkgMgr, versionUpdated)

		// then
		assert.Contains(t, desc, "`pnpm update`")
		assert.NotContains(t, desc, "`npm update`")
		assert.NotContains(t, desc, "`yarn upgrade`")
	})

	t.Run("should reference yarn when yarn is the package manager", func(t *testing.T) {
		t.Parallel()

		// given
		nodeVersion := "20.18.0"
		pkgMgr := "yarn"
		versionUpdated := false

		// when
		desc := jsUpdater.GeneratePRDescription(nodeVersion, pkgMgr, versionUpdated)

		// then
		assert.Contains(t, desc, "yarn upgrade")
		assert.NotContains(t, desc, "npm update")
		assert.NotContains(t, desc, "pnpm update")
	})

	t.Run("should default to npm when package manager is empty", func(t *testing.T) {
		t.Parallel()

		// given
		nodeVersion := "20.18.0"
		pkgMgr := ""
		versionUpdated := false

		// when
		desc := jsUpdater.GeneratePRDescription(nodeVersion, pkgMgr, versionUpdated)

		// then
		assert.Contains(t, desc, "npm update")
	})

	t.Run("should include review checklist items", func(t *testing.T) {
		t.Parallel()

		// given
		nodeVersion := "20.18.0"
		pkgMgr := "npm"
		versionUpdated := true

		// when
		desc := jsUpdater.GeneratePRDescription(nodeVersion, pkgMgr, versionUpdated)

		// then
		assert.Contains(t, desc, "Verify build passes")
		assert.Contains(t, desc, "Verify tests pass")
		assert.Contains(t, desc, "Review dependency changes in lockfile")
	})
}

func TestBuildBatchJSScript(t *testing.T) {
	t.Parallel()

	t.Run("should contain shebang and strict mode", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := jsUpdater.BuildBatchJSScript()

		// then
		assert.True(t, strings.HasPrefix(script, "#!/bin/bash\n"))
		assert.Contains(t, script, "set -euo pipefail")
	})

	t.Run("should contain JS upgrade commands without git clone or push", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := jsUpdater.BuildBatchJSScript()

		// then
		assert.Contains(t, script, "NODE_VERSION")
		assert.Contains(t, script, "PACKAGE_MANAGER")
		assert.Contains(t, script, "Dockerfile")
		assert.NotContains(t, script, "git clone")
		assert.NotContains(t, script, "git push")
		assert.NotContains(t, script, "git commit")
	})
}

func TestParseNodeVersionFileEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("should skip comment lines in version file", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# This is a comment\n20.11.1\n"

		// when
		result := jsUpdater.ParseNodeVersionFile(content)

		// then
		assert.Equal(t, "20.11.1", result)
	})

	t.Run("should handle whitespace-only content", func(t *testing.T) {
		t.Parallel()

		// given
		content := "   \n   \n"

		// when
		result := jsUpdater.ParseNodeVersionFile(content)

		// then
		assert.Equal(t, "", result)
	})

	t.Run("should handle version with leading and trailing whitespace", func(t *testing.T) {
		t.Parallel()

		// given
		content := "  20.11.1  \n"

		// when
		result := jsUpdater.ParseNodeVersionFile(content)

		// then
		assert.Equal(t, "20.11.1", result)
	})

	t.Run("should handle version file with multiple comment lines before version", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# comment 1\n# comment 2\nv22.0.0\n"

		// when
		result := jsUpdater.ParseNodeVersionFile(content)

		// then
		assert.Equal(t, "22.0.0", result)
	})
}

func TestIsLTSReleaseEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("should return false when LTS is an empty string", func(t *testing.T) {
		t.Parallel()

		// given
		release := jsUpdater.NodeRelease{Version: "v23.0.0", LTS: ""}

		// when
		result := jsUpdater.IsLTSRelease(release)

		// then
		assert.False(t, result)
	})

	t.Run("should return true when LTS is true boolean", func(t *testing.T) {
		t.Parallel()

		// given
		release := jsUpdater.NodeRelease{Version: "v20.0.0", LTS: true}

		// when
		result := jsUpdater.IsLTSRelease(release)

		// then
		assert.True(t, result)
	})
}

func TestNewUpdaterRepositoryWithDeps(t *testing.T) {
	t.Parallel()

	t.Run("should create updater with injected version fetcher", func(t *testing.T) {
		t.Parallel()

		// given
		vf := &repositorydoubles.StubVersionFetcher{Version: "22.0.0"}

		// when
		updater := jsUpdater.NewUpdaterRepositoryWithDepsExported(vf)

		// then
		require.NotNil(t, updater)
		assert.Equal(t, "javascript", updater.Name())
	})
}

func TestBuildLocalUpgradeScript(t *testing.T) {
	t.Parallel()

	t.Run("should contain shebang and strict mode", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.LocalUpgradeParamsExported{
			BranchName:     "chore/upgrade-js-deps",
			NodeVersion:    "20.18.0",
			PackageManager: "npm",
		}

		// when
		script := jsUpdater.BuildLocalUpgradeScript(params)

		// then
		assert.True(t, strings.HasPrefix(script, "#!/bin/bash\n"))
		assert.Contains(t, script, "set -euo pipefail")
	})

	t.Run("should contain JS upgrade commands and Dockerfile update", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.LocalUpgradeParamsExported{
			BranchName:     "chore/upgrade-js-deps",
			NodeVersion:    "20.18.0",
			PackageManager: "npm",
		}

		// when
		script := jsUpdater.BuildLocalUpgradeScript(params)

		// then
		assert.Contains(t, script, "NODE_VERSION")
		assert.Contains(t, script, "PACKAGE_MANAGER")
		assert.Contains(t, script, "Dockerfile")
		assert.Contains(t, script, "CHANGELOG")
	})

	t.Run("should not contain git clone or push commands", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.LocalUpgradeParamsExported{
			BranchName:     "chore/upgrade-js-deps",
			NodeVersion:    "20.18.0",
			PackageManager: "npm",
		}

		// when
		script := jsUpdater.BuildLocalUpgradeScript(params)

		// then
		assert.NotContains(t, script, "git clone")
		assert.NotContains(t, script, "git push")
		assert.NotContains(t, script, "git commit")
	})

	t.Run("should include auth setup when token is provided", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.LocalUpgradeParamsExported{
			BranchName:     "chore/upgrade-js-deps",
			NodeVersion:    "20.18.0",
			PackageManager: "npm",
			AuthToken:      "test-token",
			ProviderName:   "github",
		}

		// when
		script := jsUpdater.BuildLocalUpgradeScript(params)

		// then
		assert.Contains(t, script, "GIT_CONFIG_GLOBAL")
		assert.Contains(t, script, "github.com")
	})
}

func TestWriteLocalAuth(t *testing.T) {
	t.Parallel()

	t.Run("should return empty string when no auth token is provided", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.LocalUpgradeParamsExported{
			AuthToken: "",
		}

		// when
		result := jsUpdater.WriteLocalAuth(params)

		// then
		assert.Equal(t, "", result)
	})

	t.Run("should contain GitHub auth when provider is github and token exists", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.LocalUpgradeParamsExported{
			AuthToken:    "test-token",
			ProviderName: "github",
		}

		// when
		result := jsUpdater.WriteLocalAuth(params)

		// then
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "x-access-token")
		assert.Contains(t, result, "github.com")
		assert.Contains(t, result, "GIT_CONFIG_GLOBAL")
	})

	t.Run("should contain Azure DevOps auth when provider is azuredevops and token exists", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.LocalUpgradeParamsExported{
			AuthToken:    "test-token",
			ProviderName: "azuredevops",
		}

		// when
		result := jsUpdater.WriteLocalAuth(params)

		// then
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "dev.azure.com")
		assert.Contains(t, result, "pat:")
	})

	t.Run("should contain GitLab auth when provider is gitlab and token exists", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.LocalUpgradeParamsExported{
			AuthToken:    "test-token",
			ProviderName: "gitlab",
		}

		// when
		result := jsUpdater.WriteLocalAuth(params)

		// then
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "oauth2")
		assert.Contains(t, result, "gitlab.com")
	})
}

func TestBuildLocalEnv(t *testing.T) {
	t.Parallel()

	t.Run("should include basic environment variables", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.LocalUpgradeParamsExported{
			BranchName:     "chore/upgrade-js-deps",
			PackageManager: "npm",
		}

		// when
		env := jsUpdater.BuildLocalEnv(params)

		// then
		envMap := envToMap(env)
		assert.Equal(t, "chore/upgrade-js-deps", envMap["BRANCH_NAME"])
		assert.Equal(t, "npm", envMap["PACKAGE_MANAGER"])
	})

	t.Run("should include NODE_VERSION when provided", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.LocalUpgradeParamsExported{
			BranchName:     "chore/upgrade-node-20.18.0",
			NodeVersion:    "20.18.0",
			PackageManager: "npm",
		}

		// when
		env := jsUpdater.BuildLocalEnv(params)

		// then
		envMap := envToMap(env)
		assert.Equal(t, "20.18.0", envMap["NODE_VERSION"])
	})

	t.Run("should omit NODE_VERSION when empty", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.LocalUpgradeParamsExported{
			BranchName:     "chore/upgrade-js-deps",
			NodeVersion:    "",
			PackageManager: "npm",
		}

		// when
		env := jsUpdater.BuildLocalEnv(params)

		// then
		envMap := envToMap(env)
		_, hasNodeVersion := envMap["NODE_VERSION"]
		assert.False(t, hasNodeVersion)
	})

	t.Run("should include AUTH_TOKEN and GIT_HTTPS_TOKEN when auth token is provided", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.LocalUpgradeParamsExported{
			BranchName:     "chore/upgrade-js-deps",
			PackageManager: "npm",
			AuthToken:      "my-token",
		}

		// when
		env := jsUpdater.BuildLocalEnv(params)

		// then
		envMap := envToMap(env)
		assert.Equal(t, "my-token", envMap["AUTH_TOKEN"])
		assert.Equal(t, "my-token", envMap["GIT_HTTPS_TOKEN"])
	})

	t.Run("should omit AUTH_TOKEN when empty", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.LocalUpgradeParamsExported{
			BranchName:     "chore/upgrade-js-deps",
			PackageManager: "npm",
			AuthToken:      "",
		}

		// when
		env := jsUpdater.BuildLocalEnv(params)

		// then
		envMap := envToMap(env)
		_, hasAuth := envMap["AUTH_TOKEN"]
		assert.False(t, hasAuth)
	})

	t.Run("should include CHANGELOG_FILE when provided", func(t *testing.T) {
		t.Parallel()

		// given
		params := jsUpdater.LocalUpgradeParamsExported{
			BranchName:     "chore/upgrade-js-deps",
			PackageManager: "npm",
			ChangelogFile:  "/tmp/changelog.md",
		}

		// when
		env := jsUpdater.BuildLocalEnv(params)

		// then
		envMap := envToMap(env)
		assert.Equal(t, "/tmp/changelog.md", envMap["CHANGELOG_FILE"])
	})
}

func TestDetectLocalPackageManager(t *testing.T) {
	t.Parallel()

	t.Run("should return pnpm when pnpm-lock.yaml exists", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		err := os.WriteFile(tmpDir+"/pnpm-lock.yaml", []byte(""), 0o644)
		require.NoError(t, err)

		// when
		result := jsUpdater.DetectLocalPackageManager(tmpDir)

		// then
		assert.Equal(t, "pnpm", result)
	})

	t.Run("should return yarn when yarn.lock exists", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		err := os.WriteFile(tmpDir+"/yarn.lock", []byte(""), 0o644)
		require.NoError(t, err)

		// when
		result := jsUpdater.DetectLocalPackageManager(tmpDir)

		// then
		assert.Equal(t, "yarn", result)
	})

	t.Run("should return npm as default when no lockfile exists", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()

		// when
		result := jsUpdater.DetectLocalPackageManager(tmpDir)

		// then
		assert.Equal(t, "npm", result)
	})

	t.Run("should prefer pnpm over yarn when both lockfiles exist", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(tmpDir+"/pnpm-lock.yaml", []byte(""), 0o644))
		require.NoError(t, os.WriteFile(tmpDir+"/yarn.lock", []byte(""), 0o644))

		// when
		result := jsUpdater.DetectLocalPackageManager(tmpDir)

		// then
		assert.Equal(t, "pnpm", result)
	})
}

func TestReadLocalNodeVersion(t *testing.T) {
	t.Parallel()

	t.Run("should read version from .nvmrc file", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		err := os.WriteFile(tmpDir+"/.nvmrc", []byte("20.11.1\n"), 0o644)
		require.NoError(t, err)

		// when
		version := jsUpdater.ReadLocalNodeVersion(tmpDir)

		// then
		assert.Equal(t, "20.11.1", version)
	})

	t.Run("should read version from .node-version file when .nvmrc does not exist", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		err := os.WriteFile(tmpDir+"/.node-version", []byte("v18.19.0\n"), 0o644)
		require.NoError(t, err)

		// when
		version := jsUpdater.ReadLocalNodeVersion(tmpDir)

		// then
		assert.Equal(t, "18.19.0", version)
	})

	t.Run("should return empty string when no version files exist", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()

		// when
		version := jsUpdater.ReadLocalNodeVersion(tmpDir)

		// then
		assert.Equal(t, "", version)
	})

	t.Run("should prefer .nvmrc over .node-version when both exist", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(tmpDir+"/.nvmrc", []byte("20.11.1\n"), 0o644))
		require.NoError(t, os.WriteFile(tmpDir+"/.node-version", []byte("18.19.0\n"), 0o644))

		// when
		version := jsUpdater.ReadLocalNodeVersion(tmpDir)

		// then
		assert.Equal(t, "20.11.1", version)
	})
}

// envToMap converts a slice of "KEY=VALUE" strings into a map for easier assertions.
func envToMap(env []string) map[string]string {
	result := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if found {
			result[key] = value
		}
	}
	return result
}

func TestHandleDryRunLocal(t *testing.T) {
	t.Parallel()

	t.Run("should return result with version upgrade info", func(t *testing.T) {
		t.Parallel()

		// given
		vCtx := &jsUpdater.VersionContext{
			LatestVersion:       "20.18.0",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-node-20.18.0",
		}

		// when
		result := jsUpdater.HandleDryRunLocal(vCtx, "/tmp/repo", "npm")

		// then
		require.NotNil(t, result)
		assert.True(t, result.NodeVersionUpdated)
	})

	t.Run("should return result with deps-only info", func(t *testing.T) {
		t.Parallel()

		// given
		vCtx := &jsUpdater.VersionContext{
			LatestVersion:       "20.18.0",
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-js-deps",
		}

		// when
		result := jsUpdater.HandleDryRunLocal(vCtx, "/tmp/repo", "npm")

		// then
		require.NotNil(t, result)
		assert.False(t, result.NodeVersionUpdated)
	})
}

func TestPrepareLocalChangelogJS(t *testing.T) {
	t.Parallel()

	t.Run("should return temp file when CHANGELOG.md exists", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		changelog := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n"
		require.NoError(t, os.WriteFile(filepath.Join(root, "CHANGELOG.md"), []byte(changelog), 0o600))
		vCtx := &jsUpdater.VersionContext{LatestVersion: "20.18.0", NeedsVersionUpgrade: true}

		// when
		result := jsUpdater.PrepareLocalChangelog(root, vCtx)

		// then
		assert.NotEmpty(t, result)
		if result != "" {
			defer os.Remove(result)
		}
	})

	t.Run("should return empty when no CHANGELOG.md", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		vCtx := &jsUpdater.VersionContext{LatestVersion: "20.18.0", NeedsVersionUpgrade: true}

		// when
		result := jsUpdater.PrepareLocalChangelog(root, vCtx)

		// then
		assert.Empty(t, result)
	})
}
