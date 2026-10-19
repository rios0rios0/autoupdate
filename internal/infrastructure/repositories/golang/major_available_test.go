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
	migrations := golang.UnpreparedMigrations(golang.ParseMajorsAvailable(scriptOutputFixture))

	// when
	description := golang.GenerateGoPRDescription("1.27.1", false, false, true, migrations)

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

	for name, migrations := range map[string][]golang.MigrationPrepared{
		"none found":  {},
		"never asked": nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// given / when
			description := golang.GenerateGoPRDescription("1.27.1", false, false, true, migrations)

			// then
			// A "nothing to report" line on every pull request is one readers
			// stop seeing, and it would then be there on the one that mattered.
			assert.NotContains(t, description, "Newer major versions available")
			assert.Contains(t, description, "Review Checklist",
				"the rest of the description is unaffected")
		})
	}
}

// migrationRow is one finding with whatever the run did about it.
func migrationRow(path string, prepared bool, note string) golang.MigrationPrepared {
	return golang.MigrationPrepared{
		ModuleDir:      ".",
		Path:           "github.com/example/" + path,
		CurrentVersion: "v1.2.3",
		NextPath:       "github.com/example/" + path + "/v3",
		NextVersion:    "v3.0.1",
		Prepared:       prepared,
		Branch:         "chore/migrate-example-" + path + "-v3",
		URL: "https://github.com/example/repo/compare/main...chore/migrate-example-" +
			path + "-v3?expand=1",
		Note: note,
	}
}

func TestGenerateGoPRDescriptionLinksThePreparedMigration(t *testing.T) {
	t.Parallel()

	// given
	migrations := []golang.MigrationPrepared{migrationRow("lib", true, "")}

	// when
	description := golang.GenerateGoPRDescription("1.27.1", false, false, true, migrations)

	// then
	// The reader needs a link, not a branch name to go and find: the whole
	// point of preparing the branch is that nothing else has to be done by hand.
	assert.Contains(t,
		description,
		"[Create pull request](https://github.com/example/repo/compare/"+
			"main...chore/migrate-example-lib-v3?expand=1)",
	)
	assert.Contains(t, description, "| Migration |", "the column is there when it has something to say")
	assert.Contains(t, description, "A branch carrying the mechanical half of the migration")
	// The notice stops being a heads-up once there is something to click.
	assert.NotContains(t, description, "This is a notice, not a finding")
}

func TestGenerateGoPRDescriptionKeepsThreeColumnsWhenNothingWasPrepared(t *testing.T) {
	t.Parallel()

	// given
	migrations := golang.UnpreparedMigrations(golang.ParseMajorsAvailable(scriptOutputFixture))

	// when
	description := golang.GenerateGoPRDescription("1.27.1", false, false, true, migrations)

	// then
	// A column of dashes on every pull request is the line readers stop
	// seeing, and it would then be there on the one where it mattered.
	assert.NotContains(t, description, "| Migration |")
	assert.Contains(t, description, "| Module | In use | Newer major |")
	assert.Contains(t, description, "This is a notice, not a finding")
}

func TestGenerateGoPRDescriptionRendersADashWithoutAProviderPage(t *testing.T) {
	t.Parallel()

	// given: a provider whose "new pull request" page cannot be constructed, so
	// there is a branch to open one for but no link to it.
	prepared := migrationRow("lib", true, "")
	prepared.URL = ""
	notPrepared := migrationRow("alpha", false, "the migration changed nothing")

	// when
	description := golang.GenerateGoPRDescription(
		"1.27.1", false, false, true, []golang.MigrationPrepared{prepared, notPrepared},
	)

	// then
	assert.Contains(t, description, "| Migration |", "one prepared row is enough for the column")
	assert.Equal(t, 2, strings.Count(description, "| — |"),
		"a row with no link, and a row with no preparation at all")

	// What the run could not do is said underneath the table, where there is
	// room for a sentence rather than a cell.
	assert.Contains(t, description, "- `github.com/example/alpha`: the migration changed nothing")
}
