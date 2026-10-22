package support_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/support"
)

// majorGoModFixture holds a direct requirement at v1, one already at /v3, one
// at v0, a gopkg.in-style path, and an indirect requirement that must be left
// alone.
const majorGoModFixture = `module github.com/example/project

go 1.27.0

require github.com/example/single v1.4.0

require (
	github.com/example/lib v1.2.3
	github.com/example/suffixed/v3 v3.1.0
	github.com/example/zero v0.4.0
	gopkg.in/example.v2 v2.1.0
)

require (
	github.com/example/transitive v1.0.0 // indirect
)
`

// stubVersionsGoBinary writes a fake `go` answering "list -m -versions" from a
// table of "<path> <space-separated versions>" lines, and logging every call.
//
// It reproduces the part of the real command the probe turns on: a major that
// was never published answers with the path and NO versions, and exits zero.
// A stub that failed instead would let a probe keying on the exit status pass
// this suite and still report every dependency as having a new major waiting.
func stubVersionsGoBinary(t *testing.T, dir, table string) (string, string) {
	t.Helper()

	logPath := filepath.Join(dir, "go-calls.log")
	tablePath := filepath.Join(dir, "versions.txt")
	binPath := filepath.Join(dir, "fake-go")

	require.NoError(t, os.WriteFile(tablePath, []byte(table), 0o600))

	script := "#!" + bashPath(t) + "\n" +
		"echo \"$@\" >> " + shellQuote(logPath) + "\n" +
		"if [ \"$1\" = \"list\" ]; then\n" +
		"    for arg in \"$@\"; do probe=\"$arg\"; done\n" +
		"    versions=$(awk -v p=\"$probe\" '$1 == p { $1 = \"\"; print; exit }' " +
		shellQuote(tablePath) + ")\n" +
		"    if [ -n \"${versions// /}\" ]; then\n" +
		"        echo \"$probe $versions\"\n" +
		"    else\n" +
		"        echo \"$probe\"\n" +
		"    fi\n" +
		"    exit 0\n" +
		"fi\n" +
		"exit 0\n"
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))

	// Same reason as the compile-guard stub: prove it executes, and clear the
	// "text file busy" window before bash depends on it. A non-"list" argument
	// answers nothing, so the probe table is untouched.
	requireStubRuns(t, binPath)
	require.NoError(t, os.WriteFile(logPath, nil, 0o600))

	return binPath, logPath
}

// runMajorProbe materialises the reporter over the fixture and returns what it
// printed.
func runMajorProbe(t *testing.T, table string) (string, string) {
	t.Helper()

	dir := t.TempDir()
	goMod := filepath.Join(dir, "go.mod")
	require.NoError(t, os.WriteFile(goMod, []byte(majorGoModFixture), 0o600))

	goBin, goLog := stubVersionsGoBinary(t, dir, table)

	script := filepath.Join(dir, "harness.sh")
	body := "#!" + bashPath(t) + "\nset -u\n" +
		support.GoMajorAvailableScript() +
		"autoupdate_go_report_new_majors " + shellQuote(goBin) + " " +
		shellQuote(goMod) + " tests/terratest\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))

	return runHarness(t, script), goLog
}

// libMajorMarker is the start of every marker reported for the fixture's
// github.com/example/lib requirement; the newer path and version follow it.
const libMajorMarker = "GO_MAJOR_AVAILABLE=tests/terratest|github.com/example/lib|v1.2.3|"

// consecutiveMajorsTable publishes github.com/example/lib at every major from
// first to last, one final release each.
func consecutiveMajorsTable(first, last int) string {
	var table strings.Builder
	for major := first; major <= last; major++ {
		fmt.Fprintf(&table, "github.com/example/lib/v%d v%d.0.0\n", major, major)
	}

	return table.String()
}

// majorMarkers returns the GO_MAJOR_AVAILABLE lines of the probe's output,
// which is everything the Go half reads from it.
func majorMarkers(output string) []string {
	markers := make([]string, 0)
	for line := range strings.SplitSeq(output, "\n") {
		if strings.HasPrefix(line, "GO_MAJOR_AVAILABLE=") {
			markers = append(markers, line)
		}
	}

	return markers
}

func TestGoMajorAvailableScriptReportsANewerMajorUnderItsOwnPath(t *testing.T) {
	t.Parallel()

	// given
	table := "github.com/example/lib/v2 v2.0.0 v2.1.0\n"

	// when
	output, _ := runMajorProbe(t, table)

	// then
	assert.Contains(t, output,
		"GO_MAJOR_AVAILABLE=tests/terratest|github.com/example/lib|v1.2.3|github.com/example/lib/v2|v2.1.0")
	assert.Contains(t, output, "not applied -- it is a different module path")
}

func TestGoMajorAvailableScriptWalksPastTheFirstMajorItFinds(t *testing.T) {
	t.Parallel()

	// given
	// Two majors ahead: the report has to name the newest, not the first hit.
	table := "github.com/example/lib/v2 v2.1.0\ngithub.com/example/lib/v3 v3.0.1\n"

	// when
	output, _ := runMajorProbe(t, table)

	// then
	assert.Contains(t, output,
		"GO_MAJOR_AVAILABLE=tests/terratest|github.com/example/lib|v1.2.3|github.com/example/lib/v3|v3.0.1")
	assert.NotContains(t, output, "|github.com/example/lib/v2|",
		"only the newest major found should be reported")
}

func TestGoMajorAvailableScriptOffersOnlyFinishedReleases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		table string
		want  []string
	}{
		{
			// go-git's /v6 when it was first proposed: nothing finished yet.
			name:  "a major published only as pre-releases is not offered",
			table: "github.com/example/lib/v2 v2.0.0-alpha.1 v2.0.0-beta.1\n",
			want:  []string{},
		},
		{
			name:  "a later pre-release does not displace the final release",
			table: "github.com/example/lib/v2 v2.0.0 v2.1.0-rc.1\n",
			want:  []string{libMajorMarker + "github.com/example/lib/v2|v2.0.0"},
		},
		{
			name: "the walk stops below a major that has only pre-releases",
			table: "github.com/example/lib/v2 v2.0.0 v2.1.0\n" +
				"github.com/example/lib/v3 v3.0.0-beta.1\n",
			want: []string{libMajorMarker + "github.com/example/lib/v2|v2.1.0"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// given
			table := testCase.table

			// when
			output, _ := runMajorProbe(t, table)

			// then
			assert.Equal(t, testCase.want, majorMarkers(output))
		})
	}
}

func TestGoMajorAvailableScriptWalksMoreThanEightMajors(t *testing.T) {
	t.Parallel()

	// given
	// go-github's shape: far more than eight majors published above the one
	// required, every one of them final. Stopping early offers a major that is
	// itself already behind.
	table := consecutiveMajorsTable(2, 30)

	// when
	output, _ := runMajorProbe(t, table)

	// then
	assert.Equal(t, []string{libMajorMarker + "github.com/example/lib/v30|v30.0.0"},
		majorMarkers(output))
}

func TestGoMajorAvailableScriptStillBoundsTheWalk(t *testing.T) {
	t.Parallel()

	// given
	// A registry that never stops answering: the walk has to end on its own,
	// a hundred majors above the one required.
	table := consecutiveMajorsTable(2, 150)

	// when
	output, goLog := runMajorProbe(t, table)

	// then
	assert.Equal(t, []string{libMajorMarker + "github.com/example/lib/v101|v101.0.0"},
		majorMarkers(output))
	calls, err := os.ReadFile(goLog)
	require.NoError(t, err)
	assert.NotContains(t, string(calls), "github.com/example/lib/v102",
		"the walk must stop at the cap however long the registry keeps answering")
}

func TestGoMajorAvailableScriptReportsNothingWhenNoMajorIsPublished(t *testing.T) {
	t.Parallel()

	// given
	// Every probe answers with the path and no versions, which is what the real
	// command does for a major that does not exist.
	table := ""

	// when
	output, goLog := runMajorProbe(t, table)

	// then
	assert.NotContains(t, output, "GO_MAJOR_AVAILABLE=",
		"an unpublished major must not be reported as available")
	calls, err := os.ReadFile(goLog)
	require.NoError(t, err)
	assert.Contains(t, string(calls), "list -m -versions",
		"the probe must actually have asked")
}

func TestGoMajorAvailableScriptProbesTheRightPaths(t *testing.T) {
	t.Parallel()

	// given
	table := ""

	// when
	_, goLog := runMajorProbe(t, table)

	// then
	calls, err := os.ReadFile(goLog)
	require.NoError(t, err)
	probed := string(calls)

	// v1 and v0 both share the unsuffixed path, so the next suffixed major of
	// either is v2.
	assert.Contains(t, probed, "github.com/example/lib/v2")
	assert.Contains(t, probed, "github.com/example/zero/v2")
	// A single-line require counts the same as one inside a block.
	assert.Contains(t, probed, "github.com/example/single/v2")
	// Already at /v3, so the next one up is /v4 -- not /v2 again.
	assert.Contains(t, probed, "github.com/example/suffixed/v4")
	assert.NotContains(t, probed, "github.com/example/suffixed/v3/v")
	// Indirect requirements are somebody else's business.
	assert.NotContains(t, probed, "github.com/example/transitive",
		"an indirect requirement must not be probed")
}

func TestGoMajorAvailableScriptLeavesGopkgInPathsAlone(t *testing.T) {
	t.Parallel()

	// given
	// gopkg.in encodes the major with a dot, not a path element, so the probe
	// would be asking about a path that cannot exist.
	table := ""

	// when
	_, goLog := runMajorProbe(t, table)

	// then
	calls, err := os.ReadFile(goLog)
	require.NoError(t, err)
	assert.NotContains(t, string(calls), "gopkg.in/example.v2/v3",
		"a gopkg.in path is already major-qualified; probing a /vN under it is meaningless")
}

func TestGoMajorAvailableScriptNextMajorPathArithmetic(t *testing.T) {
	t.Parallel()

	cases := []struct{ path, version, want string }{
		{"example.com/foo", "v1.2.3", "example.com/foo/v2"},
		{"example.com/foo", "v0.4.0", "example.com/foo/v2"},
		{"example.com/foo/v3", "v3.1.0", "example.com/foo/v4"},
		{"example.com/foo", "v9.0.0+incompatible", "example.com/foo/v10"},
		{"example.com/foo", "not-a-version", ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.path+" "+testCase.version, func(t *testing.T) {
			t.Parallel()

			// given
			dir := t.TempDir()
			script := filepath.Join(dir, "arith.sh")
			body := "#!" + bashPath(t) + "\nset -u\n" +
				support.GoMajorAvailableScript() +
				"autoupdate_go_next_major_path " + shellQuote(testCase.path) + " " +
				shellQuote(testCase.version) + "\n"
			require.NoError(t, os.WriteFile(script, []byte(body), 0o755))

			// when
			output := runHarness(t, script)

			// then
			assert.Equal(t, testCase.want, strings.TrimSpace(output))
		})
	}
}

func TestGoMajorAvailableScriptSkipsDotVersionedPaths(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"gopkg.in/yaml.v3", "gopkg.in/example.v12"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			// given
			dir := t.TempDir()
			script := filepath.Join(dir, "arith.sh")
			body := "#!" + bashPath(t) + "\nset -u\n" +
				support.GoMajorAvailableScript() +
				"autoupdate_go_next_major_path " + shellQuote(path) + " v3.0.1\n"
			require.NoError(t, os.WriteFile(script, []byte(body), 0o755))

			// when
			output := runHarness(t, script)

			// then
			assert.Empty(t, strings.TrimSpace(output),
				"a dot-versioned path is already major-qualified; there is no /vN above it")
		})
	}
}
