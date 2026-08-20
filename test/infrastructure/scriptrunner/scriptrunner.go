// Package scriptrunner executes a fragment of a generated upgrade script
// against a temporary repository, so that a property of the emitted bash is
// proven by running it rather than by matching its text.
//
// The updaters that rewrite a version pin do it from bash, inside a clone the
// Go side never sees. A test that only asserts on the script's source proves
// that a guard was written, not that the guard answers correctly — which is the
// half that matters when the question is whether a pin gets downgraded.
package scriptrunner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	scriptMode = 0o700
	stubMode   = 0o700
)

// Options configures one run of a script fragment.
type Options struct {
	// Env holds the variables the fragment reads. The generated scripts run
	// under `set -u`, so every variable a fragment dereferences must be here.
	Env map[string]string

	// Stubs names commands to shadow with a no-op on PATH, keeping the run
	// hermetic when a fragment shells out to a package manager.
	Stubs []string
}

// Run writes fragment as a bash script, executes it with repoDir as the working
// directory, and returns its combined output. A failing script fails the test:
// the fragments under test are expected to tolerate the tools they call being
// absent, so a non-zero exit is a defect rather than an environment problem.
func Run(t *testing.T, repoDir, fragment string, opts Options) string {
	t.Helper()

	workDir := t.TempDir()
	scriptPath := filepath.Join(workDir, "fragment.sh")
	script := "#!/bin/bash\nset -euo pipefail\n\n" + fragment

	require.NoError(t, os.WriteFile(scriptPath, []byte(script), scriptMode))

	cmd := exec.CommandContext(t.Context(), "bash", scriptPath)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), environment(t, workDir, opts)...)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "script fragment failed:\n%s\n--- script ---\n%s", output, script)

	return string(output)
}

// environment renders the requested variables and puts the stub directory ahead
// of everything else on PATH.
func environment(t *testing.T, workDir string, opts Options) []string {
	t.Helper()

	env := make([]string, 0, len(opts.Env)+1)
	for name, value := range opts.Env {
		env = append(env, name+"="+value)
	}

	if len(opts.Stubs) > 0 {
		env = append(env, "PATH="+writeStubs(t, workDir, opts.Stubs)+":"+os.Getenv("PATH"))
	}

	return env
}

// writeStubs materialises a no-op executable for each named command and returns
// the directory holding them.
func writeStubs(t *testing.T, workDir string, names []string) string {
	t.Helper()

	stubDir := filepath.Join(workDir, "stubs")
	require.NoError(t, os.MkdirAll(stubDir, 0o750))

	for _, name := range names {
		path := filepath.Join(stubDir, name)
		body := "#!/bin/sh\necho \"stub " + name + " $*\"\nexit 0\n"
		require.NoError(t, os.WriteFile(path, []byte(body), stubMode))
	}

	return stubDir
}

// ReadFile returns the contents of a file inside the repository under test.
func ReadFile(t *testing.T, repoDir, name string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(repoDir, name)) // #nosec G304 -- test-owned temporary path
	require.NoError(t, err)

	return string(content)
}

// WriteFile creates a file inside the repository under test, creating any
// parent directories it needs.
func WriteFile(t *testing.T, repoDir, name, content string) {
	t.Helper()

	path := filepath.Join(repoDir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// Trimmed returns the contents of a file with surrounding whitespace removed,
// which is how a version pin file is read by the scripts themselves.
func Trimmed(t *testing.T, repoDir, name string) string {
	t.Helper()

	return strings.TrimSpace(ReadFile(t, repoDir, name))
}
