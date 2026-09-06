package support_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestCloneTargetFor(t *testing.T) {
	t.Parallel()

	t.Run(
		"should take the clone URL, token and provider from the provider and strip the ref prefix",
		func(t *testing.T) {
			t.Parallel()

			// given
			provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
				WithProviderName("gitlab").
				WithToken("secret").
				BuildSpy()
			repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
			changelog := support.StagedChangelog{TempPath: "/tmp/changelog", RepoPath: "CHANGELOG.md"}

			// when
			target := support.CloneTargetFor(provider, repo, "chore/upgrade", changelog)

			// then
			assert.Equal(t, "https://example.com/org/repo.git", target.CloneURL)
			assert.Equal(t, "main", target.DefaultBranch)
			assert.Equal(t, "chore/upgrade", target.BranchName)
			assert.Equal(t, "secret", target.AuthToken)
			assert.Equal(t, "gitlab", target.ProviderName)
			assert.Equal(t, changelog, target.Changelog)
		},
	)
}

func TestRemoteCloneScript(t *testing.T) {
	t.Parallel()

	t.Run(
		"should configure a fallback identity, clone the default branch and check out the upgrade branch",
		func(t *testing.T) {
			t.Parallel()

			// when
			script := support.RemoteCloneScript()

			// then
			assert.Contains(t, script, `git config --global user.name "autoupdate[bot]"`)
			assert.Contains(t, script, `git config --global user.email "autoupdate[bot]@users.noreply.github.com"`)
			assert.Contains(t, script, `git clone --depth=1 --branch "$DEFAULT_BRANCH" "$CLONE_URL" "$REPO_DIR" 2>&1`)
			assert.Contains(t, script, `cd "$REPO_DIR"`)
			assert.Contains(t, script, `git checkout -b "$BRANCH_NAME" 2>&1`)
			assert.True(t, strings.HasSuffix(script, "\n\n"), "expected the script to end with a blank line")
		},
	)

	t.Run("should only set the identity when none is configured", func(t *testing.T) {
		t.Parallel()

		// when
		script := support.RemoteCloneScript()

		// then
		assert.Contains(t, script, "if ! git config --global user.name > /dev/null 2>&1; then")
		assert.Contains(t, script, "if ! git config --global user.email > /dev/null 2>&1; then")
	})
}

func TestCloneEnv(t *testing.T) {
	t.Parallel()

	t.Run("should export the clone target and the token under both names", func(t *testing.T) {
		t.Parallel()

		// given
		target := support.CloneTarget{
			CloneURL:      "https://example.com/org/repo.git",
			DefaultBranch: "main",
			BranchName:    "chore/upgrade",
			AuthToken:     "secret",
			ProviderName:  "github",
		}

		// when
		env := support.CloneEnv(target, "/tmp/clone/repo")

		// then
		assert.Contains(t, env, "AUTH_TOKEN=secret")
		assert.Contains(t, env, "GIT_HTTPS_TOKEN=secret")
		assert.Contains(t, env, "CLONE_URL=https://example.com/org/repo.git")
		assert.Contains(t, env, "BRANCH_NAME=chore/upgrade")
		assert.Contains(t, env, "REPO_DIR=/tmp/clone/repo")
		assert.Contains(t, env, "DEFAULT_BRANCH=main")
	})

	t.Run("should carry the staged changelog when one was staged", func(t *testing.T) {
		t.Parallel()

		// given
		target := support.CloneTarget{
			Changelog: support.StagedChangelog{TempPath: "/tmp/changelog", RepoPath: "CHANGELOG.md"},
		}

		// when
		env := support.CloneEnv(target, "/tmp/clone/repo")

		// then
		assert.Contains(t, env, "CHANGELOG_FILE=/tmp/changelog")
		assert.Contains(t, env, "CHANGELOG_DEST=CHANGELOG.md")
	})

	t.Run("should leave the changelog variables unset when nothing was staged", func(t *testing.T) {
		t.Parallel()

		// given
		target := support.CloneTarget{}

		// when
		env := support.CloneEnv(target, "/tmp/clone/repo")

		// then
		for _, entry := range env {
			assert.False(t, strings.HasPrefix(entry, "CHANGELOG_FILE="), "unexpected %q", entry)
		}
	})
}
