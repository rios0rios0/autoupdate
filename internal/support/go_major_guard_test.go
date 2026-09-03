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
		// The reconciliation pass calls autoupdate_version_is_newer, so the
		// guard alone is not a runnable unit -- exactly as the Go updater emits
		// both.
		support.VersionGuardScript() +
		support.GoMajorGuardScript() +
		"autoupdate_go_hold_major_jumps " + shellQuote(goBin) + " " +
		shellQuote(beforePath) + " " + shellQuote(goModPath) + "\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))

	return guardHarness{script: script, goLog: goLog}
}

// runHarness runs the guard and returns its output. The guard exits non-zero
// when it leaves something unresolved, which is a result rather than a crash,
// so only a failure to start is fatal here.
func runHarness(t *testing.T, script string) string {
	t.Helper()

	output, err := exec.Command("bash", script).CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr, "harness failed to run: %s", output)
	}

	return string(output)
}

// guardCase is one before-state put to the guard. `held` names the requirement
// the guard must put back, or is empty when the run must be left alone entirely.
type guardCase struct {
	name   string
	before string
	held   string
}

// guardCases covers what reaches the guard after a real `go get -u`. The first
// row is the one it exists for: an indirect dependency nobody named, moved
// across the only boundary Go promises nothing about.
//
// The empty-`held` rows matter just as much. A guard that also reverted those
// would undo the upgrade it was asked to protect, which is a quieter failure
// than the one it fixes -- the pull request would simply carry nothing.
func guardCases() []guardCase {
	return []guardCase{
		{
			name:   "a dependency that crossed from v0 to v1",
			before: "github.com/gobwas/glob v0.2.3\ngithub.com/sirupsen/logrus v1.9.0\n",
			held:   "github.com/gobwas/glob@v0.2.3",
		},
		{
			name:   "a single-line require that crossed from v0 to v1",
			before: "github.com/spf13/cobra v0.0.7\n",
			held:   "github.com/spf13/cobra@v0.0.7",
		},
		{
			name:   "an ordinary minor upgrade",
			before: "github.com/sirupsen/logrus v1.9.0\n",
			held:   "",
		},
		{
			name:   "a dependency whose version did not move",
			before: "github.com/stretchr/testify v1.12.1\n",
			held:   "",
		},
		{
			name:   "a module the upgrade removed",
			before: "github.com/dropped/module v0.4.0\n",
			held:   "",
		},
	}
}

func TestGoMajorGuardScript(t *testing.T) {
	t.Parallel()

	for _, testCase := range guardCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// given
			harness := writeMajorGuardHarness(t, testCase.before)

			// when
			runHarness(t, harness.script)

			// then
			calls, err := os.ReadFile(harness.goLog)
			require.NoError(t, err)

			if testCase.held == "" {
				assert.NotContains(t, string(calls), "get ",
					"nothing should have been held back")

				return
			}

			assert.Contains(t, string(calls), "get "+testCase.held)
		})
	}

	t.Run("should re-run tidy only when something was held", func(t *testing.T) {
		t.Parallel()

		// given
		held := writeMajorGuardHarness(t, "github.com/gobwas/glob v0.2.3\n")
		clean := writeMajorGuardHarness(t, "github.com/sirupsen/logrus v1.9.0\n")

		// when
		runHarness(t, held.script)
		runHarness(t, clean.script)

		// then -- tidy is what makes the hold take effect, and running it when
		// nothing moved would rewrite go.mod for no reason
		heldCalls, err := os.ReadFile(held.goLog)
		require.NoError(t, err)
		cleanCalls, err := os.ReadFile(clean.goLog)
		require.NoError(t, err)
		assert.Contains(t, string(heldCalls), "mod tidy")
		assert.NotContains(t, string(cleanCalls), "mod tidy")
	})

	t.Run("should report each requirement it holds", func(t *testing.T) {
		t.Parallel()

		// given -- the log line is how a reader of the pull request learns an
		// upgrade was declined rather than never offered
		harness := writeMajorGuardHarness(t, "github.com/gobwas/glob v0.2.3\n")

		// when
		output := runHarness(t, harness.script)

		// then
		assert.Contains(t, output, "holding github.com/gobwas/glob at v0.2.3")
		assert.Contains(t, output, "crosses a major version boundary")
	})
}

// TestGoMajorGuardReconciliation covers the pass that runs after the holds, on
// the go.mod that is actually going to be committed.
//
// The stub `go` never rewrites go.mod, so from the reconciliation's point of
// view every hold it issued failed -- which is precisely the state worth
// asserting on, because that is what a real conflict looks like and it used to
// ship silently under a pull request claiming the opposite.
func TestGoMajorGuardReconciliation(t *testing.T) {
	t.Parallel()

	t.Run("should report a hold that did not take", func(t *testing.T) {
		t.Parallel()

		// given -- glob is v1.0.0 in the fixture and the stub cannot move it
		harness := writeMajorGuardHarness(t, "github.com/gobwas/glob v0.2.3\n")

		// when
		output := runHarness(t, harness.script)

		// then
		assert.Contains(t, output, "github.com/gobwas/glob is still v1.0.0")
		assert.Contains(t, output, "did not take")
	})

	t.Run("should report a requirement that moved backwards", func(t *testing.T) {
		t.Parallel()

		// given -- logrus started ahead of where the fixture leaves it, which is
		// what a cascade from `go get <path>@<older>` looks like
		harness := writeMajorGuardHarness(t, "github.com/sirupsen/logrus v1.11.0\n")

		// when
		output := runHarness(t, harness.script)

		// then
		assert.Contains(t, output, "github.com/sirupsen/logrus moved backwards")
		assert.Contains(t, output, "v1.11.0 -> v1.10.2")
	})

	t.Run("should say nothing when every requirement moved forwards", func(t *testing.T) {
		t.Parallel()

		// given
		harness := writeMajorGuardHarness(t, "github.com/sirupsen/logrus v1.9.0\n")

		// when
		output := runHarness(t, harness.script)

		// then
		assert.NotContains(t, output, "WARNING")
		assert.NotContains(t, output, "moved backwards")
	})
}
