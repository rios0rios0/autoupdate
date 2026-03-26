//go:build unit

package golang_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	goUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestName(t *testing.T) {
	t.Parallel()

	t.Run("should return golang as updater name", func(t *testing.T) {
		t.Parallel()

		// given
		updater := goUpdater.NewUpdaterRepository()

		// when
		name := updater.Name()

		// then
		assert.Equal(t, "golang", name)
	})
}

func TestDetect(t *testing.T) {
	t.Parallel()

	t.Run("should return true when go.mod exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"go.mod": true}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := goUpdater.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.True(t, detected)
	})

	t.Run("should return false when no Go files exist", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := goUpdater.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.False(t, detected)
	})
}

func TestParseGoDirective(t *testing.T) {
	t.Parallel()

	t.Run("should extract version from standard go directive", func(t *testing.T) {
		t.Parallel()

		// given
		content := "module example.com/foo\n\ngo 1.25.7\n\nrequire (\n)"

		// when
		result := goUpdater.ParseGoDirective(content)

		// then
		assert.Equal(t, "1.25.7", result)
	})

	t.Run("should extract two-part version", func(t *testing.T) {
		t.Parallel()

		// given
		content := "module example.com/foo\n\ngo 1.25\n"

		// when
		result := goUpdater.ParseGoDirective(content)

		// then
		assert.Equal(t, "1.25", result)
	})

	t.Run("should return empty when no go directive found", func(t *testing.T) {
		t.Parallel()

		// given
		content := "module example.com/foo\n"

		// when
		result := goUpdater.ParseGoDirective(content)

		// then
		assert.Equal(t, "", result)
	})

	t.Run("should handle go directive with toolchain line", func(t *testing.T) {
		t.Parallel()

		// given
		content := "module example.com/foo\n\ngo 1.25.7\ntoolchain go1.25.7\n"

		// when
		result := goUpdater.ParseGoDirective(content)

		// then
		assert.Equal(t, "1.25.7", result)
	})
}

func TestResolveVersionContext(t *testing.T) {
	t.Parallel()

	t.Run("should detect version upgrade needed", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"go.mod": true}).
			WithFileContents(map[string]string{
				"go.mod": "module example.com/foo\n\ngo 1.24\n",
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := goUpdater.ResolveVersionContext(t.Context(), provider, repo, "1.25.7")

		// then
		require.NotNil(t, vCtx)
		assert.Equal(t, "1.25.7", vCtx.LatestVersion)
		assert.True(t, vCtx.NeedsVersionUpgrade)
	})

	t.Run("should detect deps-only upgrade when version is current", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"go.mod": true}).
			WithFileContents(map[string]string{
				"go.mod": "module example.com/foo\n\ngo 1.25.7\n",
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}

		// when
		vCtx := goUpdater.ResolveVersionContext(t.Context(), provider, repo, "1.25.7")

		// then
		require.NotNil(t, vCtx)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Contains(t, vCtx.BranchName, "deps")
	})
}

func TestBuildLocalGoScript(t *testing.T) {
	t.Parallel()

	t.Run("should contain Go upgrade commands", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := goUpdater.BuildLocalGoScript("github", false)

		// then
		assert.Contains(t, script, "#!/bin/bash")
		assert.Contains(t, script, "go mod tidy")
	})

	t.Run("should include config.sh sourcing when available", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := goUpdater.BuildLocalGoScript("github", true)

		// then
		assert.Contains(t, script, "config.sh")
	})
}

func TestGenerateGoPRDescription(t *testing.T) {
	t.Parallel()

	t.Run("should include version update info", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := goUpdater.GenerateGoPRDescription("1.25.7", false, true)

		// then
		assert.Contains(t, result, "1.25.7")
	})

	t.Run("should describe deps-only update", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := goUpdater.GenerateGoPRDescription("1.25.7", false, false)

		// then
		assert.Contains(t, result, "dependencies")
	})
}

func TestCreateUpdatePRs(t *testing.T) {
	t.Parallel()

	t.Run("should skip when PR already exists for branch", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"go.mod": true}).
			WithFileContents(map[string]string{
				"go.mod": "module example.com/foo\n\ngo 1.24\n",
			}).
			WithPRExistsResult(true).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		fetcher := &repositorydoubles.StubVersionFetcher{Version: "1.25.7"}
		updater := goUpdater.NewUpdaterRepositoryWithDeps(fetcher)

		// when
		prs, err := updater.CreateUpdatePRs(t.Context(), provider, repo, entities.UpdateOptions{})

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
	})
}

func TestLocalResolveVersionContext(t *testing.T) {
	t.Parallel()

	t.Run("should detect upgrade needed when versions differ", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(root, "go.mod"),
			[]byte("module example.com/foo\n\ngo 1.24\n"),
			0o600,
		))

		// when
		vCtx := goUpdater.LocalResolveVersionContext(root, "1.25.7")

		// then
		require.NotNil(t, vCtx)
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "1.25.7", vCtx.LatestVersion)
		assert.Contains(t, vCtx.BranchName, "1.25.7")
	})

	t.Run("should detect deps-only when version is current", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(root, "go.mod"),
			[]byte("module example.com/foo\n\ngo 1.25.7\n"),
			0o600,
		))

		// when
		vCtx := goUpdater.LocalResolveVersionContext(root, "1.25.7")

		// then
		require.NotNil(t, vCtx)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Contains(t, vCtx.BranchName, "deps")
	})

	t.Run("should assume upgrade when go.mod is missing", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()

		// when
		vCtx := goUpdater.LocalResolveVersionContext(root, "1.25.7")

		// then
		require.NotNil(t, vCtx)
		assert.True(t, vCtx.NeedsVersionUpgrade)
	})
}

func TestWriteAzureDevOpsAuth(t *testing.T) {
	t.Parallel()

	t.Run("should write Azure DevOps git config entries", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder

		// when
		goUpdater.WriteAzureDevOpsAuth(&sb)

		// then
		result := sb.String()
		assert.Contains(t, result, "dev.azure.com")
		assert.Contains(t, result, "AUTH_TOKEN")
	})
}

func TestWriteGitLabAuth(t *testing.T) {
	t.Parallel()

	t.Run("should write GitLab git config entries", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder

		// when
		goUpdater.WriteGitLabAuth(&sb)

		// then
		result := sb.String()
		assert.Contains(t, result, "gitlab.com")
		assert.Contains(t, result, "oauth2")
	})
}

func TestBuildUpgradeScript(t *testing.T) {
	t.Parallel()

	t.Run("should produce valid bash script with GitHub auth", func(t *testing.T) {
		t.Parallel()

		// given
		params := goUpdater.UpgradeParams{
			CloneURL:      "https://github.com/org/repo.git",
			DefaultBranch: "main",
			BranchName:    "chore/upgrade-go-1.25.7",
			GoVersion:     "1.25.7",
			AuthToken:     "test-token",
			ProviderName:  "github",
		}

		// when
		script := goUpdater.BuildUpgradeScript(params, "/tmp/repo", "/usr/local/go/bin/go")

		// then
		assert.Contains(t, script, "#!/bin/bash")
		assert.Contains(t, script, "set -euo pipefail")
		assert.Contains(t, script, "github.com")
		assert.Contains(t, script, "go mod tidy")
	})

	t.Run("should include Azure DevOps auth section", func(t *testing.T) {
		t.Parallel()

		// given
		params := goUpdater.UpgradeParams{
			ProviderName: "azuredevops",
			AuthToken:    "token",
		}

		// when
		script := goUpdater.BuildUpgradeScript(params, "/tmp/repo", "/usr/local/go/bin/go")

		// then
		assert.Contains(t, script, "dev.azure.com")
	})

	t.Run("should include GitLab auth section", func(t *testing.T) {
		t.Parallel()

		// given
		params := goUpdater.UpgradeParams{
			ProviderName: "gitlab",
			AuthToken:    "token",
		}

		// when
		script := goUpdater.BuildUpgradeScript(params, "/tmp/repo", "/usr/local/go/bin/go")

		// then
		assert.Contains(t, script, "gitlab.com")
	})

	t.Run("should include changelog update when file provided", func(t *testing.T) {
		t.Parallel()

		// given
		params := goUpdater.UpgradeParams{
			ProviderName:  "github",
			ChangelogFile: "/tmp/changelog.md",
		}

		// when
		script := goUpdater.BuildUpgradeScript(params, "/tmp/repo", "/usr/local/go/bin/go")

		// then
		assert.Contains(t, script, "CHANGELOG")
	})

	t.Run("should include config.sh sourcing when flag set", func(t *testing.T) {
		t.Parallel()

		// given
		params := goUpdater.UpgradeParams{
			ProviderName: "github",
			HasConfigSH:  true,
		}

		// when
		script := goUpdater.BuildUpgradeScript(params, "/tmp/repo", "/usr/local/go/bin/go")

		// then
		assert.Contains(t, script, "config.sh")
	})
}

func TestBuildEnv(t *testing.T) {
	t.Parallel()

	t.Run("should include all required environment variables", func(t *testing.T) {
		t.Parallel()

		// given
		params := goUpdater.UpgradeParams{
			CloneURL:      "https://github.com/org/repo.git",
			DefaultBranch: "main",
			BranchName:    "chore/upgrade-go-1.25.7",
			GoVersion:     "1.25.7",
			AuthToken:     "test-token",
		}

		// when
		env := goUpdater.BuildEnv(params, "/tmp/repo", "/usr/local/go/bin/go")

		// then
		assert.NotEmpty(t, env)
		found := false
		for _, e := range env {
			if strings.HasPrefix(e, "GO_VERSION=") {
				found = true
				assert.Equal(t, "GO_VERSION=1.25.7", e)
			}
		}
		assert.True(t, found, "GO_VERSION should be in env")
	})
}

func TestFileExistsLocally(t *testing.T) {
	t.Parallel()

	t.Run("should return true for existing file", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		path := filepath.Join(root, "test.txt")
		require.NoError(t, os.WriteFile(path, []byte("test"), 0o600))

		// when
		result := goUpdater.FileExistsLocally(path)

		// then
		assert.True(t, result)
	})

	t.Run("should return false for non-existent file", func(t *testing.T) {
		t.Parallel()

		// given
		path := "/tmp/non-existent-file-12345"

		// when
		result := goUpdater.FileExistsLocally(path)

		// then
		assert.False(t, result)
	})
}

func TestOpenPullRequest(t *testing.T) {
	t.Parallel()

	t.Run("should create PR with correct parameters", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithCreatedPR(&entities.PullRequest{ID: 42, URL: "https://example.com/pr/42"}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}
		vCtx := &goUpdater.VersionContext{
			LatestVersion:       "1.25.7",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-go-1.25.7",
		}
		result := &goUpdater.UpgradeResult{
			HasChanges:      true,
			GoVersionUpdated: true,
			Output:          "",
		}

		// when
		prs, err := goUpdater.OpenPullRequest(t.Context(), provider, repo, opts, vCtx, result, false)

		// then
		require.NoError(t, err)
		require.Len(t, prs, 1)
		assert.Equal(t, 42, prs[0].ID)
		assert.NotEmpty(t, provider.PRInputs)
	})

	t.Run("should use target branch from options", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithCreatedPR(&entities.PullRequest{ID: 1}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{TargetBranch: "develop"}
		vCtx := &goUpdater.VersionContext{
			LatestVersion:       "1.25.7",
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-go-deps",
		}
		result := &goUpdater.UpgradeResult{HasChanges: true}

		// when
		prs, err := goUpdater.OpenPullRequest(t.Context(), provider, repo, opts, vCtx, result, false)

		// then
		require.NoError(t, err)
		require.Len(t, prs, 1)
		assert.Contains(t, provider.PRInputs[0].TargetBranch, "develop")
	})
}

func TestPrepareChangelog(t *testing.T) {
	t.Parallel()

	t.Run("should return temp file path when CHANGELOG.md exists", func(t *testing.T) {
		t.Parallel()

		// given
		changelog := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n"
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": changelog}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		vCtx := &goUpdater.VersionContext{LatestVersion: "1.25.7", NeedsVersionUpgrade: true}

		// when
		result := goUpdater.PrepareChangelog(t.Context(), provider, repo, vCtx)

		// then
		assert.NotEmpty(t, result)
		if result != "" {
			defer os.Remove(result)
		}
	})

	t.Run("should return empty string when no CHANGELOG.md", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		vCtx := &goUpdater.VersionContext{LatestVersion: "1.25.7", NeedsVersionUpgrade: true}

		// when
		result := goUpdater.PrepareChangelog(t.Context(), provider, repo, vCtx)

		// then
		assert.Empty(t, result)
	})
}

func TestHandleDryRun(t *testing.T) {
	t.Parallel()

	t.Run("should return result with version upgrade info", func(t *testing.T) {
		t.Parallel()

		// given
		vCtx := &goUpdater.VersionContext{
			LatestVersion:       "1.25.7",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-go-1.25.7",
		}

		// when
		result := goUpdater.HandleDryRun(vCtx, "/tmp/repo")

		// then
		require.NotNil(t, result)
		assert.Equal(t, "1.25.7", result.LatestVersion)
		assert.True(t, result.GoVersionUpdated)
		assert.Equal(t, "chore/upgrade-go-1.25.7", result.BranchName)
	})

	t.Run("should return result with deps-only info", func(t *testing.T) {
		t.Parallel()

		// given
		vCtx := &goUpdater.VersionContext{
			LatestVersion:       "1.25.7",
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-go-deps",
		}

		// when
		result := goUpdater.HandleDryRun(vCtx, "/tmp/repo")

		// then
		require.NotNil(t, result)
		assert.False(t, result.GoVersionUpdated)
	})
}

func TestBuildLocalUpgradeScriptFull(t *testing.T) {
	t.Parallel()

	t.Run("should contain bash shebang and go commands", func(t *testing.T) {
		t.Parallel()

		// given
		params := goUpdater.LocalUpgradeParamsType{
			BranchName:   "chore/upgrade-go-1.25.7",
			GoVersion:    "1.25.7",
			AuthToken:    "token",
			ProviderName: "github",
		}

		// when
		script := goUpdater.BuildLocalUpgradeScriptFull(params)

		// then
		assert.Contains(t, script, "#!/bin/bash")
		assert.Contains(t, script, "go mod tidy")
	})

	t.Run("should include config.sh sourcing when set", func(t *testing.T) {
		t.Parallel()

		// given
		params := goUpdater.LocalUpgradeParamsType{
			ProviderName: "github",
			HasConfigSH:  true,
		}

		// when
		script := goUpdater.BuildLocalUpgradeScriptFull(params)

		// then
		assert.Contains(t, script, "config.sh")
	})
}

func TestWriteLocalAuth(t *testing.T) {
	t.Parallel()

	t.Run("should write GitHub auth when token provided", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := goUpdater.LocalUpgradeParamsType{
			AuthToken:    "ghp_token",
			ProviderName: "github",
		}

		// when
		goUpdater.WriteLocalAuth(&sb, params)

		// then
		assert.Contains(t, sb.String(), "github.com")
	})

	t.Run("should write Azure DevOps auth when token provided", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := goUpdater.LocalUpgradeParamsType{
			AuthToken:    "token",
			ProviderName: "azuredevops",
		}

		// when
		goUpdater.WriteLocalAuth(&sb, params)

		// then
		assert.Contains(t, sb.String(), "dev.azure.com")
	})

	t.Run("should skip auth when no token", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		params := goUpdater.LocalUpgradeParamsType{
			ProviderName: "github",
		}

		// when
		goUpdater.WriteLocalAuth(&sb, params)

		// then
		assert.Empty(t, sb.String())
	})
}

func TestBuildLocalEnvFull(t *testing.T) {
	t.Parallel()

	t.Run("should include required environment variables", func(t *testing.T) {
		t.Parallel()

		// given
		params := goUpdater.LocalUpgradeParamsType{
			GoVersion: "1.25.7",
			AuthToken: "token",
		}

		// when
		env := goUpdater.BuildLocalEnvFull(params, "/usr/local/go/bin/go")

		// then
		assert.NotEmpty(t, env)
		found := false
		for _, e := range env {
			if strings.HasPrefix(e, "GO_VERSION=") {
				found = true
			}
		}
		assert.True(t, found)
	})
}

func TestPrepareLocalChangelogGo(t *testing.T) {
	t.Parallel()

	t.Run("should return temp file when CHANGELOG.md exists", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		changelog := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n"
		require.NoError(t, os.WriteFile(filepath.Join(root, "CHANGELOG.md"), []byte(changelog), 0o600))
		vCtx := &goUpdater.VersionContext{LatestVersion: "1.25.7", NeedsVersionUpgrade: true}

		// when
		result := goUpdater.PrepareLocalChangelog(root, vCtx)

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
		vCtx := &goUpdater.VersionContext{LatestVersion: "1.25.7", NeedsVersionUpgrade: true}

		// when
		result := goUpdater.PrepareLocalChangelog(root, vCtx)

		// then
		assert.Empty(t, result)
	})
}

