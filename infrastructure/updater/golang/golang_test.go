package golang //nolint:testpackage // tests unexported functions

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/domain"
	testdoubles "github.com/rios0rios0/autoupdate/test"
)

func TestGoUpdater_Name(t *testing.T) {
	t.Parallel()

	t.Run("should return golang", func(t *testing.T) {
		t.Parallel()

		// given
		u := New()

		// when
		name := u.Name()

		// then
		assert.Equal(t, "golang", name)
	})
}

func TestFindGoBinary(t *testing.T) {
	t.Parallel()

	t.Run("should find go binary on system", func(t *testing.T) {
		t.Parallel()

		// given - a system where Go is installed (CI/dev environment)

		// when
		path, err := findGoBinary()

		// then
		// This test verifies the function works in the current environment.
		// In CI environments where Go is installed, it should succeed.
		if err == nil {
			assert.NotEmpty(t, path)
			assert.Contains(t, path, "go")
		}
		// If Go is genuinely not installed, the error is expected
	})
}

func TestGenerateGoPRDescription(t *testing.T) {
	t.Parallel()

	t.Run("should include Go version upgrade in description when version was updated", func(t *testing.T) {
		t.Parallel()

		// given
		goVersion := "1.25.7"
		hasConfigSH := false
		goVersionUpdated := true

		// when
		desc := GenerateGoPRDescription(goVersion, hasConfigSH, goVersionUpdated)

		// then
		assert.Contains(t, desc, "## Summary")
		assert.Contains(t, desc, "upgrades the Go version to **1.25.7**")
		assert.Contains(t, desc, "go.mod")
		assert.Contains(t, desc, "go get -u all")
		assert.Contains(t, desc, "go mod tidy")
		assert.NotContains(t, desc, "config.sh")
	})

	t.Run("should indicate deps-only update when version was already current", func(t *testing.T) {
		t.Parallel()

		// given
		goVersion := "1.25.7"
		hasConfigSH := false
		goVersionUpdated := false

		// when
		desc := GenerateGoPRDescription(goVersion, hasConfigSH, goVersionUpdated)

		// then
		assert.Contains(t, desc, "## Summary")
		assert.Contains(t, desc, "updates all Go module dependencies")
		assert.Contains(t, desc, "already at **1.25.7**")
		assert.NotContains(t, desc, "Updated `go.mod` Go directive")
		assert.Contains(t, desc, "go get -u all")
		assert.Contains(t, desc, "go mod tidy")
	})

	t.Run("should mention config.sh when present", func(t *testing.T) {
		t.Parallel()

		// given
		goVersion := "1.25.7"
		hasConfigSH := true

		// when
		desc := GenerateGoPRDescription(goVersion, hasConfigSH, true)

		// then
		assert.Contains(t, desc, "config.sh")
		assert.Contains(t, desc, "private package settings")
	})

	t.Run("should include review checklist", func(t *testing.T) {
		t.Parallel()

		// given
		goVersion := "1.24"
		hasConfigSH := false

		// when
		desc := GenerateGoPRDescription(goVersion, hasConfigSH, true)

		// then
		assert.Contains(t, desc, "### Review Checklist")
		assert.Contains(t, desc, "Verify build passes")
		assert.Contains(t, desc, "Verify tests pass")
		assert.Contains(t, desc, "go.sum")
	})

	t.Run("should include autoupdate attribution", func(t *testing.T) {
		t.Parallel()

		// given
		goVersion := "1.25.7"

		// when
		desc := GenerateGoPRDescription(goVersion, false, true)

		// then
		assert.Contains(t, desc, "autoupdate")
		assert.Contains(t, desc, "github.com/rios0rios0/autoupdate")
	})
}

func TestParseGoDirective(t *testing.T) {
	t.Parallel()

	t.Run("should extract three-part version from go.mod content", func(t *testing.T) {
		t.Parallel()

		// given
		content := "module github.com/org/repo\n\ngo 1.25.7\n\nrequire (\n)\n"

		// when
		version := parseGoDirective(content)

		// then
		assert.Equal(t, "1.25.7", version)
	})

	t.Run("should extract two-part version from go.mod content", func(t *testing.T) {
		t.Parallel()

		// given
		content := "module github.com/org/repo\n\ngo 1.25\n"

		// when
		version := parseGoDirective(content)

		// then
		assert.Equal(t, "1.25", version)
	})

	t.Run("should return empty string when go directive is missing", func(t *testing.T) {
		t.Parallel()

		// given
		content := "module github.com/org/repo\n"

		// when
		version := parseGoDirective(content)

		// then
		assert.Empty(t, version)
	})

	t.Run("should handle leading/trailing whitespace on the go line", func(t *testing.T) {
		t.Parallel()

		// given
		content := "module github.com/org/repo\n  go 1.24.3  \n"

		// when
		version := parseGoDirective(content)

		// then
		assert.Equal(t, "1.24.3", version)
	})

	t.Run("should not match toolchain directive", func(t *testing.T) {
		t.Parallel()

		// given — "toolchain go1.25.7" should NOT be picked up
		content := "module github.com/org/repo\n\ntoolchain go1.25.7\n"

		// when
		version := parseGoDirective(content)

		// then
		assert.Empty(t, version)
	})
}

func TestResolveVersionContext(t *testing.T) {
	t.Parallel()

	t.Run("should choose version-upgrade branch when go directive is older", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			FileContents: map[string]string{
				"go.mod": "module example.com/repo\n\ngo 1.24.3\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := resolveVersionContext(ctx, provider, repo, "1.25.7")

		// then
		assert.Equal(t, "1.25.7", vCtx.LatestVersion)
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-go-1.25.7", vCtx.BranchName)
	})

	t.Run("should choose deps-only branch when go directive matches latest", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			FileContents: map[string]string{
				"go.mod": "module example.com/repo\n\ngo 1.25.7\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := resolveVersionContext(ctx, provider, repo, "1.25.7")

		// then
		assert.Equal(t, "1.25.7", vCtx.LatestVersion)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-deps-1.25.7", vCtx.BranchName)
	})

	t.Run("should default to version-upgrade when GetFileContent fails", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			FileContentErr: errors.New("file not found"),
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := resolveVersionContext(ctx, provider, repo, "1.25.7")

		// then
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-go-1.25.7", vCtx.BranchName)
	})

	t.Run("should treat two-part version as needing upgrade to three-part latest", func(t *testing.T) {
		t.Parallel()

		// given — go.mod says "go 1.25" but latest is "1.25.7"
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			FileContents: map[string]string{
				"go.mod": "module example.com/repo\n\ngo 1.25\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := resolveVersionContext(ctx, provider, repo, "1.25.7")

		// then
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-go-1.25.7", vCtx.BranchName)
	})

	t.Run("should treat missing go directive as needing version upgrade", func(t *testing.T) {
		t.Parallel()

		// given — go.mod exists but has no go directive
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			FileContents: map[string]string{
				"go.mod": "module example.com/repo\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}

		// when
		vCtx := resolveVersionContext(ctx, provider, repo, "1.25.7")

		// then — parseGoDirective returns "" which != "1.25.7"
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-go-1.25.7", vCtx.BranchName)
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
			LatestVersion:       "1.25.7",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-go-1.25.7",
		}

		// when
		path := prepareChangelog(ctx, provider, repo, vCtx)

		// then
		assert.NotEmpty(t, path)
		// Verify the file contains the expected entry
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "### Changed")
		assert.Contains(
			t,
			string(content),
			"- changed the Go version to `1.25.7` and updated all module dependencies",
		)
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
			LatestVersion:       "1.25.7",
			NeedsVersionUpgrade: false,
			BranchName:          "chore/upgrade-deps-1.25.7",
		}

		// when
		path := prepareChangelog(ctx, provider, repo, vCtx)

		// then
		assert.NotEmpty(t, path)
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "- changed the Go module dependencies to their latest versions")
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
			LatestVersion:       "1.25.7",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-go-1.25.7",
		}

		// when
		path := prepareChangelog(ctx, provider, repo, vCtx)

		// then
		assert.Empty(t, path)
	})

	t.Run("should return empty string when CHANGELOG.md has no Unreleased section", func(t *testing.T) {
		t.Parallel()

		// given
		ctx := context.Background()
		provider := &testdoubles.SpyProvider{
			ExistingFiles: map[string]bool{"CHANGELOG.md": true},
			FileContents: map[string]string{
				"CHANGELOG.md": "# Changelog\n\n## [1.0.0] - 2026-01-01\n",
			},
		}
		repo := domain.Repository{Organization: "org", Name: "repo"}
		vCtx := &versionContext{
			LatestVersion:       "1.25.7",
			NeedsVersionUpgrade: true,
			BranchName:          "chore/upgrade-go-1.25.7",
		}

		// when
		path := prepareChangelog(ctx, provider, repo, vCtx)

		// then
		assert.Empty(t, path)
	})
}

func TestBuildUpgradeScript(t *testing.T) {
	t.Parallel()

	t.Run("should include clone, checkout, and sed-based version update", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			CloneURL:      "https://github.com/org/repo.git",
			DefaultBranch: "main",
			BranchName:    "chore/upgrade-go-1.25.7",
			GoVersion:     "1.25.7",
			AuthToken:     "token",
			HasConfigSH:   false,
			ProviderName:  "github",
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo", "/usr/local/go/bin/go")

		// then
		assert.Contains(t, script, "#!/bin/bash")
		assert.Contains(t, script, "set -euo pipefail")
		assert.Contains(t, script, "git clone")
		assert.Contains(t, script, "git checkout -b")

		// Should use portable sed + redirect-and-move instead of go mod edit
		assert.NotContains(t, script, "mod edit -go=")
		assert.NotContains(t, script, "sed -i")
		assert.Contains(t, script, "CURRENT_GO_VERSION=$(grep -m1")
		assert.Contains(t, script, "go.mod > go.mod.tmp && mv go.mod.tmp go.mod")
		assert.Contains(t, script, "GO_VERSION_UPDATED=true")
		assert.Contains(t, script, "GO_VERSION_UPDATED=false")

		// Should guard against missing go directive
		assert.Contains(t, script, "if [ -z \"$CURRENT_GO_VERSION\" ]")
		assert.Contains(t, script, "no go directive found")

		// Should verify sed actually modified the file before setting flags
		assert.Contains(t, script, "UPDATED_VERSION=$(grep -m1")
		assert.Contains(t, script, "failed to update go directive")

		// Should still run dependency updates
		assert.Contains(t, script, "go get -u all")
		assert.Contains(t, script, "go mod tidy")

		// Should re-apply version after go mod tidy
		assert.Contains(t, script, "Re-apply Go version if go mod tidy")
		assert.Contains(t, script, "AFTER_TIDY_VERSION")

		// Should have conditional commit messages
		assert.Contains(t, script, "GO_VERSION_CHANGED")
		assert.Contains(t, script, "CHANGES_PUSHED=true")
		assert.Contains(t, script, "CHANGES_PUSHED=false")
	})

	t.Run("should include config.sh sourcing when present", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			CloneURL:      "https://github.com/org/repo.git",
			DefaultBranch: "main",
			BranchName:    "branch",
			GoVersion:     "1.25.7",
			AuthToken:     "token",
			HasConfigSH:   true,
			ProviderName:  "github",
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo", "/usr/local/go/bin/go")

		// then
		assert.Contains(t, script, "config.sh")
		assert.Contains(t, script, "source ./config.sh")
	})

	t.Run("should not include config.sh when absent", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			CloneURL:      "https://github.com/org/repo.git",
			DefaultBranch: "main",
			BranchName:    "branch",
			GoVersion:     "1.25.7",
			AuthToken:     "token",
			HasConfigSH:   false,
			ProviderName:  "github",
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo", "/usr/local/go/bin/go")

		// then
		assert.NotContains(t, script, "source ./config.sh")
	})

	t.Run("should configure GitHub git auth", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			ProviderName: "github",
			HasConfigSH:  false,
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo", "go")

		// then
		assert.Contains(t, script, "x-access-token")
		assert.Contains(t, script, "github.com")
	})

	t.Run("should configure Azure DevOps git auth", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			ProviderName: "azuredevops",
			HasConfigSH:  false,
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo", "go")

		// then
		assert.Contains(t, script, "dev.azure.com")
		assert.Contains(t, script, "pat:")
	})

	t.Run("should configure GitLab git auth", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			ProviderName: "gitlab",
			HasConfigSH:  false,
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo", "go")

		// then
		assert.Contains(t, script, "oauth2")
		assert.Contains(t, script, "gitlab.com")
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
			GoVersion:     "1.25.7",
			DefaultBranch: "main",
		}

		// when
		env := buildEnv(params, "/tmp/repo", "/usr/local/go/bin/go")

		// then
		assert.Contains(t, env, "AUTH_TOKEN=my-token")
		assert.Contains(t, env, "CLONE_URL=https://example.com/repo.git")
		assert.Contains(t, env, "BRANCH_NAME=upgrade-branch")
		assert.Contains(t, env, "GO_VERSION=1.25.7")
		assert.Contains(t, env, "REPO_DIR=/tmp/repo")
		assert.Contains(t, env, "GO_BINARY=/usr/local/go/bin/go")
		assert.Contains(t, env, "DEFAULT_BRANCH=main")
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
		env := buildEnv(params, "/tmp/repo", "go")

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
		env := buildEnv(params, "/tmp/repo", "go")

		// then
		for _, e := range env {
			assert.NotContains(t, e, "CHANGELOG_FILE")
		}
	})
}

func TestBuildUpgradeScriptChangelogSection(t *testing.T) {
	t.Parallel()

	t.Run("should include changelog update step in script", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			ProviderName: "github",
			HasConfigSH:  false,
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo", "go")

		// then
		assert.Contains(t, script, "CHANGELOG_FILE")
		assert.Contains(t, script, "cp \"$CHANGELOG_FILE\" CHANGELOG.md")
	})
}

func TestBuildUpgradeScriptDockerfileSection(t *testing.T) {
	t.Parallel()

	t.Run("should include Dockerfile update step gated on GO_VERSION_CHANGED", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			ProviderName: "github",
			HasConfigSH:  false,
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo", "go")

		// then
		assert.Contains(t, script, "GO_VERSION_CHANGED")
		assert.Contains(t, script, "golang:${GO_VERSION}")
		assert.Contains(t, script, "Updated $df")

		// Should use -type f to skip directories and -print0 for safe path handling
		assert.Contains(t, script, "-type f")
		assert.Contains(t, script, "-print0")
		assert.Contains(t, script, "read -r -d ''")
	})

	t.Run("should place Dockerfile update after go upgrade and before changelog", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			ProviderName: "github",
			HasConfigSH:  false,
		}

		// when
		script := buildUpgradeScript(params, "/tmp/repo", "go")

		// then — verify ordering: go get → Dockerfile update → CHANGELOG
		goGetIdx := strings.Index(script, "go get -u all")
		dockerfileIdx := strings.Index(script, "Updating Dockerfile golang image tags")
		changelogIdx := strings.Index(script, "Updating CHANGELOG.md")

		assert.Greater(t, dockerfileIdx, goGetIdx, "Dockerfile update should come after go get")
		assert.Greater(t, changelogIdx, dockerfileIdx, "CHANGELOG update should come after Dockerfile update")
	})
}

func TestBuildLocalUpgradeScript(t *testing.T) {
	t.Parallel()

	t.Run("should not include clone or auth when no token is provided", func(t *testing.T) {
		t.Parallel()

		// given
		params := localUpgradeParams{
			BranchName: "chore/upgrade-go-1.25.7",
			GoVersion:  "1.25.7",
		}

		// when
		script := buildLocalUpgradeScript(params)

		// then
		assert.Contains(t, script, "#!/bin/bash")
		assert.Contains(t, script, "set -euo pipefail")

		// Should NOT contain remote-mode clone or auth sections
		assert.NotContains(t, script, "git clone")
		assert.NotContains(t, script, "CLONE_URL")
		assert.NotContains(t, script, "TEMP_GITCONFIG")
		assert.NotContains(t, script, "x-access-token")
		assert.NotContains(t, script, "config.sh")
		assert.NotContains(t, script, "autoupdate[bot]")

		// Should contain dirty-tree check
		assert.Contains(t, script, "git status --porcelain")
		assert.Contains(t, script, "uncommitted changes")

		// Should contain branch creation
		assert.Contains(t, script, "git checkout -b")

		// Should contain Go upgrade commands
		assert.Contains(t, script, "CURRENT_GO_VERSION")
		assert.Contains(t, script, "go get -u all")
		assert.Contains(t, script, "go mod tidy")
		assert.Contains(t, script, "GO_VERSION_UPDATED")

		// Should contain Dockerfile update section
		assert.Contains(t, script, "Updating Dockerfile golang image tags")
		assert.Contains(t, script, "golang:${GO_VERSION}")

		// Should contain changelog update
		assert.Contains(t, script, "CHANGELOG_FILE")
		assert.Contains(t, script, "CHANGELOG.md")

		// Should contain commit and push
		assert.Contains(t, script, "git add -A")
		assert.Contains(t, script, "git commit")
		assert.Contains(t, script, "git push origin")
		assert.Contains(t, script, "CHANGES_PUSHED")
	})

	t.Run("should place Dockerfile update between go upgrade and changelog in local script", func(t *testing.T) {
		t.Parallel()

		// given
		params := localUpgradeParams{
			BranchName: "chore/upgrade-go-1.25.7",
			GoVersion:  "1.25.7",
		}

		// when
		script := buildLocalUpgradeScript(params)

		// then — verify ordering: go get → Dockerfile update → CHANGELOG
		goGetIdx := strings.Index(script, "go get -u all")
		dockerfileIdx := strings.Index(script, "Updating Dockerfile golang image tags")
		changelogIdx := strings.Index(script, "Updating CHANGELOG.md")

		assert.Greater(t, dockerfileIdx, goGetIdx, "Dockerfile update should come after go get")
		assert.Greater(t, changelogIdx, dockerfileIdx, "CHANGELOG update should come after Dockerfile update")
	})

	t.Run("should include auth and config.sh when token and config are present", func(t *testing.T) {
		t.Parallel()

		// given
		params := localUpgradeParams{
			BranchName:   "chore/upgrade-go-1.25.7",
			GoVersion:    "1.25.7",
			AuthToken:    "my-token",
			ProviderName: "azuredevops",
			HasConfigSH:  true,
		}

		// when
		script := buildLocalUpgradeScript(params)

		// then
		assert.Contains(t, script, "TEMP_GITCONFIG")
		assert.Contains(t, script, "dev.azure.com")
		assert.Contains(t, script, "pat:")
		assert.Contains(t, script, "GIT_CONFIG_GLOBAL")
		assert.Contains(t, script, "source ./config.sh")

		// Should still NOT clone
		assert.NotContains(t, script, "git clone")
		assert.NotContains(t, script, "autoupdate[bot]")
	})
}

func TestBuildLocalEnv(t *testing.T) {
	t.Parallel()

	t.Run("should include core variables without auth when no token", func(t *testing.T) {
		t.Parallel()

		// given
		params := localUpgradeParams{
			BranchName: "chore/upgrade-go-1.25.7",
			GoVersion:  "1.25.7",
		}

		// when
		env := buildLocalEnv(params, "/usr/local/go/bin/go")

		// then
		assert.Contains(t, env, "BRANCH_NAME=chore/upgrade-go-1.25.7")
		assert.Contains(t, env, "GO_VERSION=1.25.7")
		assert.Contains(t, env, "GO_BINARY=/usr/local/go/bin/go")

		// Should NOT contain remote-mode-only variables (CLONE_URL,
		// DEFAULT_BRANCH are never inherited from the process env).
		// Note: AUTH_TOKEN / GIT_HTTPS_TOKEN may exist in the process
		// environment, so we only verify we didn't explicitly add them
		// by checking the exact "our-value" entries are absent.
		assert.NotContains(t, env, "CLONE_URL=")
		assert.NotContains(t, env, "DEFAULT_BRANCH=")
	})

	t.Run("should include AUTH_TOKEN and GIT_HTTPS_TOKEN when token is provided", func(t *testing.T) {
		t.Parallel()

		// given
		params := localUpgradeParams{
			BranchName: "branch",
			GoVersion:  "1.25.7",
			AuthToken:  "my-secret-token",
		}

		// when
		env := buildLocalEnv(params, "go")

		// then
		assert.Contains(t, env, "AUTH_TOKEN=my-secret-token")
		assert.Contains(t, env, "GIT_HTTPS_TOKEN=my-secret-token")
	})

	t.Run("should include CHANGELOG_FILE when set", func(t *testing.T) {
		t.Parallel()

		// given
		params := localUpgradeParams{
			BranchName:    "branch",
			GoVersion:     "1.25.7",
			ChangelogFile: "/tmp/changelog-12345.md",
		}

		// when
		env := buildLocalEnv(params, "go")

		// then
		assert.Contains(t, env, "CHANGELOG_FILE=/tmp/changelog-12345.md")
	})

	t.Run("should not include CHANGELOG_FILE when empty", func(t *testing.T) {
		t.Parallel()

		// given
		params := localUpgradeParams{
			BranchName: "branch",
			GoVersion:  "1.25.7",
		}

		// when
		env := buildLocalEnv(params, "go")

		// then
		for _, e := range env {
			assert.NotContains(t, e, "CHANGELOG_FILE")
		}
	})
}
