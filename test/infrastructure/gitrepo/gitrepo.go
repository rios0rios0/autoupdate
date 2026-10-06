// Package gitrepo lays out throwaway git repositories for tests that read what a
// working tree changed relative to HEAD, the way every dependency reader does.
package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	// dirMode keeps the scratch directories owner-only: everything lives under
	// t.TempDir() and is read by this process alone.
	dirMode  = 0o700
	fileMode = 0o600
)

// New initializes a repository in a temporary directory, commits the given
// files (repository-relative path to content) and returns its root.
func New(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	run(t, root, "init", "--quiet", "--initial-branch=main")
	Write(t, root, files)
	Commit(t, root, "initial commit")
	return root
}

// Write writes files into the working tree without committing them.
func Write(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for path, content := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), dirMode))
		require.NoError(t, os.WriteFile(full, []byte(content), fileMode))
	}
}

// Commit stages every change in the working tree, deletions included, and
// commits it.
func Commit(t *testing.T, root, message string) {
	t.Helper()

	run(t, root, "add", "--all")
	run(t, root, "commit", "--quiet", "--allow-empty", "--message", message)
}

// run executes git in root with an identity and signing settings of its own,
// so the developer's global configuration -- a signing key, a commit template
// -- cannot make a test fail or prompt.
func run(t *testing.T, root string, args ...string) {
	t.Helper()

	full := append([]string{
		"-c", "commit.gpgsign=false",
		"-c", "tag.gpgsign=false",
		"-c", "core.hooksPath=/dev/null",
		"-c", "user.name=autoupdate-test",
		"-c", "user.email=autoupdate-test@example.com",
	}, args...)

	// #nosec G204 -- the executable is git and every argument is built by this
	// helper from test inputs.
	cmd := exec.CommandContext(t.Context(), "git", full...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=autoupdate-test",
		"GIT_AUTHOR_EMAIL=autoupdate-test@example.com",
		"GIT_COMMITTER_NAME=autoupdate-test",
		"GIT_COMMITTER_EMAIL=autoupdate-test@example.com",
	)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, output)
}
