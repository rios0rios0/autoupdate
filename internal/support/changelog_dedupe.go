package support

import (
	"strings"

	logger "github.com/sirupsen/logrus"
)

// unreleasedHeading opens the section holding the changes that have not been
// released yet. autoupdate writes there, so it is also the only section a
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
// catches that, so the check has to happen here, in the one place every updater
// funnels through.
//
// Two entries are the same statement when they normalize to the same text.
// Deliberately nothing fuzzier for these plain entries: a similarity threshold
// cannot tell a reworded statement from a different one. Statements that name
// dependencies are not compared here at all -- [mergeDependencyChanges] matches
// them dependency by dependency, which is what lets a second upgrade of the same
// library update the pending statement instead of being written beside it.
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
// Every edit of a CHANGELOG.md goes through the same [changelogDocument], so
// the duplicate check cannot be bypassed by a new call site, and an entry is
// never inserted between a wrapped bullet and its continuation line.
func insertChangelogEntries(content string, entries []string) string {
	return mergeIntoChangelog(content, entries, nil)
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
