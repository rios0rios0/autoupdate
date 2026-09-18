package support_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/support"
)

// compileGuardPreGoMod is the module as it stood before `go get -u`: two direct
// requirements, and three indirect ones written in both of the shapes a real
// go.mod uses.
const compileGuardPreGoMod = `module github.com/example/project

go 1.27.0

require github.com/spf13/cobra v1.10.2

require (
	github.com/gocolly/colly/v2 v2.3.0
)

require (
	github.com/unchanged/dep v1.0.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	k8s.io/kube-openapi v0.0.0-20260721132016-d427ff9ee9ad // indirect
)
`

// compileGuardPostGoMod is the same module after `go get -u -t ./...`: both
// direct requirements moved, two of the three indirect ones were raised, and a
// fourth indirect requirement appeared that nothing in the before state names.
const compileGuardPostGoMod = `module github.com/example/project

go 1.27.0

require github.com/spf13/cobra v1.11.0

require (
	github.com/gocolly/colly/v2 v2.4.0
)

require (
	github.com/unchanged/dep v1.0.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	k8s.io/kube-openapi v0.0.0-20260911184034-7970a1e230da // indirect
	sigs.k8s.io/structured-merge-diff/v7 v7.0.0 // indirect
)
`

const (
	compileGuardPreGoSum  = "github.com/example/before v1.0.0/go.mod h1:before\n"
	compileGuardPostGoSum = "github.com/example/after v1.0.0/go.mod h1:after\n"
)

// stubScriptedGoBinary writes a fake `go` that logs its arguments and answers
// `vet` from a scripted list of exit codes, one per call, the last repeating.
//
// `vet` is the only answer the guard branches on, and it asks up to three times
// per module for three different states of go.mod. Scripting the sequence is
// what lets a test say "it compiled before the upgrade but not after" without
// a real module graph, a network, and a toolchain to resolve it with.
func stubScriptedGoBinary(t *testing.T, dir string, vetPlan []int) (string, string) {
	t.Helper()

	logPath := filepath.Join(dir, "go-calls.log")
	planPath := filepath.Join(dir, "vet-plan.txt")
	countPath := filepath.Join(dir, "vet-count.txt")
	binPath := filepath.Join(dir, "fake-go")

	codes := make([]string, 0, len(vetPlan))
	for _, code := range vetPlan {
		codes = append(codes, strconv.Itoa(code))
	}
	require.NoError(t, os.WriteFile(planPath, []byte(strings.Join(codes, "\n")+"\n"), 0o600))

	script := "#!" + bashPath(t) + "\n" +
		"echo \"$@\" >> " + shellQuote(logPath) + "\n" +
		"if [ \"$1\" = \"vet\" ]; then\n" +
		"    n=0\n" +
		"    [ -f " + shellQuote(countPath) + " ] && n=$(cat " + shellQuote(countPath) + ")\n" +
		"    n=$((n + 1))\n" +
		"    echo \"$n\" > " + shellQuote(countPath) + "\n" +
		"    code=$(sed -n \"${n}p\" " + shellQuote(planPath) + ")\n" +
		"    [ -n \"$code\" ] || code=$(tail -n1 " + shellQuote(planPath) + ")\n" +
		"    exit \"$code\"\n" +
		"fi\n" +
		"exit 0\n"
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))

	return binPath, logPath
}

// compileGuardHarness is one materialised guard run: the script, the log of
// what the stub `go` was asked for, and the module directory to read back.
type compileGuardHarness struct {
	script    string
	goLog     string
	moduleDir string
	preMod    string
}

// writeCompileGuardHarness materialises the guard plus a caller that runs it
// against a module already in its post-upgrade state, with the pre-upgrade
// manifests set aside the way the generated script sets them aside.
func writeCompileGuardHarness(t *testing.T, vetPlan []int, withPreGoSum bool) compileGuardHarness {
	t.Helper()

	// Two directories rather than one: the guard restores `go.mod` and `go.sum`
	// over the working copy, so a snapshot sitting beside them would be a file
	// the restore could reach.
	dir := t.TempDir()
	moduleDir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(moduleDir, "go.mod"), []byte(compileGuardPostGoMod), 0o600))
	require.NoError(t, os.WriteFile(
		filepath.Join(moduleDir, "go.sum"), []byte(compileGuardPostGoSum), 0o600))

	preMod := filepath.Join(dir, "pre-go.mod")
	require.NoError(t, os.WriteFile(preMod, []byte(compileGuardPreGoMod), 0o600))

	// An absent snapshot is how the generated script records "this module had
	// no go.sum", so the missing-file case is a path the harness has to be able
	// to produce rather than something only the restore ever sees.
	preSum := filepath.Join(dir, "pre-go.sum")
	if withPreGoSum {
		require.NoError(t, os.WriteFile(preSum, []byte(compileGuardPreGoSum), 0o600))
	}

	goBin, goLog := stubScriptedGoBinary(t, dir, vetPlan)

	script := filepath.Join(dir, "harness.sh")
	body := "#!" + bashPath(t) + "\nset -u\ncd " + shellQuote(moduleDir) + "\n" +
		support.GoCompileGuardScript() +
		"autoupdate_go_hold_breaking_bumps " + shellQuote(goBin) + " " +
		shellQuote(preMod) + " " + shellQuote(preSum) + "\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))

	return compileGuardHarness{script: script, goLog: goLog, moduleDir: moduleDir, preMod: preMod}
}

// readTextFile is a test-local read that fails the test rather than returning an
// error nobody would handle differently.
func readTextFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(data)
}

func TestGoCompileGuardScriptLeavesACompilingUpgradeAlone(t *testing.T) {
	t.Parallel()

	// given
	harness := writeCompileGuardHarness(t, []int{0}, true)

	// when
	output := runHarness(t, harness.script)

	// then
	assert.Contains(t, output, "the module compiles")
	assert.NotContains(t, readTextFile(t, harness.goLog), "mod edit",
		"a module that compiles must not have anything held back")
	assert.Equal(t, compileGuardPostGoMod, readTextFile(t, filepath.Join(harness.moduleDir, "go.mod")),
		"the upgrade must reach the commit untouched")
}

func TestGoCompileGuardScriptHoldsTheIndirectRequirementsTheRunRaised(t *testing.T) {
	t.Parallel()

	// given
	// Fails now, compiled before the upgrade, compiles again once the raised
	// indirect requirements are put back.
	harness := writeCompileGuardHarness(t, []int{1, 0, 0}, true)

	// when
	output := runHarness(t, harness.script)

	// then
	calls := readTextFile(t, harness.goLog)
	assert.Contains(t, calls,
		"mod edit -require=k8s.io/kube-openapi@v0.0.0-20260721132016-d427ff9ee9ad")
	assert.Contains(t, calls, "mod edit -require=golang.org/x/net@v0.58.0")
	assert.Contains(t, calls, "mod tidy",
		"the graph has to be resolved again after the holds")
	assert.Contains(t, output, "the module compiles again with those held back")
}

func TestGoCompileGuardScriptHoldsOnlyWhatTheRunActuallyMoved(t *testing.T) {
	t.Parallel()

	// given
	harness := writeCompileGuardHarness(t, []int{1, 0, 0}, true)

	// when
	runHarness(t, harness.script)

	// then
	calls := readTextFile(t, harness.goLog)
	// A direct requirement is what the pull request exists to move; holding
	// those back would leave a branch that updates nothing.
	assert.NotContains(t, calls, "github.com/spf13/cobra@",
		"a direct requirement must not be held back")
	assert.NotContains(t, calls, "github.com/gocolly/colly/v2@",
		"a direct requirement must not be held back")
	assert.NotContains(t, calls, "github.com/unchanged/dep@",
		"a requirement the run did not move has nothing to put back")
	// Nothing to put it back to: it is the upgrade's own arrival, and
	// `go mod tidy` drops it by itself once nothing requires it any more.
	assert.NotContains(t, calls, "sigs.k8s.io/structured-merge-diff/v7@",
		"a requirement the run introduced cannot be held at a previous version")
}

func TestGoCompileGuardScriptLeavesAnUpgradeAloneWhenTheModuleWasAlreadyBroken(t *testing.T) {
	t.Parallel()

	// given
	// Fails now and failed before the upgrade too -- a vet diagnostic nobody
	// has got to, or a module that declares no packages at all.
	harness := writeCompileGuardHarness(t, []int{1, 1}, true)

	// when
	output := runHarness(t, harness.script)

	// then
	assert.Contains(t, output, "did not compile before this upgrade either")
	assert.NotContains(t, readTextFile(t, harness.goLog), "mod edit",
		"a failure the upgrade did not cause must not revert the upgrade")
	assert.Equal(t, compileGuardPostGoMod, readTextFile(t, filepath.Join(harness.moduleDir, "go.mod")),
		"the post-upgrade manifest must be put back after the before-state check")
	assert.Equal(t, compileGuardPostGoSum, readTextFile(t, filepath.Join(harness.moduleDir, "go.sum")),
		"the post-upgrade go.sum must be put back after the before-state check")
}

func TestGoCompileGuardScriptKeepsABreakingUpgradeSoThePullRequestReportsIt(t *testing.T) {
	t.Parallel()

	// given
	// Fails now, compiled before, still fails after the holds -- the shape of a
	// direct dependency that broke the build, which the hold cannot reach.
	harness := writeCompileGuardHarness(t, []int{1, 0, 1}, true)

	// when
	output := runHarness(t, harness.script)

	// then
	assert.Contains(t, output, "leaving the upgrade in place so the pull request reports it")
	// Reverting would leave a branch with nothing in it, and in a single-module
	// repository the commit check would then open no pull request at all -- so a
	// genuine breaking upgrade would be swallowed silently on every run.
	assert.Equal(t, compileGuardPostGoMod, readTextFile(t, filepath.Join(harness.moduleDir, "go.mod")),
		"a breaking upgrade must reach CI rather than be reverted out of sight")
	assert.Equal(t, compileGuardPostGoSum, readTextFile(t, filepath.Join(harness.moduleDir, "go.sum")),
		"go.sum has to stay with the go.mod it was resolved against")
}

func TestGoCompileGuardScriptProbeRestoresAGoSumTheModuleDidNotHaveBefore(t *testing.T) {
	t.Parallel()

	// given
	// The module had no go.sum before the upgrade, so the snapshot is absent.
	// The before-state probe puts the pre-upgrade pair back to ask its question
	// and has to undo that afterwards, removal included.
	harness := writeCompileGuardHarness(t, []int{1, 1}, false)

	// when
	runHarness(t, harness.script)

	// then
	assert.Equal(t, compileGuardPostGoSum, readTextFile(t, filepath.Join(harness.moduleDir, "go.sum")),
		"the probe must put back the go.sum the upgrade wrote after removing it")
	assert.Equal(t, compileGuardPostGoMod, readTextFile(t, filepath.Join(harness.moduleDir, "go.mod")),
		"the probe must put back the go.mod the upgrade wrote")
}

func TestGoCompileGuardScriptNeverRevertsAnUpgrade(t *testing.T) {
	t.Parallel()

	// given
	// Every outcome the guard can reach, by the vet answers that produce it.
	plans := map[string][]int{
		"compiles":                 {0},
		"held back and fixed":      {1, 0, 0},
		"already broken before":    {1, 1},
		"still broken after holds": {1, 0, 1},
	}

	for name, plan := range plans {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			harness := writeCompileGuardHarness(t, plan, true)

			// when
			runHarness(t, harness.script)

			// then
			// The pre-upgrade manifest is a probe input, never an outcome. If it
			// is ever what lands, a breaking upgrade has been reverted out of
			// sight of the only thing that reports it.
			assert.NotEqual(t, compileGuardPreGoMod,
				readTextFile(t, filepath.Join(harness.moduleDir, "go.mod")),
				"the guard must never leave the pre-upgrade go.mod in place")
		})
	}
}

func TestGoCompileGuardScriptReadsIndirectRequirementsOnly(t *testing.T) {
	t.Parallel()

	// given
	dir := t.TempDir()
	goMod := filepath.Join(dir, "go.mod")
	require.NoError(t, os.WriteFile(goMod, []byte(compileGuardPreGoMod+
		"\nrequire golang.org/x/sys v0.30.0 // indirect\n"), 0o600))

	script := filepath.Join(dir, "reader.sh")
	body := "#!" + bashPath(t) + "\nset -u\n" +
		support.GoCompileGuardScript() +
		"autoupdate_go_indirect_versions " + shellQuote(goMod) + "\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))

	// when
	output := runHarness(t, script)

	// then
	assert.Contains(t, output, "k8s.io/kube-openapi v0.0.0-20260721132016-d427ff9ee9ad")
	assert.Contains(t, output, "golang.org/x/net v0.58.0")
	assert.Contains(t, output, "github.com/unchanged/dep v1.0.0")
	// A single-line `require path version // indirect` counts the same as one
	// inside a block; the two shapes are interchangeable in a real go.mod.
	assert.Contains(t, output, "golang.org/x/sys v0.30.0")
	assert.NotContains(t, output, "cobra", "a direct requirement is not indirect")
	assert.NotContains(t, output, "colly", "a direct requirement is not indirect")
}

func TestGoCompileGuardScriptAsksTheToolchainRatherThanBuilding(t *testing.T) {
	t.Parallel()

	// given
	harness := writeCompileGuardHarness(t, []int{0}, true)

	// when
	runHarness(t, harness.script)

	// then
	calls := readTextFile(t, harness.goLog)
	// A harness module holds no non-test Go files, and for one of those
	// `go build ./...` compiles nothing and exits zero while the test packages
	// fail to build. Checking with `go build` is how the breakage this guard
	// exists for reached a released pull request.
	assert.Contains(t, calls, "vet ./...")
	assert.NotContains(t, calls, "build ./...",
		"the check must type-check test files; got calls:\n%s", calls)
}
