package support

import (
	"strings"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// unreleasedHeading opens the section holding the changes that have not been
// released yet. gitforge writes there, so it is also the only section a
// duplicate can land in.
const unreleasedHeading = "## [Unreleased]"

// h2Prefix opens any changelog section. The heading following [Unreleased]
// closes it, which is what keeps released notes out of the comparison.
const h2Prefix = "## "

// bulletMarkers are the Markdown list markers a changelog entry can start with.
// Keep a Changelog writes "-", and so does gitforge, but a repository whose
// changelog was written by hand may use "*" for the very same statement.
//
//nolint:gochecknoglobals // read-only lookup table
var bulletMarkers = []string{"- ", "* "}

// newChangelogEntries returns the entries that recorded does not already state,
// with the repeats inside entries itself collapsed.
//
// It exists because autoupdate runs unattended, on a schedule, against the same
// repositories: an entry it wrote yesterday is merged into the default branch
// by the time it looks again, so an unfiltered insert restates it verbatim on
// every run until the next release moves the section away. Nothing downstream
// catches that -- gitforge's InsertChangelogEntry appends whatever it is
// handed -- so the check has to happen here, in the one place every updater
// funnels through.
//
// Two entries are the same statement when they normalize to the same text.
// Deliberately nothing fuzzier: an entry naming a different version
// ("from 3.13 to 3.14" after "from 3.12 to 3.13") describes a second, real
// upgrade, and a similarity threshold that collapsed those would silently drop
// a change the repository actually took.
func newChangelogEntries(recorded, entries []string) []string {
	if len(entries) == 0 {
		return entries
	}

	seen := make(map[string]bool, len(recorded)+len(entries))
	for _, entry := range recorded {
		if key := normalizeChangelogEntry(entry); key != "" {
			seen[key] = true
		}
	}

	fresh := make([]string, 0, len(entries))
	for _, entry := range entries {
		key := normalizeChangelogEntry(entry)
		if key == "" {
			continue
		}
		if seen[key] {
			logger.Debugf("Skipping the changelog entry already recorded as pending: %s", entry)
			continue
		}
		seen[key] = true
		fresh = append(fresh, entry)
	}

	return fresh
}

// insertChangelogEntries records the entries in a Keep a Changelog document,
// leaving out the ones its [Unreleased] section already states.
//
// Every edit of a CHANGELOG.md goes through here rather than calling
// entities.InsertChangelogEntry directly, so the duplicate check cannot be
// bypassed by a new call site.
func insertChangelogEntries(content string, entries []string) string {
	fresh := newChangelogEntries(unreleasedEntries(content), entries)
	if len(fresh) == 0 {
		return content
	}
	return entities.InsertChangelogEntry(content, fresh)
}

// unreleasedEntries returns the bullets a Keep a Changelog document already
// records under [Unreleased], one string per bullet with wrapped continuation
// lines folded back into it.
//
// Only the pending section is read. A bullet under a released heading describes
// something that has already shipped, and the same dependency moving again
// after that release is a new fact the changelog is supposed to state.
func unreleasedEntries(content string) []string {
	var (
		entries []string
		current strings.Builder
		inside  bool
	)

	flush := func() {
		if current.Len() > 0 {
			entries = append(entries, current.String())
			current.Reset()
		}
	}

	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)

		// "### Changed" is not an H2, so a subsection never closes the section.
		if strings.HasPrefix(trimmed, h2Prefix) {
			flush()
			inside = strings.HasPrefix(trimmed, unreleasedHeading)
			continue
		}
		if !inside {
			continue
		}

		switch {
		case isBulletLine(trimmed):
			flush()
			current.WriteString(trimmed)
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			// A blank line or a subheading ends the bullet above it, and is
			// itself not an entry.
			flush()
		case current.Len() > 0:
			// An indented continuation of the bullet above: changelogs wrap, so
			// a single statement can span several lines and still has to
			// compare equal to the one-line entry an updater offers.
			current.WriteString(" " + trimmed)
		}
	}
	flush()

	return entries
}

// isBulletLine reports whether an already-trimmed line opens a list item.
func isBulletLine(trimmed string) bool {
	for _, marker := range bulletMarkers {
		if strings.HasPrefix(trimmed, marker) {
			return true
		}
	}
	return false
}

// normalizeChangelogEntry reduces an entry to the form two spellings of the
// same statement share: no list marker, no code formatting, one space between
// words and no case.
//
// Folding the whitespace is what lets a wrapped bullet read from a changelog
// match the single-line entry an updater produces, and dropping the backticks
// keeps a hand-reformatted entry from being restated as a new one.
func normalizeChangelogEntry(entry string) string {
	text := strings.TrimSpace(entry)
	for _, marker := range bulletMarkers {
		if body, found := strings.CutPrefix(text, marker); found {
			text = body
			break
		}
	}

	text = strings.ReplaceAll(text, "`", "")
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}
