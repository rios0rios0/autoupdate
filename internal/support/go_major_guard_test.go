package support_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/support"
)

// goModFixture is the requirement set as it stands *after* `go get -u`, written
// in both of the shapes a real go.mod uses: a parenthesised require block and a
// single-line require.
const goModFixture = `module github.com/example/project

go 1.27.0

require github.com/spf13/cobra v1.10.2

require (
	github.com/gocolly/colly/v2 v2.3.0
	github.com/sirupsen/logrus v1.10.2
	github.com/stretchr/testify v1.12.1
)

require (
	github.com/gobwas/glob v1.0.0 // indirect
	github.com/kennygrant/sanitize v1.2.4 // indirect
	golang.org/x/net v0.58.0 // indirect
)
`

// bashPath resolves the interpreter instead of hard-coding an env shebang,
// which does not resolve on every platform this suite runs on. A stub whose
// shebang cannot be resolved fails as "no such file or directory" on a path
// that is plainly there, which is a confusing way to learn it.
func bashPath(t *testing.T) string {
	t.Helper()

	path, err := exec.LookPath("bash")
	require.NoError(t, err, "bash is required to exercise the guard")

	return path
}

// stubGoBinary writes a fake `go` that appends its arguments to a log and
// succeeds. The guard's job is to decide *which* modules to hold, so what it
// asks for is the observable behaviour; actually resolving a module would make
// the test a network test.
func stubGoBinary(t *testing.T, dir string) (string, string) {
	t.Helper()

	logPath := filepath.Join(dir, "go-calls.log")
	binPath := filepath.Join(dir, "fake-go")
	script := "#!" + bashPath(t) + "\necho \"$@\" >> " + shellQuote(logPath) + "\nexit 0\n"
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))

	// A stub that cannot execute makes every "did not call go" assertion pass
	// for the wrong reason: the log would simply never exist, and an absent
	// file contains nothing. Prove it runs here, then truncate that proof.
	require.NoError(t, exec.Command(binPath, "stub-self-test").Run(),
		"stub go binary is not executable")
	require.FileExists(t, logPath, "stub go binary did not write its log")
	require.NoError(t, os.WriteFile(logPath, nil, 0o600))

	return binPath, logPath
}

// shellQuote wraps a path in single quotes for safe embedding in a script.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// guardHarness is one materialised guard: the script that runs it, and the log
// the stub `go` writes what it was asked for into.
type guardHarness struct {
	script string
	goLog  string
}

// writeMajorGuardHarness materialises the guard plus a caller that runs it
// against the fixture.
func writeMajorGuardHarness(t *testing.T, before string) guardHarness {
	t.Helper()

	dir := t.TempDir()

	goModPath := filepath.Join(dir, "go.mod")
	require.NoError(t, os.WriteFile(goModPath, []byte(goModFixture), 0o600))

	beforePath := filepath.Join(dir, "before.txt")
	require.NoError(t, os.WriteFile(beforePath, []byte(before), 0o600))

	goBin, goLog := stubGoBinary(t, dir)

	script := filepath.Join(dir, "harness.sh")
	body := "#!" + bashPath(t) + "\nset -u\ncd " + shellQuote(dir) + "\n" +
		support.GoMajorGuardScript() +
		"autoupdate_go_hold_major_jumps " + shellQuote(goBin) + " " +
		shellQuote(beforePath) + " " + shellQuote(goModPath) + "\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))

	return guardHarness{script: script, goLog: goLog}
}

func runHarness(t *testing.T, script string) string {
	t.Helper()

	output, err := exec.Command("bash", script).CombinedOutput()
	require.NoError(t, err, "harness failed: %s", output)

	return string(output)
}

func TestGoMajorGuardScript(t *testing.T) {
	t.Parallel()

	t.Run("should hold a dependency that crossed from v0 to v1", func(t *testing.T) {
		t.Parallel()

		// given -- the glob case: an indirect dependency nobody named, moved
		// across the one boundary Go promises nothing about
		harness := writeMajorGuardHarness(t,
			"github.com/gobwas/glob v0.2.3\n"+
				"github.com/sirupsen/logrus v1.9.0\n")

		// when
		output := runHarness(t, harness.script)

		// then
		calls, err := os.ReadFile(harness.goLog)
		require.NoError(t, err)
		assert.Contains(t, string(calls), "get github.com/gobwas/glob@v0.2.3")
		assert.Contains(t, output, "holding github.com/gobwas/glob at v0.2.3")
	})

	t.Run("should leave an ordinary minor upgrade alone", func(t *testing.T) {
		t.Parallel()

		// given -- logrus moved v1.9.0 -> v1.10.2, which is the whole point of
		// the run and must survive it
		harness := writeMajorGuardHarness(t,
			"github.com/sirupsen/logrus v1.9.0\n")

		// when
		runHarness(t, harness.script)

		// then
		calls, _ := os.ReadFile(harness.goLog)
		assert.NotContains(t, string(calls), "logrus")
	})

	t.Run("should not touch a dependency whose version did not move", func(t *testing.T) {
		t.Parallel()

		// given
		harness := writeMajorGuardHarness(t,
			"github.com/stretchr/testify v1.12.1\n")

		// when
		runHarness(t, harness.script)

		// then
		calls, _ := os.ReadFile(harness.goLog)
		assert.NotContains(t, string(calls), "testify")
	})

	t.Run("should ignore a module the upgrade removed", func(t *testing.T) {
		t.Parallel()

		// given -- present before, absent after: `go mod tidy` dropped it, and
		// reinstating it would undo that
		harness := writeMajorGuardHarness(t,
			"github.com/dropped/module v0.4.0\n")

		// when
		runHarness(t, harness.script)

		// then
		calls, _ := os.ReadFile(harness.goLog)
		assert.NotContains(t, string(calls), "dropped")
	})

	t.Run("should read a single-line require as well as a block", func(t *testing.T) {
		t.Parallel()

		// given -- cobra is declared outside the parenthesised block, and
		// crossed v0 -> v1 there
		harness := writeMajorGuardHarness(t,
			"github.com/spf13/cobra v0.0.7\n")

		// when
		runHarness(t, harness.script)

		// then
		calls, err := os.ReadFile(harness.goLog)
		require.NoError(t, err)
		assert.Contains(t, string(calls), "get github.com/spf13/cobra@v0.0.7")
	})

	t.Run("should re-run tidy only when something was held", func(t *testing.T) {
		t.Parallel()

		// given
		held := writeMajorGuardHarness(t,
			"github.com/gobwas/glob v0.2.3\n")
		clean := writeMajorGuardHarness(t,
			"github.com/sirupsen/logrus v1.9.0\n")

		// when
		runHarness(t, held.script)
		runHarness(t, clean.script)

		// then
		heldCalls, _ := os.ReadFile(held.goLog)
		cleanCalls, _ := os.ReadFile(clean.goLog)
		assert.Contains(t, string(heldCalls), "mod tidy")
		assert.NotContains(t, string(cleanCalls), "mod tidy")
	})
}
