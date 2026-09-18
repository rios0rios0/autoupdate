package golang_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
)

// scriptOutputFixture is a slice of real upgrade-script output: markers mixed
// in with the prose beside them and everything else the run said, because that
// is what the parser is handed.
const scriptOutputFixture = `=== Upgrading Go module in . ===
Running go get -u -t ./...
go: upgraded github.com/example/lib v1.2.2 => v1.2.3
Checking for newer majors published under a different path...
GO_MAJOR_AVAILABLE=.|github.com/example/lib|v1.2.3|github.com/example/lib/v3|v3.0.1
  github.com/example/lib v1.2.3: a newer major is published as github.com/example/lib/v3 v3.0.1
    not applied -- it is a different module path, so taking it means rewriting imports
=== Upgrading Go module in tests/terratest ===
GO_MAJOR_AVAILABLE=tests/terratest|github.com/example/lib|v1.2.3|github.com/example/lib/v3|v3.0.1
GO_MAJOR_AVAILABLE=tests/terratest|github.com/example/alpha|v0.4.0|github.com/example/alpha/v2|v2.0.0
CHANGES_PUSHED=true
`

func TestParseMajorsAvailableReadsTheMarkers(t *testing.T) {
	t.Parallel()

	// given / when
	majors := golang.ParseMajorsAvailable(scriptOutputFixture)

	// then
	require.Len(t, majors, 2, "the same finding in two modules is one fact")
	// Sorted by module path, so the description does not reshuffle between runs.
	assert.Equal(t, "github.com/example/alpha", majors[0].Path)
	assert.Equal(t, "v0.4.0", majors[0].CurrentVersion)
	assert.Equal(t, "github.com/example/alpha/v2", majors[0].NextPath)
	assert.Equal(t, "v2.0.0", majors[0].NextVersion)
	assert.Equal(t, "github.com/example/lib", majors[1].Path)
	assert.Equal(t, "github.com/example/lib/v3", majors[1].NextPath)
}

func TestParseMajorsAvailableIgnoresEverythingElse(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"no markers at all":   "Running go mod tidy...\nCHANGES_PUSHED=true\n",
		"empty output":        "",
		"truncated marker":    "GO_MAJOR_AVAILABLE=.|github.com/example/lib|v1.2.3\n",
		"marker-like prose":   "  a GO_MAJOR_AVAILABLE marker looks like this\n",
		"trailing separators": "GO_MAJOR_AVAILABLE=a|b|c|d|e|f\n",
	}

	for name, output := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// given / when
			majors := golang.ParseMajorsAvailable(output)

			// then
			assert.Empty(t, majors,
				"a line that is not a complete marker must not become a finding")
		})
	}
}

func TestGenerateGoPRDescriptionReportsNewerMajors(t *testing.T) {
	t.Parallel()

	// given
	majors := golang.ParseMajorsAvailable(scriptOutputFixture)

	// when
	description := golang.GenerateGoPRDescription("1.27.1", false, false, true, majors)

	// then
	assert.Contains(t, description, "Newer major versions available (not in this PR)")
	assert.Contains(t, description, "`github.com/example/lib/v3` `v3.0.1`")
	assert.Contains(t, description, "`github.com/example/alpha/v2` `v2.0.0`")
	// The reader has to be told why it is not simply in the diff, or the
	// obvious next question is why the bot did not do its job.
	assert.Contains(t, description, "different module path")
	// The section belongs above the checklist; a notice after the sign-off
	// reads as a footnote.
	assert.Less(t,
		strings.Index(description, "Newer major versions available"),
		strings.Index(description, "Review Checklist"),
		"the notice must come before the review checklist")
}

func TestGenerateGoPRDescriptionSaysNothingWhenThereAreNoNewerMajors(t *testing.T) {
	t.Parallel()

	for name, majors := range map[string][]golang.MajorAvailable{
		"none found":  {},
		"never asked": nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// given / when
			description := golang.GenerateGoPRDescription("1.27.1", false, false, true, majors)

			// then
			// A "nothing to report" line on every pull request is one readers
			// stop seeing, and it would then be there on the one that mattered.
			assert.NotContains(t, description, "Newer major versions available")
			assert.Contains(t, description, "Review Checklist",
				"the rest of the description is unaffected")
		})
	}
}
