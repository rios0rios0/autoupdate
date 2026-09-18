package golang

import (
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
func writeMajorsAvailableSection(sb *strings.Builder, majors []MajorAvailable) {
	if len(majors) == 0 {
		return
	}

	sb.WriteString("\n### Newer major versions available (not in this PR)\n\n")
	sb.WriteString(
		"Go publishes v2 and above under a different module path (`/v2`, `/v3`, ...), " +
			"so `go get -u` never reaches them and no automated run can. " +
			"Taking one means rewriting the imports and fixing whatever the new API changed, " +
			"which is a decision rather than a dependency bump:\n\n",
	)
	sb.WriteString("| Module | In use | Newer major |\n")
	sb.WriteString("|---|---|---|\n")

	for _, major := range majors {
		sb.WriteString("| `" + major.Path + "` | `" + major.CurrentVersion + "` | `" +
			major.NextPath + "` `" + major.NextVersion + "` |\n")
	}

	sb.WriteString(
		"\nThis is a notice, not a finding: a dependency staying on its current major " +
			"is a perfectly good answer.\n",
	)
}
