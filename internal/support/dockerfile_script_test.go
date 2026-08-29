package support_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/support"
)

// pythonDigest is a digest-shaped fixture value. Only its shape matters: the
// script recognises a digest pin, it never resolves one.
const pythonDigest = "@sha256:" +
	"1111111111111111111111111111111111111111111111111111111111111111"

func TestDockerfileTagUpdateScript(t *testing.T) {
	t.Parallel()

	t.Run("should upgrade a base image pinned by tag alone", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := writeDockerfiles(t, map[string]string{
			"Dockerfile": "FROM python:3.13-slim\nRUN true\n",
		})

		// when
		runTagUpdateScript(t, repoDir)

		// then
		assert.Equal(t, "FROM python:3.14.0-slim\nRUN true\n",
			readFile(t, repoDir))
	})

	t.Run("should leave a base image pinned by digest untouched", func(t *testing.T) {
		t.Parallel()

		// given: the digest is what Docker resolves, so moving the tag beside it
		// would produce a diff that reads as an upgrade and builds the old image.
		content := "FROM python:3.13-slim" + pythonDigest + "\nRUN true\n"
		repoDir := writeDockerfiles(t, map[string]string{"Dockerfile": content})

		// when
		runTagUpdateScript(t, repoDir)

		// then
		assert.Equal(t, content, readFile(t, repoDir))
	})

	t.Run("should report the digest-pinned reference it left behind", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := writeDockerfiles(t, map[string]string{
			"Dockerfile": "FROM python:3.13-slim" + pythonDigest + "\n",
		})

		// when
		output := runTagUpdateScript(t, repoDir)

		// then
		assert.Contains(t, output, "Left the digest-pinned python")
	})

	t.Run("should upgrade the plain pin while leaving the digest-pinned one alone",
		func(t *testing.T) {
			t.Parallel()

			// given: a multi-stage build pinning the same image both ways
			repoDir := writeDockerfiles(t, map[string]string{
				"Dockerfile": "FROM python:3.13-slim" + pythonDigest + " AS builder\n" +
					"FROM python:3.13-slim AS runtime\n",
			})

			// when
			runTagUpdateScript(t, repoDir)

			// then
			assert.Equal(t,
				"FROM python:3.13-slim"+pythonDigest+" AS builder\n"+
					"FROM python:3.14.0-slim AS runtime\n",
				readFile(t, repoDir))
		})

	t.Run("should leave an image already ahead of the version untouched", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM python:3.15-slim\n"
		repoDir := writeDockerfiles(t, map[string]string{"Dockerfile": content})

		// when
		runTagUpdateScript(t, repoDir)

		// then
		assert.Equal(t, content, readFile(t, repoDir))
	})

	t.Run("should leave every file alone when the version pin did not move", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM python:3.13-slim\n"
		repoDir := writeDockerfiles(t, map[string]string{"Dockerfile": content})

		// when
		runTagUpdateScriptWith(t, repoDir, "3.14.0", "false")

		// then
		assert.Equal(t, content, readFile(t, repoDir))
	})
}

// writeDockerfiles materialises a repository holding the given files and
// returns its root.
func writeDockerfiles(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)

		// A directory needs the owner search bit, so 0o700 is the
		// least-privilege mode here.
		// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	return root
}

// runTagUpdateScript runs the emitted rewrite against repoDir with the pin
// reported as moved, which is what the generated upgrade scripts do.
func runTagUpdateScript(t *testing.T, repoDir string) string {
	t.Helper()

	// The only version any caller has ever passed.
	const version = "3.14.0"

	return runTagUpdateScriptWith(t, repoDir, version, "true")
}

// runTagUpdateScriptWith runs the emitted rewrite with the "did the pin move?"
// flag set explicitly, and returns its output.
func runTagUpdateScriptWith(t *testing.T, repoDir, version, changed string) string {
	t.Helper()

	script := "#!/bin/bash\nset -euo pipefail\n\n" +
		support.DockerfileTagUpdateScript(support.DockerfileTagUpdate{
			ChangedVar: "PYTHON_VERSION_CHANGED",
			VersionVar: "PYTHON_VERSION",
			Subject:    "Python",
			Images:     []support.DockerfileImage{{Name: "python"}},
		})

	path := filepath.Join(t.TempDir(), "rewrite.sh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700))

	cmd := exec.Command("bash", path)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"PYTHON_VERSION="+version,
		"PYTHON_VERSION_CHANGED="+changed,
	)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "script failed: %s", output)

	return string(output)
}

// readFile returns the current content of the Dockerfile under root.
func readFile(t *testing.T, root string) string {
	t.Helper()

	const name = "Dockerfile"

	content, err := os.ReadFile(filepath.Join(root, name))
	require.NoError(t, err)

	return string(content)
}
