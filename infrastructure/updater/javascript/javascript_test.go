package javascript //nolint:testpackage // tests unexported functions

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

func TestJavaScriptUpdater_Name(t *testing.T) {
	t.Parallel()

	t.Run("should return javascript", func(t *testing.T) {
		t.Parallel()

		// given
		u := New()

		// when
		name := u.Name()

		// then
		assert.Equal(t, "javascript", name)
	})
}

func TestJavaScriptUpdater_Detect(t *testing.T) {
	t.Parallel()

	t.Run("should detect repository with package.json", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{"package.json": true},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}
		u := New()

		// when
		result := u.Detect(ctx, provider, repo)

		// then
		assert.True(t, result)
	})

	t.Run("should not detect repository without package.json", func(t *testing.T) {
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

func TestParseNodeVersionFile(t *testing.T) {
	t.Parallel()

	t.Run("should extract version from .nvmrc content", func(t *testing.T) {
		t.Parallel()

		// given
		content := "22.12.0\n"

		// when
		version := parseNodeVersionFile(content)

		// then
		assert.Equal(t, "22.12.0", version)
	})

	t.Run("should strip v prefix", func(t *testing.T) {
		t.Parallel()

		// given
		content := "v22.12.0\n"

		// when
		version := parseNodeVersionFile(content)

		// then
		assert.Equal(t, "22.12.0", version)
	})

	t.Run("should handle version with leading/trailing whitespace", func(t *testing.T) {
		t.Parallel()

		// given
		content := "  22.12.0  \n"

		// when
		version := parseNodeVersionFile(content)

		// then
		assert.Equal(t, "22.12.0", version)
	})

	t.Run("should skip comment lines", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Node.js version\n22.12.0\n"

		// when
		version := parseNodeVersionFile(content)

		// then
		assert.Equal(t, "22.12.0", version)
	})

	t.Run("should return empty string for empty file", func(t *testing.T) {
		t.Parallel()

		// given
		content := "\n"

		// when
		version := parseNodeVersionFile(content)

		// then
		assert.Empty(t, version)
	})
}

func TestIsLTSRelease(t *testing.T) {
	t.Parallel()

	t.Run("should return true when LTS is a string codename", func(t *testing.T) {
		t.Parallel()

		// given
		release := nodeRelease{Version: "v22.12.0", LTS: "Jod"}

		// when
		result := isLTSRelease(release)

		// then
		assert.True(t, result)
	})

	t.Run("should return false when LTS is false", func(t *testing.T) {
		t.Parallel()

		// given
		release := nodeRelease{Version: "v23.3.0", LTS: false}

		// when
		result := isLTSRelease(release)

		// then
		assert.False(t, result)
	})

	t.Run("should return false when LTS is empty string", func(t *testing.T) {
		t.Parallel()

		// given
		release := nodeRelease{Version: "v23.3.0", LTS: ""}

		// when
		result := isLTSRelease(release)

		// then
		assert.False(t, result)
	})
}

func TestDetectPackageManager(t *testing.T) {
	t.Parallel()

	t.Run("should detect pnpm from pnpm-lock.yaml", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{
				"package.json":   true,
				"pnpm-lock.yaml": true,
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		result := detectPackageManager(ctx, provider, repo)

		// then
		assert.Equal(t, "pnpm", result)
	})

	t.Run("should detect yarn from yarn.lock", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{
				"package.json": true,
				"yarn.lock":    true,
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		result := detectPackageManager(ctx, provider, repo)

		// then
		assert.Equal(t, "yarn", result)
	})

	t.Run("should default to npm when no lockfile found", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{
				"package.json": true,
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		result := detectPackageManager(ctx, provider, repo)

		// then
		assert.Equal(t, "npm", result)
	})

	t.Run("should prefer pnpm over yarn when both lockfiles exist", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{
				"package.json":   true,
				"pnpm-lock.yaml": true,
				"yarn.lock":      true,
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		result := detectPackageManager(ctx, provider, repo)

		// then
		assert.Equal(t, "pnpm", result)
	})
}

func TestResolveVersionContext(t *testing.T) {
	t.Parallel()

	t.Run("should choose version-upgrade branch when .nvmrc is older", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{".nvmrc": true},
			FileContents: map[string]string{
				".nvmrc": "20.10.0\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := resolveVersionContext(ctx, provider, repo, "22.12.0")

		// then
		assert.Equal(t, "22.12.0", vCtx.LatestVersion)
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-node-22.12.0", vCtx.BranchName)
	})

	t.Run("should choose deps-only branch when .nvmrc matches latest", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{".nvmrc": true},
			FileContents: map[string]string{
				".nvmrc": "22.12.0\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := resolveVersionContext(ctx, provider, repo, "22.12.0")

		// then
		assert.Equal(t, "22.12.0", vCtx.LatestVersion)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-js-deps", vCtx.BranchName)
	})

	t.Run("should detect .node-version when .nvmrc is absent", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{".node-version": true},
			FileContents: map[string]string{
				".node-version": "20.10.0\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := resolveVersionContext(ctx, provider, repo, "22.12.0")

		// then
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-node-22.12.0", vCtx.BranchName)
	})

	t.Run("should choose deps-only branch when no version file exists", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := resolveVersionContext(ctx, provider, repo, "22.12.0")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-js-deps", vCtx.BranchName)
	})

	t.Run("should choose deps-only branch when latest version is empty", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{".nvmrc": true},
			FileContents: map[string]string{
				".nvmrc": "20.10.0\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := resolveVersionContext(ctx, provider, repo, "")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-js-deps", vCtx.BranchName)
	})

	t.Run("should strip v prefix when comparing versions", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{".nvmrc": true},
			FileContents: map[string]string{
				".nvmrc": "v22.12.0\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := resolveVersionContext(ctx, provider, repo, "22.12.0")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-js-deps", vCtx.BranchName)
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
			LatestVersion:       "22.12.0",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-node-22.12.0",
		}

		// when
		path := prepareChangelog(ctx, provider, repo, vCtx)

		// then
		assert.NotEmpty(t, path)
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "### Changed")
		assert.Contains(t, string(content), "- changed the Node.js version to `22.12.0`")
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
			BranchName:          "chore/upgrade-js-deps",
		}

		// when
		path := prepareChangelog(ctx, provider, repo, vCtx)

		// then
		assert.NotEmpty(t, path)
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "- changed the JavaScript dependencies to their latest versions")
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
			LatestVersion:       "22.12.0",
			NeedsVersionUpgrade: true,
		}

		// when
		path := prepareChangelog(ctx, provider, repo, vCtx)

		// then
		assert.Empty(t, path)
	})
}

func TestBuildUpgradeScript(t *testing.T) {
	t.Parallel()

	t.Run("should include clone, checkout, and npm update commands", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			CloneURL:       "https://github.com/org/repo.git",
			DefaultBranch:  "main",
			BranchName:     "chore/upgrade-node-22.12.0",
			NodeVersion:    "22.12.0",
			AuthToken:      "token",
			ProviderName:   "github",
			PackageManager: "npm",
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo")

		// then
		assert.Contains(t, script, "#!/bin/bash")
		assert.Contains(t, script, "set -euo pipefail")
		assert.Contains(t, script, "git clone")
		assert.Contains(t, script, "git checkout -b")
		assert.Contains(t, script, "npm update")
		assert.Contains(t, script, "NODE_VERSION_UPDATED=true")
		assert.Contains(t, script, "NODE_VERSION_UPDATED=false")
		assert.Contains(t, script, "CHANGES_PUSHED=true")
		assert.Contains(t, script, "CHANGES_PUSHED=false")
	})

	t.Run("should include yarn upgrade for yarn projects", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			ProviderName:   "github",
			PackageManager: "yarn",
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo")

		// then
		assert.Contains(t, script, "yarn upgrade")
	})

	t.Run("should include pnpm update for pnpm projects", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			ProviderName:   "github",
			PackageManager: "pnpm",
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo")

		// then
		assert.Contains(t, script, "pnpm update")
	})

	t.Run("should configure GitHub git auth", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			ProviderName:   "github",
			PackageManager: "npm",
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
			ProviderName:   "azuredevops",
			PackageManager: "npm",
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
			ProviderName:   "gitlab",
			PackageManager: "npm",
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo")

		// then
		assert.Contains(t, script, "oauth2")
		assert.Contains(t, script, "gitlab.com")
	})

	t.Run("should include Dockerfile node image update section", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			ProviderName:   "github",
			PackageManager: "npm",
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo")

		// then
		assert.Contains(t, script, "NODE_VERSION_CHANGED")
		assert.Contains(t, script, "node:${NODE_VERSION}")
	})
}

func TestBuildEnv(t *testing.T) {
	t.Parallel()

	t.Run("should include all required environment variables", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			AuthToken:      "my-token",
			CloneURL:       "https://example.com/repo.git",
			BranchName:     "upgrade-branch",
			NodeVersion:    "22.12.0",
			DefaultBranch:  "main",
			PackageManager: "npm",
		}

		// when
		env := buildEnv(params, "/tmp/repo")

		// then
		assert.Contains(t, env, "AUTH_TOKEN=my-token")
		assert.Contains(t, env, "CLONE_URL=https://example.com/repo.git")
		assert.Contains(t, env, "BRANCH_NAME=upgrade-branch")
		assert.Contains(t, env, "NODE_VERSION=22.12.0")
		assert.Contains(t, env, "REPO_DIR=/tmp/repo")
		assert.Contains(t, env, "DEFAULT_BRANCH=main")
		assert.Contains(t, env, "PACKAGE_MANAGER=npm")
	})

	t.Run("should include CHANGELOG_FILE when set", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			AuthToken:      "token",
			ChangelogFile:  "/tmp/changelog-12345.md",
			DefaultBranch:  "main",
			PackageManager: "npm",
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
			AuthToken:      "token",
			DefaultBranch:  "main",
			PackageManager: "npm",
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

	t.Run("should include Node.js version upgrade in description", func(t *testing.T) {
		t.Parallel()

		// given
		nodeVersion := "22.12.0"
		nodeVersionUpdated := true

		// when
		desc := GeneratePRDescription(nodeVersion, "npm", nodeVersionUpdated)

		// then
		assert.Contains(t, desc, "## Summary")
		assert.Contains(t, desc, "upgrades the Node.js version to **22.12.0**")
		assert.Contains(t, desc, ".nvmrc")
		assert.Contains(t, desc, "npm update")
	})

	t.Run("should indicate deps-only update when version was already current", func(t *testing.T) {
		t.Parallel()

		// given
		nodeVersion := "22.12.0"
		nodeVersionUpdated := false

		// when
		desc := GeneratePRDescription(nodeVersion, "npm", nodeVersionUpdated)

		// then
		assert.Contains(t, desc, "## Summary")
		assert.Contains(t, desc, "updates all JavaScript dependencies")
		assert.NotContains(t, desc, ".nvmrc")
	})

	t.Run("should mention yarn when using yarn", func(t *testing.T) {
		t.Parallel()

		// given
		nodeVersion := "22.12.0"

		// when
		desc := GeneratePRDescription(nodeVersion, "yarn", false)

		// then
		assert.Contains(t, desc, "yarn upgrade")
	})

	t.Run("should mention pnpm when using pnpm", func(t *testing.T) {
		t.Parallel()

		// given
		nodeVersion := "22.12.0"

		// when
		desc := GeneratePRDescription(nodeVersion, "pnpm", false)

		// then
		assert.Contains(t, desc, "pnpm update")
	})

	t.Run("should include review checklist", func(t *testing.T) {
		t.Parallel()

		// given
		nodeVersion := "22.12.0"

		// when
		desc := GeneratePRDescription(nodeVersion, "npm", true)

		// then
		assert.Contains(t, desc, "### Review Checklist")
		assert.Contains(t, desc, "Verify build passes")
		assert.Contains(t, desc, "Verify tests pass")
		assert.Contains(t, desc, "lockfile")
	})

	t.Run("should include autoupdate attribution", func(t *testing.T) {
		t.Parallel()

		// given
		nodeVersion := "22.12.0"

		// when
		desc := GeneratePRDescription(nodeVersion, "npm", false)

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
			BranchName:     "chore/upgrade-js-deps",
			NodeVersion:    "22.12.0",
			PackageManager: "npm",
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
		assert.Contains(t, script, "npm update")
		assert.Contains(t, script, "CHANGES_PUSHED")
	})

	t.Run("should include auth when token is provided", func(t *testing.T) {
		t.Parallel()

		// given
		params := localUpgradeParams{
			BranchName:     "chore/upgrade-js-deps",
			NodeVersion:    "22.12.0",
			AuthToken:      "my-token",
			ProviderName:   "github",
			PackageManager: "npm",
		}

		// when
		script := buildLocalUpgradeScript(params)

		// then
		assert.Contains(t, script, "TEMP_GITCONFIG")
		assert.Contains(t, script, "x-access-token")
		assert.Contains(t, script, "GIT_CONFIG_GLOBAL")
		assert.NotContains(t, script, "git clone")
	})

	t.Run("should place Dockerfile update between JS upgrade and changelog", func(t *testing.T) {
		t.Parallel()

		// given
		params := localUpgradeParams{
			BranchName:     "chore/upgrade-js-deps",
			NodeVersion:    "22.12.0",
			PackageManager: "npm",
		}

		// when
		script := buildLocalUpgradeScript(params)

		// then
		npmIdx := strings.Index(script, "npm update")
		dockerfileIdx := strings.Index(script, "Updating Dockerfile node image tags")
		changelogIdx := strings.Index(script, "Updating CHANGELOG.md")

		assert.Greater(t, dockerfileIdx, npmIdx, "Dockerfile update should come after npm update")
		assert.Greater(t, changelogIdx, dockerfileIdx, "CHANGELOG update should come after Dockerfile update")
	})
}

func TestBuildLocalEnv(t *testing.T) {
	t.Parallel()

	t.Run("should include core variables without auth when no token", func(t *testing.T) {
		t.Parallel()

		// given
		params := localUpgradeParams{
			BranchName:     "chore/upgrade-js-deps",
			NodeVersion:    "22.12.0",
			PackageManager: "npm",
		}

		// when
		env := buildLocalEnv(params)

		// then
		assert.Contains(t, env, "BRANCH_NAME=chore/upgrade-js-deps")
		assert.Contains(t, env, "NODE_VERSION=22.12.0")
		assert.Contains(t, env, "PACKAGE_MANAGER=npm")
	})

	t.Run("should include AUTH_TOKEN when token is provided", func(t *testing.T) {
		t.Parallel()

		// given
		params := localUpgradeParams{
			BranchName:     "branch",
			NodeVersion:    "22.12.0",
			AuthToken:      "my-secret-token",
			PackageManager: "npm",
		}

		// when
		env := buildLocalEnv(params)

		// then
		assert.Contains(t, env, "AUTH_TOKEN=my-secret-token")
		assert.Contains(t, env, "GIT_HTTPS_TOKEN=my-secret-token")
	})
}
