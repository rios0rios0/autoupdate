package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/domain"
)

func TestInsertChangelogEntry(t *testing.T) {
	t.Parallel()

	t.Run("should insert entry into empty Unreleased section", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n\n### Added\n\n- initial release\n"
		entries := []string{"- changed the Go version to `1.25.7` and updated all module dependencies"}

		// when
		result := domain.InsertChangelogEntry(content, entries)

		// then
		assert.Contains(t, result, "## [Unreleased]\n\n### Changed\n\n- changed the Go version")
		assert.Contains(t, result, "## [1.0.0] - 2026-01-01")
	})

	t.Run("should append entry to existing Changed subsection", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Changelog\n\n## [Unreleased]\n\n### Changed\n\n- existing change\n\n## [1.0.0] - 2026-01-01\n"
		entries := []string{"- changed the Terraform module networking from v1.0.0 to v2.0.0"}

		// when
		result := domain.InsertChangelogEntry(content, entries)

		// then
		assert.Contains(t, result, "- existing change\n- changed the Terraform module networking")
		assert.Contains(t, result, "## [1.0.0] - 2026-01-01")
	})

	t.Run("should insert Changed subsection when other subsections exist", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Changelog\n\n## [Unreleased]\n\n### Fixed\n\n- fixed a bug\n\n## [1.0.0] - 2026-01-01\n"
		entries := []string{"- changed the Go module dependencies to their latest versions"}

		// when
		result := domain.InsertChangelogEntry(content, entries)

		// then
		assert.Contains(t, result, "## [Unreleased]\n\n### Changed\n\n- changed the Go module")
		assert.Contains(t, result, "### Fixed")
	})

	t.Run("should return content unchanged when Unreleased section is missing", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Changelog\n\n## [1.0.0] - 2026-01-01\n\n### Added\n\n- initial release\n"
		entries := []string{"- changed something"}

		// when
		result := domain.InsertChangelogEntry(content, entries)

		// then
		assert.Equal(t, content, result)
	})

	t.Run("should return content unchanged when entries slice is empty", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n"

		// when
		result := domain.InsertChangelogEntry(content, nil)

		// then
		assert.Equal(t, content, result)
	})

	t.Run("should handle multiple entries at once", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n"
		entries := []string{
			"- changed the Terraform module networking from v1.0.0 to v2.0.0",
			"- changed the Terraform module compute from v3.0.0 to v4.0.0",
		}

		// when
		result := domain.InsertChangelogEntry(content, entries)

		// then
		assert.Contains(t, result, "### Changed\n\n- changed the Terraform module networking")
		assert.Contains(t, result, "- changed the Terraform module compute")
	})

	t.Run("should handle Unreleased at end of file with no next section", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Changelog\n\n## [Unreleased]\n"
		entries := []string{"- changed something"}

		// when
		result := domain.InsertChangelogEntry(content, entries)

		// then
		assert.Contains(t, result, "## [Unreleased]\n\n### Changed\n\n- changed something")
	})

	t.Run("should append to Changed with multiple existing bullets", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Changelog\n\n## [Unreleased]\n\n### Changed\n\n- first change\n- second change\n\n## [1.0.0] - 2026-01-01\n"
		entries := []string{"- third change"}

		// when
		result := domain.InsertChangelogEntry(content, entries)

		// then
		assert.Contains(t, result, "- second change\n- third change")
	})
}
