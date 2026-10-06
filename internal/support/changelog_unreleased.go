package support

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// changedSubsection is the Keep a Changelog subsection autoupdate files under.
const changedSubsection = "Changed"

// newBulletMarker is the list marker of every bullet autoupdate inserts, the one
// Keep a Changelog writes.
const newBulletMarker = "- "

// subsectionsAfterChanged are the Keep a Changelog subsections that follow
// "Changed" in the order the format prescribes; a missing "### Changed" is
// created before the first of them.
//
//nolint:gochecknoglobals // read-only lookup table
var subsectionsAfterChanged = []string{"Deprecated", "Removed", "Fixed", "Security"}

// changelogDocument is a Keep a Changelog file split into lines, with the
// bullets of its [Unreleased] section located so they can be rewritten in place.
//
// Reading and writing share this one model, and that is the point of it. The
// insertion it replaces stopped at the first line that was not a bullet, so a
// wrapped bullet's continuation line ended the list early and every new entry
// landed between a bullet and the rest of its own sentence -- gluing the tail
// onto the entry that was just written.
type changelogDocument struct {
	// lines is the content split on "\n". A file with CRLF line endings keeps
	// the "\r" on each line, so lines that are not edited come back byte for
	// byte.
	lines []string
	crlf  bool
	// start is the index of the [Unreleased] heading, or -1 when there is none;
	// end is the index of the heading that closes the section, or len(lines).
	start, end int
	bullets    []changelogBullet
}

// changelogBullet is one entry of the [Unreleased] section.
type changelogBullet struct {
	// first and last delimit the entry's lines: the bullet line itself and its
	// continuation lines, indented or not.
	first, last int
	indent      string
	marker      string
	// heading is the index of the subsection heading above the bullet, or -1
	// when the bullet sits directly under [Unreleased].
	heading int
	// text is the entry with its continuation lines folded in and its marker
	// removed.
	text string
}

// parseChangelogDocument locates the [Unreleased] section of a Keep a Changelog
// document and the bullets it holds.
func parseChangelogDocument(content string) changelogDocument {
	lines := strings.Split(content, "\n")
	doc := changelogDocument{
		lines: lines,
		crlf:  strings.HasSuffix(lines[0], "\r"),
		start: -1,
		end:   len(lines),
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, h2Prefix) {
			continue
		}
		if doc.start >= 0 {
			doc.end = i
			break
		}
		if strings.HasPrefix(trimmed, unreleasedHeading) {
			doc.start = i
		}
	}

	if doc.start >= 0 {
		doc.bullets = locateBullets(lines, doc.start+1, doc.end)
	}
	return doc
}

// locateBullets finds the bullets between two line indices. A blank line or a
// heading ends the bullet above it; any other line continues it.
func locateBullets(lines []string, from, to int) []changelogBullet {
	var (
		bullets []changelogBullet
		current *changelogBullet
	)
	heading := -1

	for i := from; i < to; i++ {
		trimmed := strings.TrimSpace(lines[i])
		switch {
		case trimmed == "":
			current = nil
		case strings.HasPrefix(trimmed, "#"):
			current = nil
			heading = i
		case isBulletLine(trimmed):
			bullets = append(bullets, changelogBullet{
				first:   i,
				last:    i,
				indent:  lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " \t"))],
				marker:  trimmed[:len(newBulletMarker)],
				heading: heading,
				text:    strings.TrimSpace(trimmed[len(newBulletMarker):]),
			})
			current = &bullets[len(bullets)-1]
		case current != nil:
			current.last = i
			current.text += " " + trimmed
		}
	}
	return bullets
}

// entries returns the pending bullets as the statements they make, marker
// included, wrapped lines folded back into one.
func (d changelogDocument) entries() []string {
	entries := make([]string, 0, len(d.bullets))
	for _, bullet := range d.bullets {
		entries = append(entries, bullet.marker+bullet.text)
	}
	return entries
}

// subsection returns the name of the subsection a bullet is filed under, or ""
// when it sits directly under [Unreleased].
func (d changelogDocument) subsection(bullet changelogBullet) string {
	if bullet.heading < 0 {
		return ""
	}
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(d.lines[bullet.heading]), "#"))
}

// apply returns the document with bullets rewritten and new bullets inserted.
//
// rewrites maps a bullet index to its new statement, without a marker; an
// empty statement removes the bullet. A rewritten bullet becomes one line that
// keeps its indentation, marker and line ending. additions are complete
// bullets, inserted after the last line -- continuation lines included -- of
// the last bullet under "### Changed", which is created when missing.
func (d changelogDocument) apply(rewrites map[int]string, additions []string) string {
	if d.start < 0 {
		return strings.Join(d.lines, "\n")
	}

	plan := d.insertionPlan(additions)
	replaced := make(map[int]int, len(rewrites))
	for index := range rewrites {
		replaced[d.bullets[index].first] = index
	}

	out := make([]string, 0, len(d.lines)+len(plan.lines))
	for i := 0; i < len(d.lines); i++ {
		if plan.before == i {
			out = append(out, plan.lines...)
		}

		index, rewritten := replaced[i]
		if !rewritten {
			out = append(out, d.lines[i])
			if plan.after == i {
				out = append(out, plan.lines...)
			}
			continue
		}

		bullet := d.bullets[index]
		skipTo := bullet.last
		if text := rewrites[index]; text != "" {
			out = append(out, bullet.indent+bullet.marker+text+lineEnding(d.lines[bullet.first]))
		} else if isBlank(d.lines, bullet.first-1) && isBlank(d.lines, bullet.last+1) &&
			bullet.last+1 < len(d.lines) && plan.after != bullet.last {
			// Dropping the bullet would leave two blank lines in a row.
			skipTo = bullet.last + 1
		}
		if plan.after == bullet.last {
			out = append(out, plan.lines...)
		}
		i = skipTo
	}
	return strings.Join(out, "\n")
}

// changelogInsertion is where new bullets go and the lines that carry them.
type changelogInsertion struct {
	// before and after are line indices, at most one of them set; -1 is unset.
	before, after int
	lines         []string
}

// insertionPlan decides where new bullets go: after the last bullet under
// "### Changed", directly under that heading when it holds none, or in a new
// "### Changed" placed in Keep a Changelog order.
func (d changelogDocument) insertionPlan(additions []string) changelogInsertion {
	plan := changelogInsertion{before: -1, after: -1}
	if len(additions) == 0 {
		return plan
	}

	cr := ""
	if d.crlf {
		cr = "\r"
	}
	bullets := make([]string, 0, len(additions))
	for _, addition := range additions {
		bullets = append(bullets, addition+cr)
	}

	if heading := d.findSubsection(changedSubsection); heading >= 0 {
		return d.planUnderHeading(heading, bullets, cr)
	}
	return d.planNewSubsection(bullets, cr)
}

// planUnderHeading appends the bullets to an existing "### Changed".
func (d changelogDocument) planUnderHeading(heading int, bullets []string, cr string) changelogInsertion {
	plan := changelogInsertion{before: -1, after: -1, lines: bullets}

	last := -1
	for _, bullet := range d.bullets {
		if bullet.heading == heading {
			last = bullet.last
		}
	}
	if last >= 0 {
		plan.after = last
		return plan
	}

	// An empty "### Changed": the bullets go below the blank line under the
	// heading, and keep one between them and whatever follows.
	plan.after = heading
	if isBlank(d.lines, heading+1) {
		plan.after = heading + 1
	} else {
		plan.lines = append([]string{cr}, plan.lines...)
	}
	if next := plan.after + 1; next < len(d.lines) && !isBlank(d.lines, next) {
		plan.lines = append(plan.lines, cr)
	}
	return plan
}

// planNewSubsection creates "### Changed" before the first subsection Keep a
// Changelog orders after it, or at the end of the section when there is none.
func (d changelogDocument) planNewSubsection(bullets []string, cr string) changelogInsertion {
	block := append([]string{"### " + changedSubsection + cr, cr}, bullets...)

	for _, name := range subsectionsAfterChanged {
		if heading := d.findSubsection(name); heading >= 0 {
			if !isBlank(d.lines, heading-1) {
				block = append([]string{cr}, block...)
			}
			return changelogInsertion{before: heading, after: -1, lines: append(block, cr)}
		}
	}

	last := d.start
	for i := d.start + 1; i < d.end; i++ {
		if !isBlank(d.lines, i) {
			last = i
		}
	}
	block = append([]string{cr}, block...)
	if last+1 >= len(d.lines) || (last+1 == d.end && d.end < len(d.lines)) {
		block = append(block, cr)
	}
	return changelogInsertion{before: -1, after: last, lines: block}
}

// findSubsection returns the index of the named subsection heading inside the
// [Unreleased] section, or -1.
func (d changelogDocument) findSubsection(name string) int {
	for i := d.start + 1; i < d.end; i++ {
		trimmed := strings.TrimSpace(d.lines[i])
		if strings.HasPrefix(trimmed, "#") &&
			strings.EqualFold(strings.TrimSpace(strings.TrimLeft(trimmed, "#")), name) {
			return i
		}
	}
	return -1
}

// recordKeepAChangelogChanges records plain entries and dependency changes in a
// repository's CHANGELOG.md, returning true when the file was written. A
// repository without the file, or without an [Unreleased] section, is left
// alone.
func recordKeepAChangelogChanges(
	repoDir string,
	plain []string,
	changes []entities.DependencyChange,
) bool {
	changelogPath := filepath.Clean(filepath.Join(repoDir, ChangelogFileName))
	data, err := os.ReadFile(changelogPath)
	if err != nil {
		logger.Warnf("Failed to read %s: %v", ChangelogFileName, err)
		return false
	}

	content := string(data)
	modified := mergeIntoChangelog(content, plain, changes)
	if modified == content {
		return false
	}

	writeErr := os.WriteFile( //nolint:gosec // repoDir is a controlled internal path
		changelogPath,
		[]byte(modified),
		0o600,
	)
	if writeErr != nil {
		logger.Warnf("Failed to write %s: %v", ChangelogFileName, writeErr)
		return false
	}
	return true
}

// mergeIntoChangelog returns the document with the dependency changes merged
// into the statements [Unreleased] already makes, and with the plain entries
// and the dependencies nothing mentions yet added as new bullets.
func mergeIntoChangelog(content string, plain []string, changes []entities.DependencyChange) string {
	doc := parseChangelogDocument(content)
	if doc.start < 0 {
		return content
	}

	statements := make([]pendingStatement, len(doc.bullets))
	for i, bullet := range doc.bullets {
		statements[i] = pendingStatement{
			preferred: isPreferredSubsection(doc.subsection(bullet)),
		}
		// Only a top-level bullet is a statement autoupdate may rewrite: one
		// nested under another entry belongs to that entry's author.
		if bullet.indent == "" {
			statements[i].items, _ = entities.ParseDependencyEntry(bullet.text)
		}
	}

	merge := mergeDependencyChanges(statements, changes)
	additions := append(slices.Clone(plain), entities.RenderDependencyEntries(merge.additions)...)
	additions = newChangelogEntries(doc.entries(), additions)
	if len(merge.rewrites) == 0 && len(additions) == 0 {
		return content
	}
	return doc.apply(merge.rewrites, additions)
}

// isPreferredSubsection reports whether a statement filed under the given
// subsection was placed there on purpose: anything other than the "Changed"
// subsection autoupdate writes to, such as a "Security" entry for a bump that
// remediates a vulnerability.
func isPreferredSubsection(name string) bool {
	return name != "" && !strings.EqualFold(name, changedSubsection)
}

// lineEnding returns "\r" when the line carries a CRLF ending.
func lineEnding(line string) string {
	if strings.HasSuffix(line, "\r") {
		return "\r"
	}
	return ""
}

// isBlank reports whether the line at index is blank. An index outside the
// document counts as blank, so callers can look past either end.
func isBlank(lines []string, index int) bool {
	return index < 0 || index >= len(lines) || strings.TrimSpace(lines[index]) == ""
}
