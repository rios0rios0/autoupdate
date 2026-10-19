package golang

import (
	"slices"
	"sort"
	"strings"
)

// majorAvailableMarker prefixes the line support.GoMajorAvailableScript echoes
// for each newer major it found. The fields after it are separated by "|",
// which cannot occur in a module path or a semantic version.
const majorAvailableMarker = "GO_MAJOR_AVAILABLE="

// majorAvailableFields is how many "|"-separated fields the marker carries:
// module directory, current path, current version, newer path, newer version.
const majorAvailableFields = 5

// MajorAvailable is a newer major of a direct requirement, published under the
// separate module path Go's semantic import versioning gives it.
type MajorAvailable struct {
	// ModuleDir is the repo-relative directory of the go.mod it was found in,
	// which is what tells them apart in a repository with nested modules.
	ModuleDir string
	// Path and CurrentVersion are the requirement as the upgraded go.mod has it.
	Path           string
	CurrentVersion string
	// NextPath and NextVersion are the newest major found, at the path it is
	// actually published under.
	NextPath    string
	NextVersion string
}

// ParseMajorsAvailable reads the markers out of an upgrade script's output.
//
// The output is also the run log, so it carries the human-readable line beside
// each marker and everything else the script said. Parsing the marker rather
// than the prose is what lets the wording of either change without breaking the
// other.
//
// Duplicates are dropped: a repository with several modules requiring the same
// dependency reports it once per module, and a reader wants the fact, not the
// count. They are returned sorted so the same findings always render the same
// way -- a pull request description that reshuffles between runs reads as a
// change when nothing changed.
func ParseMajorsAvailable(output string) []MajorAvailable {
	seen := make(map[string]struct{})
	found := make([]MajorAvailable, 0)

	for line := range strings.SplitSeq(output, "\n") {
		_, marker, ok := strings.Cut(strings.TrimSpace(line), majorAvailableMarker)
		if !ok {
			continue
		}

		fields := strings.Split(marker, "|")
		if len(fields) != majorAvailableFields {
			continue
		}

		key := fields[1] + "|" + fields[3] + "|" + fields[4]
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}

		found = append(found, MajorAvailable{
			ModuleDir:      fields[0],
			Path:           fields[1],
			CurrentVersion: fields[2],
			NextPath:       fields[3],
			NextVersion:    fields[4],
		})
	}

	sort.Slice(found, func(i, j int) bool {
		if found[i].Path != found[j].Path {
			return found[i].Path < found[j].Path
		}
		return found[i].NextPath < found[j].NextPath
	})

	return found
}

// writeMajorsAvailableSection renders the findings into the pull request
// description, and writes nothing at all when there are none.
//
// Nothing is the right output for an empty list. A section saying "no newer
// majors" on every pull request is a line readers stop seeing within a week,
// and it would then be there on the one where it mattered.
//
// The Migration column appears only when a run prepared something to link: a
// column of dashes on every pull request is the same line readers stop seeing,
// and preparation can be off, or impossible for the repository in front of it.
func writeMajorsAvailableSection(sb *strings.Builder, migrations []MigrationPrepared) {
	if len(migrations) == 0 {
		return
	}

	sb.WriteString("\n### Newer major versions available (not in this PR)\n\n")
	sb.WriteString(
		"Go publishes v2 and above under a different module path (`/v2`, `/v3`, ...), " +
			"so `go get -u` never reaches them and no automated run can. " +
			"Taking one means rewriting the imports and fixing whatever the new API changed, " +
			"which is a decision rather than a dependency bump:\n\n",
	)

	withBranch := hasPreparedMigration(migrations)
	if withBranch {
		sb.WriteString(
			"A branch carrying the mechanical half of the migration -- every import rewritten, " +
				"every module moved onto the new path and tidied -- is ready wherever one is " +
				"linked. The API changes are not in it: open the pull request and finish the " +
				"migration there.\n\n",
		)
	}

	writeMigrationsTable(sb, migrations, withBranch)

	if notes := migrationNotes(migrations); len(notes) > 0 {
		sb.WriteString("\n")
		for _, note := range notes {
			sb.WriteString("- `" + note.path + "`: " + note.text + "\n")
		}
	}

	if withBranch {
		sb.WriteString(
			"\nA dependency staying on its current major is still a perfectly good answer: " +
				"every branch above is a proposal, and none of them merges itself.\n",
		)
	} else {
		sb.WriteString(
			"\nThis is a notice, not a finding: a dependency staying on its current major " +
				"is a perfectly good answer.\n",
		)
	}
}

// hasPreparedMigration reports whether any finding has a branch behind it.
func hasPreparedMigration(migrations []MigrationPrepared) bool {
	return slices.ContainsFunc(migrations, func(migration MigrationPrepared) bool {
		return migration.Prepared
	})
}

// writeMigrationsTable renders one row per finding.
func writeMigrationsTable(
	sb *strings.Builder, migrations []MigrationPrepared, withBranch bool,
) {
	if withBranch {
		sb.WriteString("| Module | In use | Newer major | Migration |\n")
		sb.WriteString("|---|---|---|---|\n")
	} else {
		sb.WriteString("| Module | In use | Newer major |\n")
		sb.WriteString("|---|---|---|\n")
	}

	for _, migration := range migrations {
		sb.WriteString("| `" + migration.Path + "` | `" + migration.CurrentVersion + "` | `" +
			migration.NextPath + "` `" + migration.NextVersion + "` ")
		if withBranch {
			sb.WriteString("| " + migrationCell(migration) + " ")
		}
		sb.WriteString("|\n")
	}
}

// migrationCell renders what the reader can do about one finding: open the pull
// request for the prepared branch, or nothing at all.
//
// The link is what the provider's own "new pull request" page needs -- a source
// branch that already exists and a target -- so it works from static text, with
// no API call and no token behind it.
func migrationCell(migration MigrationPrepared) string {
	if !migration.Prepared || migration.URL == "" {
		return "—"
	}

	return "[Create pull request](" + migration.URL + ")"
}

// migrationNoteRow is what the run had to say about one finding, with the
// module it said it about.
type migrationNoteRow struct {
	path string
	text string
}

// migrationNotes collects what the run had to say about the findings that were
// not prepared, and about the ones that were but come with a caveat.
func migrationNotes(migrations []MigrationPrepared) []migrationNoteRow {
	notes := make([]migrationNoteRow, 0, len(migrations))
	for _, migration := range migrations {
		text := strings.TrimSpace(migration.Note)
		if text == "" {
			continue
		}
		notes = append(notes, migrationNoteRow{path: migration.Path, text: text})
	}

	return notes
}
