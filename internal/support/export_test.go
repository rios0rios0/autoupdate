package support

import "github.com/rios0rios0/autoupdate/internal/domain/entities"

// InsertChangelogEntries is exported for testing.
func InsertChangelogEntries(content string, entries []string) string {
	return insertChangelogEntries(content, entries)
}

// MergeIntoChangelog is exported for testing.
func MergeIntoChangelog(content string, plain []string, changes []entities.DependencyChange) string {
	return mergeIntoChangelog(content, plain, changes)
}
