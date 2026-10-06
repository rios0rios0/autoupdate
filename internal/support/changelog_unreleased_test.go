package support_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
)

func TestInsertChangelogEntries(t *testing.T) {
	t.Parallel()

	t.Run("should insert entries under the Unreleased section", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n"

		// when
		result := support.InsertChangelogEntries(content, []string{"- added new feature X"})

		// then
		assert.Equal(t,
			"# Changelog\n\n## [Unreleased]\n\n### Changed\n\n- added new feature X\n\n## [1.0.0] - 2026-01-01\n",
			result)
	})

	t.Run("should return the content unchanged when no Unreleased section exists", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Changelog\n\n## [1.0.0] - 2026-01-01\n"

		// when
		result := support.InsertChangelogEntries(content, []string{"- added something"})

		// then
		assert.Equal(t, content, result)
	})

	t.Run("should insert after the continuation line of a wrapped last bullet", func(t *testing.T) {
		t.Parallel()

		// given: the shape that had a new entry glued onto the tail of the
		// bullet above it, in this repository's own 1.0 changelog
		content := "## [Unreleased]\n\n### Changed\n\n" +
			"- changed cleanup to run only after the same-day pull request check has passed, so a pull request is\n" +
			"  never closed without a replacement being opened for it\n\n" +
			"### Fixed\n\n- fixed something\n\n## [1.0.0] - 2026-01-01\n"

		// when
		result := support.InsertChangelogEntries(content,
			[]string{"- changed the Go module `golang.org/x/mod` from `v0.40.0` to `v0.41.0`"})

		// then
		assert.Equal(t, "## [Unreleased]\n\n### Changed\n\n"+
			"- changed cleanup to run only after the same-day pull request check has passed, so a pull request is\n"+
			"  never closed without a replacement being opened for it\n"+
			"- changed the Go module `golang.org/x/mod` from `v0.40.0` to `v0.41.0`\n\n"+
			"### Fixed\n\n- fixed something\n\n## [1.0.0] - 2026-01-01\n", result)
	})

	t.Run("should treat an unindented line as a continuation", func(t *testing.T) {
		t.Parallel()

		// given
		content := "## [Unreleased]\n\n### Changed\n\n- changed the first half\nand the second half\n\n## [1.0.0]\n"

		// when
		result := support.InsertChangelogEntries(content, []string{"- changed another thing"})

		// then
		assert.Equal(t,
			"## [Unreleased]\n\n### Changed\n\n- changed the first half\nand the second half\n"+
				"- changed another thing\n\n## [1.0.0]\n",
			result)
	})

	t.Run("should keep a CRLF file CRLF", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Changelog\r\n\r\n## [Unreleased]\r\n\r\n## [1.0.0]\r\n"

		// when
		result := support.InsertChangelogEntries(content, []string{"- changed a thing"})

		// then
		assert.Equal(t,
			"# Changelog\r\n\r\n## [Unreleased]\r\n\r\n### Changed\r\n\r\n- changed a thing\r\n\r\n## [1.0.0]\r\n",
			result)
	})

	t.Run("should create Changed after Added and before Fixed", func(t *testing.T) {
		t.Parallel()

		// given
		content := "## [Unreleased]\n\n### Added\n\n- added a\n\n### Fixed\n\n- fixed b\n\n## [1.0.0]\n"

		// when
		result := support.InsertChangelogEntries(content, []string{"- changed c"})

		// then
		assert.Equal(t,
			"## [Unreleased]\n\n### Added\n\n- added a\n\n### Changed\n\n- changed c\n\n"+
				"### Fixed\n\n- fixed b\n\n## [1.0.0]\n",
			result)
	})

	t.Run("should create Changed at the end of a section holding only Added", func(t *testing.T) {
		t.Parallel()

		// given
		content := "## [Unreleased]\n\n### Added\n\n- added a\n\n## [1.0.0]\n"

		// when
		result := support.InsertChangelogEntries(content, []string{"- changed c"})

		// then
		assert.Equal(t,
			"## [Unreleased]\n\n### Added\n\n- added a\n\n### Changed\n\n- changed c\n\n## [1.0.0]\n",
			result)
	})

	t.Run("should fill an empty Changed subsection", func(t *testing.T) {
		t.Parallel()

		// given
		content := "## [Unreleased]\n\n### Changed\n\n## [1.0.0]\n"

		// when
		result := support.InsertChangelogEntries(content, []string{"- changed c"})

		// then
		assert.Equal(t, "## [Unreleased]\n\n### Changed\n\n- changed c\n\n## [1.0.0]\n", result)
	})

	t.Run("should add the section's content at the end of a file without a newline", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Changelog\n\n## [Unreleased]"

		// when
		result := support.InsertChangelogEntries(content, []string{"- changed c"})

		// then
		assert.Equal(t, "# Changelog\n\n## [Unreleased]\n\n### Changed\n\n- changed c\n", result)
	})
}

func TestMergeIntoChangelog(t *testing.T) {
	t.Parallel()

	t.Run("should update a pending dependency in place and add only new ones", func(t *testing.T) {
		t.Parallel()

		// given
		content := "## [Unreleased]\n\n### Changed\n\n" +
			"- changed the Go modules `github.com/spf13/cobra` from `v1.8.0` to `v1.9.0` and " +
			"`golang.org/x/mod` from `v0.20.0` to `v0.21.0`\n\n## [1.0.0]\n"
		changes := []entitiesChange{
			goModuleChange("github.com/spf13/cobra", "v1.9.0", "v1.10.2"),
			goModuleChange("gopkg.in/yaml.v3", "v3.0.0", "v3.0.1"),
		}

		// when
		result := support.MergeIntoChangelog(content, nil, changes)

		// then
		assert.Equal(t, "## [Unreleased]\n\n### Changed\n\n"+
			"- changed the Go modules `github.com/spf13/cobra` from `v1.8.0` to `v1.10.2` and "+
			"`golang.org/x/mod` from `v0.20.0` to `v0.21.0`\n"+
			"- changed the Go module `gopkg.in/yaml.v3` from `v3.0.0` to `v3.0.1`\n\n## [1.0.0]\n", result)
	})

	t.Run("should change nothing when the same changes are merged again", func(t *testing.T) {
		t.Parallel()

		// given
		changes := []entitiesChange{
			goModuleChange("github.com/spf13/cobra", "v1.9.0", "v1.10.2"),
			goModuleChange("gopkg.in/yaml.v3", "v3.0.0", "v3.0.1"),
		}
		once := support.MergeIntoChangelog(baseChangelog, nil, changes)

		// when
		twice := support.MergeIntoChangelog(once, nil, changes)

		// then
		assert.Equal(t, once, twice)
	})

	t.Run("should write new dependencies five to a line", func(t *testing.T) {
		t.Parallel()

		// given
		var changes []entitiesChange
		for _, name := range []string{"a", "b", "c", "d", "e", "f", "g"} {
			changes = append(changes, goModuleChange("example.com/"+name, "v1.0.0", "v1.1.0"))
		}

		// when
		result := support.MergeIntoChangelog(baseChangelog, nil, changes)

		// then
		assert.Contains(t, result, "- changed the Go modules `example.com/a` from `v1.0.0` to `v1.1.0`, "+
			"`example.com/b` from `v1.0.0` to `v1.1.0`, `example.com/c` from `v1.0.0` to `v1.1.0`, "+
			"`example.com/d` from `v1.0.0` to `v1.1.0` and `example.com/e` from `v1.0.0` to `v1.1.0`\n"+
			"- changed the Go modules `example.com/f` from `v1.0.0` to `v1.1.0` and "+
			"`example.com/g` from `v1.0.0` to `v1.1.0`\n")
	})

	t.Run("should keep the mention a maintainer filed under another subsection", func(t *testing.T) {
		t.Parallel()

		// given: the same module filed twice, once under Security on purpose
		content := "## [Unreleased]\n\n### Changed\n\n" +
			"- changed the Go modules `golang.org/x/net` from `v0.30.0` to `v0.31.0` and " +
			"`golang.org/x/text` from `v0.20.0` to `v0.21.0`\n\n" +
			"### Security\n\n- changed the Go module `golang.org/x/net` from `v0.29.0` to `v0.31.0`\n\n" +
			"## [1.0.0]\n"
		changes := []entitiesChange{goModuleChange("golang.org/x/net", "v0.31.0", "v0.33.0")}

		// when
		result := support.MergeIntoChangelog(content, nil, changes)

		// then: the Security entry keeps it, from the lowest version either
		// mention recorded, and the other entry no longer names it
		assert.Equal(t, "## [Unreleased]\n\n### Changed\n\n"+
			"- changed the Go module `golang.org/x/text` from `v0.20.0` to `v0.21.0`\n\n"+
			"### Security\n\n- changed the Go module `golang.org/x/net` from `v0.29.0` to `v0.33.0`\n\n"+
			"## [1.0.0]\n", result)
	})

	t.Run("should remove a statement left naming nothing", func(t *testing.T) {
		t.Parallel()

		// given
		content := "## [Unreleased]\n\n### Changed\n\n" +
			"- changed the Go module `golang.org/x/net` from `v0.30.0` to `v0.31.0`\n" +
			"- changed the Go module `golang.org/x/net` from `v0.31.0` to `v0.32.0`\n\n## [1.0.0]\n"
		changes := []entitiesChange{goModuleChange("golang.org/x/net", "v0.32.0", "v0.33.0")}

		// when
		result := support.MergeIntoChangelog(content, nil, changes)

		// then
		assert.Equal(t, "## [Unreleased]\n\n### Changed\n\n"+
			"- changed the Go module `golang.org/x/net` from `v0.30.0` to `v0.33.0`\n\n## [1.0.0]\n", result)
	})

	t.Run("should drop a dependency that ends the cycle where it started", func(t *testing.T) {
		t.Parallel()

		// given
		content := "## [Unreleased]\n\n### Changed\n\n" +
			"- changed the Go modules `example.com/a` from `v1.0.0` to `v1.1.0` and " +
			"`example.com/b` from `v2.0.0` to `v2.1.0`\n\n## [1.0.0]\n"
		changes := []entitiesChange{goModuleChange("example.com/a", "v1.1.0", "v1.0.0")}

		// when
		result := support.MergeIntoChangelog(content, nil, changes)

		// then
		assert.Equal(t, "## [Unreleased]\n\n### Changed\n\n"+
			"- changed the Go module `example.com/b` from `v2.0.0` to `v2.1.0`\n\n## [1.0.0]\n", result)
	})

	t.Run("should take the version a run leaves even when it is lower", func(t *testing.T) {
		t.Parallel()

		// given: a Go major-version hold dragged the requirement back down
		content := "## [Unreleased]\n\n### Changed\n\n" +
			"- changed the Go module `example.com/a` from `v1.0.0` to `v1.5.0`\n\n## [1.0.0]\n"
		changes := []entitiesChange{goModuleChange("example.com/a", "v1.5.0", "v1.4.0")}

		// when
		result := support.MergeIntoChangelog(content, nil, changes)

		// then
		assert.Contains(t, result, "- changed the Go module `example.com/a` from `v1.0.0` to `v1.4.0`\n")
	})

	t.Run("should take a lower version for a pin as well", func(t *testing.T) {
		t.Parallel()

		// given
		content := "## [Unreleased]\n\n### Changed\n\n" +
			"- changed the Go version from `1.26.0` to `1.27.1`\n\n## [1.0.0]\n"
		changes := []entitiesChange{{Subject: entities.SubjectGoVersion, From: "1.27.1", To: "1.27.0"}}

		// when
		result := support.MergeIntoChangelog(content, nil, changes)

		// then
		assert.Contains(t, result, "- changed the Go version from `1.26.0` to `1.27.0`\n")
	})

	t.Run("should leave prose, generic statements and other subjects untouched", func(t *testing.T) {
		t.Parallel()

		// given
		content := "## [Unreleased]\n\n### Changed\n\n" +
			"- changed the Go module dependencies to their latest versions\n" +
			"- changed the retry loop so `golang` builds wait for the lock\n" +
			"- changed the pipeline runtime `golang` from `1.25` to `1.26`\n" +
			"  - a nested note: changed the Go module `golang` from `v1` to `v2`\n\n## [1.0.0]\n"
		changes := []entitiesChange{{
			Subject: entities.SubjectDockerBaseImage, Name: "golang", From: "1.25", To: "1.26",
		}}

		// when
		result := support.MergeIntoChangelog(content, nil, changes)

		// then: every pending line is byte-identical, and the image is new
		assert.True(t, strings.HasPrefix(result, strings.TrimSuffix(content, "\n\n## [1.0.0]\n")))
		assert.Contains(t, result, "- changed the Docker base image `golang` from `1.25` to `1.26`\n")
	})

	t.Run("should merge a per-upgrade statement written before this grammar", func(t *testing.T) {
		t.Parallel()

		// given
		content := "## [Unreleased]\n\n### Changed\n\n" +
			"* changed the golang pipeline version from `1.24` to `1.25`\n\n## [1.0.0]\n"
		changes := []entitiesChange{{
			Subject: entities.SubjectPipelineRuntime, Name: "golang", From: "1.25", To: "1.26",
		}}

		// when
		result := support.MergeIntoChangelog(content, nil, changes)

		// then: rewritten in the current wording, keeping the bullet marker
		assert.Equal(t, "## [Unreleased]\n\n### Changed\n\n"+
			"* changed the pipeline runtime `golang` from `1.24` to `1.26`\n\n## [1.0.0]\n", result)
	})

	t.Run("should keep the line endings of a rewritten CRLF bullet", func(t *testing.T) {
		t.Parallel()

		// given
		content := "## [Unreleased]\r\n\r\n### Changed\r\n\r\n" +
			"- changed the Go module `example.com/a` from `v1.0.0` to `v1.1.0`\r\n\r\n## [1.0.0]\r\n"
		changes := []entitiesChange{goModuleChange("example.com/a", "v1.1.0", "v1.2.0")}

		// when
		result := support.MergeIntoChangelog(content, nil, changes)

		// then
		assert.Equal(t, "## [Unreleased]\r\n\r\n### Changed\r\n\r\n"+
			"- changed the Go module `example.com/a` from `v1.0.0` to `v1.2.0`\r\n\r\n## [1.0.0]\r\n", result)
	})
}
