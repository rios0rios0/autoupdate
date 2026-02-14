package entities

import "strings"

const (
	unreleasedHeading = "## [Unreleased]"
	changedSubheading = "### Changed"
	h2Prefix          = "## ["
	bulletPrefix      = "- "
)

// InsertChangelogEntry inserts one or more bullet entries into the
// "## [Unreleased]" / "### Changed" section of a Keep-a-Changelog
// formatted string.
//
// Behaviour:
//   - If "## [Unreleased]" is missing, the content is returned unchanged.
//   - If "### Changed" already exists under Unreleased, the entries are
//     appended after the last bullet line in that subsection.
//   - If "### Changed" does not exist, a new subsection is created right
//     after the "## [Unreleased]" line.
func InsertChangelogEntry(content string, entries []string) string {
	if len(entries) == 0 {
		return content
	}

	lines := strings.Split(content, "\n")

	unreleasedIdx := findUnreleasedIndex(lines)
	if unreleasedIdx < 0 {
		return content // no Unreleased section
	}

	// Find the boundary of the Unreleased section (next ## [ heading or EOF).
	nextH2Idx := findNextH2Index(lines, unreleasedIdx)

	// Look for an existing ### Changed subsection inside the Unreleased region.
	changedIdx := findChangedIndex(lines, unreleasedIdx, nextH2Idx)

	bulletLines := make([]string, 0, len(entries))
	bulletLines = append(bulletLines, entries...)

	if changedIdx >= 0 {
		insertAfter := findLastBullet(lines, changedIdx, nextH2Idx)
		lines = insertLines(lines, insertAfter+1, bulletLines)
	} else {
		// No ### Changed subsection — create one after ## [Unreleased].
		block := []string{"", changedSubheading, ""}
		block = append(block, bulletLines...)
		lines = insertLines(lines, unreleasedIdx+1, block)
	}

	return strings.Join(lines, "\n")
}

// findUnreleasedIndex returns the line index of the "## [Unreleased]"
// heading, or -1 if not found.
func findUnreleasedIndex(lines []string) int {
	for i, line := range lines {
		if strings.TrimSpace(line) == unreleasedHeading {
			return i
		}
	}
	return -1
}

// findNextH2Index returns the line index of the next "## [" heading after
// startIdx, or len(lines) if there is none.
func findNextH2Index(lines []string, startIdx int) int {
	for i := startIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), h2Prefix) {
			return i
		}
	}
	return len(lines)
}

// findChangedIndex returns the line index of the "### Changed" subsection
// between startIdx and endIdx, or -1 if not found.
func findChangedIndex(lines []string, startIdx, endIdx int) int {
	for i := startIdx + 1; i < endIdx; i++ {
		if strings.TrimSpace(lines[i]) == changedSubheading {
			return i
		}
	}
	return -1
}

// findLastBullet returns the index of the last bullet line in the
// ### Changed subsection, starting from changedIdx.
func findLastBullet(lines []string, changedIdx, endIdx int) int {
	insertAfter := changedIdx
	for i := changedIdx + 1; i < endIdx; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue // skip blank lines between bullets
		}
		if strings.HasPrefix(trimmed, bulletPrefix) {
			insertAfter = i
			continue
		}
		// Hit a different subsection heading or non-bullet content.
		break
	}
	return insertAfter
}

// insertLines inserts extra lines into slice at the given index.
func insertLines(lines []string, at int, extra []string) []string {
	result := make([]string, 0, len(lines)+len(extra))
	result = append(result, lines[:at]...)
	result = append(result, extra...)
	result = append(result, lines[at:]...)
	return result
}
