package gitlocal_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlocal"
)

func TestOriginPushURLs(t *testing.T) {
	t.Parallel()

	t.Run("should return origin's URL as the repository's own configuration names it", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithHTTPSRemote(t)

		// when
		urls, err := gitlocal.OriginPushURLs(repoDir)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"https://github.com/test/test-repo.git"}, urls)
	})

	t.Run("should list a pushurl after origin's URL, where go-git pushes to it", func(t *testing.T) {
		t.Parallel()

		// given -- fetched over SSH, pushed over HTTPS
		repoDir := createTestRepoWithCommit(t)
		gitIn(t, repoDir, "remote", "add", "origin", "git@github.com:test/test-repo.git")
		gitIn(t, repoDir, "remote", "set-url", "--push", "origin", "https://push.example.com/test/test-repo.git")

		// when
		urls, err := gitlocal.OriginPushURLs(repoDir)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{
			"git@github.com:test/test-repo.git",
			"https://push.example.com/test/test-repo.git",
		}, urls)
	})

	t.Run("should list every url origin has, in the order they are configured", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)
		gitIn(t, repoDir, "remote", "add", "origin", "git@github.com:test/test-repo.git")
		gitIn(t, repoDir, "config", "--add", "remote.origin.url", "https://mirror.example.com/test/test-repo.git")

		// when
		urls, err := gitlocal.OriginPushURLs(repoDir)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{
			"git@github.com:test/test-repo.git",
			"https://mirror.example.com/test/test-repo.git",
		}, urls)
	})

	t.Run("should apply an insteadOf rule of the repository's own configuration", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithHTTPSRemote(t)
		repo, err := git.PlainOpen(repoDir)
		require.NoError(t, err)
		cfg, err := repo.Config()
		require.NoError(t, err)
		cfg.URLs["git@github.com:"] = &gitconfig.URL{Name: "git@github.com:", InsteadOf: "https://github.com/"}
		require.NoError(t, repo.SetConfig(cfg))

		// when
		urls, err := gitlocal.OriginPushURLs(repoDir)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"git@github.com:test/test-repo.git"}, urls)
	})

	t.Run("should return error when the repository has no origin", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := createTestRepoWithCommit(t)

		// when
		urls, err := gitlocal.OriginPushURLs(repoDir)

		// then
		require.ErrorContains(t, err, "failed to get origin remote")
		assert.Empty(t, urls)
	})

	t.Run("should return error for a subdirectory of a repository", func(t *testing.T) {
		t.Parallel()

		// given -- git finds the repository from here, go-git does not, and neither can the push
		repoDir := createTestRepoWithHTTPSRemote(t)
		subDir := filepath.Join(repoDir, "module")
		// A directory needs the owner search bit, so 0o700 is the least-privilege mode here.
		// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
		require.NoError(t, os.MkdirAll(subDir, 0o700))

		// when
		urls, err := gitlocal.OriginPushURLs(subDir)

		// then
		require.ErrorContains(t, err, "failed to open repository")
		assert.Empty(t, urls)
	})
}

func TestOriginRepositoryHostName(t *testing.T) {
	t.Parallel()

	t.Run("should return the host of an HTTPS URL without reading the SSH configuration", func(t *testing.T) {
		t.Parallel()

		// given
		lookups := 0
		origins := &gitlocal.OriginRepository{SSHHostName: func(string) string {
			lookups++
			return "gitlab.com"
		}}

		// when
		host := origins.HostName("https://oauth2@gitlab.example.com/group/repo.git")

		// then
		assert.Equal(t, "gitlab.example.com", host)
		assert.Zero(t, lookups, "an HTTPS host is no SSH alias")
	})

	t.Run("should return the host of an scp-like URL the SSH configuration has nothing for", func(t *testing.T) {
		t.Parallel()

		// given
		origins := &gitlocal.OriginRepository{SSHHostName: func(string) string { return "" }}

		// when
		host := origins.HostName("git@gitlab.example.com:platform/service.git")

		// then
		assert.Equal(t, "gitlab.example.com", host)
	})

	t.Run("should return the HostName the SSH configuration gives an alias", func(t *testing.T) {
		t.Parallel()

		// given
		var asked []string
		origins := &gitlocal.OriginRepository{SSHHostName: func(alias string) string {
			asked = append(asked, alias)
			return "gitlab.com"
		}}

		// when
		scpHost := origins.HostName("git@gitlab.com-work:group/repo.git")
		sshHost := origins.HostName("ssh://git@gitlab.com-work:2222/group/repo.git")

		// then
		assert.Equal(t, "gitlab.com", scpHost)
		assert.Equal(t, "gitlab.com", sshHost)
		assert.Equal(t, []string{"gitlab.com-work", "gitlab.com-work"}, asked)
	})

	t.Run("should return no host for a path on the local filesystem", func(t *testing.T) {
		t.Parallel()

		// given
		origins := &gitlocal.OriginRepository{SSHHostName: func(string) string { return "gitlab.com" }}

		// when
		host := origins.HostName("/srv/git/gitlab.com/group/repo.git")

		// then
		assert.Empty(t, host)
	})
}

// gitIn runs git in dir with signing and hooks off, and fails the test when it fails.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command(
		"git",
		append([]string{"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, string(output))
}
