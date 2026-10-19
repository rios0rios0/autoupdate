package support

import (
	"io"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// InsertChangelogEntries is exported for testing.
func InsertChangelogEntries(content string, entries []string) string {
	return insertChangelogEntries(content, entries)
}

// MergeIntoChangelog is exported for testing.
func MergeIntoChangelog(content string, plain []string, changes []entities.DependencyChange) string {
	return mergeIntoChangelog(content, plain, changes)
}

// WriteGitAuthConfig is exported for testing, with the operator's gitconfig
// injected so the test does not depend on the machine it runs on.
func WriteGitAuthConfig(w io.Writer, operatorConfig []byte, providerName, token string) error {
	return writeGitAuthConfig(w, operatorConfig, providerName, token)
}

// NewSharedCacheWithSpace is exported for testing, with the free-space reading
// injected so a test can run the disk low on demand.
func NewSharedCacheWithSpace(space func(path string) (uint64, uint64, error)) (*SharedCache, error) {
	return newSharedCache(space)
}
