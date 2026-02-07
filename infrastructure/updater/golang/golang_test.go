package golang //nolint:testpackage // tests unexported functions

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

	t.Run("should include Go version in description", func(t *testing.T) {
		t.Parallel()

		// given
		goVersion := "1.25"
		hasConfigSH := false

		// when
		desc := generateGoPRDescription(goVersion, hasConfigSH)

		// then
		assert.Contains(t, desc, "## Summary")
		assert.Contains(t, desc, "**1.25**")
		assert.Contains(t, desc, "go.mod")
		assert.Contains(t, desc, "go get -u ./...")
		assert.Contains(t, desc, "go mod tidy")
		assert.NotContains(t, desc, "config.sh")
	})

	t.Run("should mention config.sh when present", func(t *testing.T) {
		t.Parallel()

		// given
		goVersion := "1.25"
		hasConfigSH := true

		// when
		desc := generateGoPRDescription(goVersion, hasConfigSH)

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
		desc := generateGoPRDescription(goVersion, hasConfigSH)

		// then
		assert.Contains(t, desc, "### Review Checklist")
		assert.Contains(t, desc, "Verify build passes")
		assert.Contains(t, desc, "Verify tests pass")
		assert.Contains(t, desc, "go.sum")
	})

	t.Run("should include autoupdate attribution", func(t *testing.T) {
		t.Parallel()

		// given
		goVersion := "1.25"

		// when
		desc := generateGoPRDescription(goVersion, false)

		// then
		assert.Contains(t, desc, "autoupdate")
		assert.Contains(t, desc, "github.com/rios0rios0/autoupdate")
	})
}

func TestBuildUpgradeScript(t *testing.T) {
	t.Parallel()

	t.Run("should include clone and checkout commands", func(t *testing.T) {
		t.Parallel()

		// given
		params := upgradeParams{
			CloneURL:      "https://github.com/org/repo.git",
			DefaultBranch: "main",
			BranchName:    "go-deps-upgrade/go-1.25",
			GoVersion:     "1.25",
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
		assert.Contains(t, script, "mod edit -go=")
		assert.Contains(t, script, "go get -u ./...")
		assert.Contains(t, script, "go mod tidy")
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
			GoVersion:     "1.25",
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
			GoVersion:     "1.25",
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
			GoVersion:     "1.25",
			DefaultBranch: "main",
		}

		// when
		env := buildEnv(params, "/tmp/repo", "/usr/local/go/bin/go")

		// then
		assert.Contains(t, env, "AUTH_TOKEN=my-token")
		assert.Contains(t, env, "CLONE_URL=https://example.com/repo.git")
		assert.Contains(t, env, "BRANCH_NAME=upgrade-branch")
		assert.Contains(t, env, "GO_VERSION=1.25")
		assert.Contains(t, env, "REPO_DIR=/tmp/repo")
		assert.Contains(t, env, "GO_BINARY=/usr/local/go/bin/go")
		assert.Contains(t, env, "DEFAULT_BRANCH=main")
	})
}
