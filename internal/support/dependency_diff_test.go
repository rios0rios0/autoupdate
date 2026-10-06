package support_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
)

func TestDiffDeclaredVersions(t *testing.T) {
	t.Parallel()

	t.Run("should report only dependencies declared on both sides at different versions", func(t *testing.T) {
		t.Parallel()

		// given
		before := map[string]string{"b": "v1.0.0", "a": "v2.0.0", "same": "v1.0.0", "removed": "v1.0.0"}
		after := map[string]string{"b": "v1.1.0", "a": "v2.1.0", "same": "v1.0.0", "added": "v1.0.0"}

		// when
		changes := support.DiffDeclaredVersions(entities.SubjectGoModule, before, after)

		// then
		assert.Equal(t, []entities.DependencyChange{
			goModuleChange("a", "v2.0.0", "v2.1.0"),
			goModuleChange("b", "v1.0.0", "v1.1.0"),
		}, changes)
	})

	t.Run("should match names the way the subject does", func(t *testing.T) {
		t.Parallel()

		// given: pip freeze may respell a distribution between runs
		before := map[string]string{"Foo_Bar": "1.0.0"}
		after := map[string]string{"foo-bar": "1.1.0"}

		// when
		changes := support.DiffDeclaredVersions(entities.SubjectPythonPackage, before, after)

		// then
		assert.Equal(t, []entities.DependencyChange{
			{Subject: entities.SubjectPythonPackage, Name: "foo-bar", From: "1.0.0", To: "1.1.0"},
		}, changes)
	})

	t.Run("should report nothing when nothing moved", func(t *testing.T) {
		t.Parallel()

		// given
		versions := map[string]string{"a": "v1.0.0"}

		// when
		changes := support.DiffDeclaredVersions(entities.SubjectGoModule, versions, versions)

		// then
		assert.Empty(t, changes)
	})
}

func TestFoldDependencyChanges(t *testing.T) {
	t.Parallel()

	t.Run("should fold one dependency into its lowest from and highest to", func(t *testing.T) {
		t.Parallel()

		// given
		changes := []entities.DependencyChange{
			goModuleChange("example.com/a", "v1.1.0", "v1.2.0"),
			goModuleChange("example.com/b", "v2.0.0", "v2.1.0"),
			goModuleChange("example.com/a", "v1.0.0", "v1.3.0-0.20261001000000-abcdef123456"),
		}

		// when
		folded := support.FoldDependencyChanges(changes)

		// then
		assert.Equal(t, []entities.DependencyChange{
			goModuleChange("example.com/a", "v1.0.0", "v1.3.0-0.20261001000000-abcdef123456"),
			goModuleChange("example.com/b", "v2.0.0", "v2.1.0"),
		}, folded)
	})

	t.Run("should keep the first version when two cannot be compared", func(t *testing.T) {
		t.Parallel()

		// given
		changes := []entities.DependencyChange{
			{Subject: entities.SubjectGitHubAction, Name: "actions/checkout", From: "v4", To: "main"},
			{Subject: entities.SubjectGitHubAction, Name: "actions/checkout", From: "v4", To: "trunk"},
		}

		// when
		folded := support.FoldDependencyChanges(changes)

		// then
		assert.Equal(t, changes[:1], folded)
	})

	t.Run("should drop a change that ends where it started", func(t *testing.T) {
		t.Parallel()

		// given
		changes := []entities.DependencyChange{goModuleChange("example.com/a", "v1.0.0", "v1.0.0")}

		// when
		folded := support.FoldDependencyChanges(changes)

		// then
		assert.Empty(t, folded)
	})
}

func TestPinChanges(t *testing.T) {
	t.Parallel()

	firstLine := func(content string) string {
		line, _, _ := strings.Cut(strings.TrimSpace(content), "\n")
		if line == "system" {
			return ""
		}
		return line
	}

	t.Run("should report a pin that moved", func(t *testing.T) {
		t.Parallel()

		// given
		file := support.ModifiedFile{Path: ".python-version", Before: []byte("3.12.1\n"), After: []byte("3.13.1\n")}

		// when
		changes := support.PinChanges(file, entities.SubjectPythonVersion, firstLine)

		// then
		assert.Equal(t, []entities.DependencyChange{
			{Subject: entities.SubjectPythonVersion, From: "3.12.1", To: "3.13.1"},
		}, changes)
	})

	t.Run("should report nothing for a pin that did not move or is not a version", func(t *testing.T) {
		t.Parallel()

		testCases := []support.ModifiedFile{
			{Path: ".python-version", Before: []byte("3.12.1\n"), After: []byte("3.12.1\r\n")},
			{Path: ".python-version", Before: []byte("system\n"), After: []byte("3.13.1\n")},
		}

		for _, file := range testCases {
			// when
			changes := support.PinChanges(file, entities.SubjectPythonVersion, firstLine)

			// then
			assert.Empty(t, changes)
		}
	})
}
