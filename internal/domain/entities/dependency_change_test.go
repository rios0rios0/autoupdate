package entities_test

import (
	"fmt"
	"testing"

	changelogEntities "github.com/rios0rios0/gitforge/pkg/changelog/domain/entities"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// goModule builds a Go module change, the most common statement autoupdate
// writes.
func goModule(name, from, to string) entities.DependencyChange {
	return entities.DependencyChange{Subject: entities.SubjectGoModule, Name: name, From: from, To: to}
}

// goModules builds count Go module changes named m01, m02, ...
func goModules(count int) []entities.DependencyChange {
	changes := make([]entities.DependencyChange, 0, count)
	for i := range count {
		changes = append(changes, goModule(fmt.Sprintf("example.com/m%02d", i+1), "v1.0.0", "v1.1.0"))
	}
	return changes
}

func TestRenderDependencyEntries(t *testing.T) {
	t.Parallel()

	t.Run("should name a single dependency with the singular subject", func(t *testing.T) {
		t.Parallel()

		// given
		changes := []entities.DependencyChange{goModule("github.com/spf13/cobra", "v1.8.0", "v1.9.1")}

		// when
		entries := entities.RenderDependencyEntries(changes)

		// then
		assert.Equal(t, []string{
			"- changed the Go module `github.com/spf13/cobra` from `v1.8.0` to `v1.9.1`",
		}, entries)
	})

	t.Run("should list dependencies with commas and a final and", func(t *testing.T) {
		t.Parallel()

		// given
		changes := []entities.DependencyChange{
			goModule("c.example/c", "v1.0.0", "v1.0.1"),
			goModule("a.example/a", "v1.0.0", "v1.0.1"),
			goModule("b.example/b", "v2.0.0", "v2.1.0"),
		}

		// when
		entries := entities.RenderDependencyEntries(changes)

		// then
		assert.Equal(t, []string{
			"- changed the Go modules `a.example/a` from `v1.0.0` to `v1.0.1`, " +
				"`b.example/b` from `v2.0.0` to `v2.1.0` and `c.example/c` from `v1.0.0` to `v1.0.1`",
		}, entries)
	})

	t.Run("should join two dependencies with and alone", func(t *testing.T) {
		t.Parallel()

		// given
		changes := goModules(2)

		// when
		entries := entities.RenderDependencyEntries(changes)

		// then
		require.Len(t, entries, 1)
		assert.Contains(t, entries[0], "`v1.1.0` and `example.com/m02`")
		assert.NotContains(t, entries[0], ",")
	})

	t.Run("should split into lines of at most five dependencies", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			count int
			lines []int
		}{
			{count: 1, lines: []int{1}},
			{count: 5, lines: []int{5}},
			{count: 6, lines: []int{5, 1}},
			{count: 11, lines: []int{5, 5, 1}},
		}

		for _, testCase := range testCases {
			// given
			changes := goModules(testCase.count)

			// when
			entries := entities.RenderDependencyEntries(changes)

			// then
			require.Len(t, entries, len(testCase.lines), "count %d", testCase.count)
			for i, entry := range entries {
				parsed, ok := entities.ParseDependencyEntry(entry)
				require.True(t, ok, entry)
				assert.Len(t, parsed, testCase.lines[i], entry)
			}
		}
	})

	t.Run("should sort names case-insensitively", func(t *testing.T) {
		t.Parallel()

		// given
		changes := []entities.DependencyChange{
			{Subject: entities.SubjectNuGetPackage, Name: "serilog", From: "3.0.0", To: "3.1.0"},
			{Subject: entities.SubjectNuGetPackage, Name: "Newtonsoft.Json", From: "13.0.1", To: "13.0.3"},
		}

		// when
		entries := entities.RenderDependencyEntries(changes)

		// then
		assert.Equal(t, []string{
			"- changed the NuGet packages `Newtonsoft.Json` from `13.0.1` to `13.0.3` and " +
				"`serilog` from `3.0.0` to `3.1.0`",
		}, entries)
	})

	t.Run("should write version pins first and subjects in registry order", func(t *testing.T) {
		t.Parallel()

		// given
		changes := []entities.DependencyChange{
			{Subject: entities.SubjectDockerBaseImage, Name: "golang", From: "1.26", To: "1.27"},
			goModule("golang.org/x/mod", "v0.40.0", "v0.41.0"),
			{Subject: entities.SubjectGoVersion, From: "1.26.0", To: "1.27.1"},
		}

		// when
		entries := entities.RenderDependencyEntries(changes)

		// then
		assert.Equal(t, []string{
			"- changed the Go version from `1.26.0` to `1.27.1`",
			"- changed the Go module `golang.org/x/mod` from `v0.40.0` to `v0.41.0`",
			"- changed the Docker base image `golang` from `1.26` to `1.27`",
		}, entries)
	})

	t.Run("should render nothing for no changes", func(t *testing.T) {
		t.Parallel()

		// given / when
		entries := entities.RenderDependencyEntries(nil)

		// then
		assert.Empty(t, entries)
	})
}

func TestParseDependencyEntry(t *testing.T) {
	t.Parallel()

	t.Run("should read back every statement it renders", func(t *testing.T) {
		t.Parallel()

		// given
		changes := append(goModules(7),
			entities.DependencyChange{Subject: entities.SubjectGoVersion, From: "1.26.0", To: "1.27.1"},
			entities.DependencyChange{
				Subject: entities.SubjectGitHubAction, Name: "actions/checkout", From: "v4", To: "v5",
			},
			entities.DependencyChange{
				Subject: entities.SubjectContainerImage, Name: "redis", From: "7.2-alpine", To: "7.4-alpine",
			},
		)

		// when
		var parsed []entities.DependencyChange
		for _, entry := range entities.RenderDependencyEntries(changes) {
			items, ok := entities.ParseDependencyEntry(entry)
			require.True(t, ok, entry)
			parsed = append(parsed, items...)
		}

		// then
		assert.ElementsMatch(t, changes, parsed)
	})

	t.Run("should accept the forms people reformat a statement into", func(t *testing.T) {
		t.Parallel()

		testCases := []string{
			"* Changed the Go modules `a.example/a` from `v1` to `v2`, and `b.example/b` from `v3` to `v4`.",
			"- changed the Go module `a.example/a` from `v1` to `v2` and `b.example/b` from `v3` to `v4`",
			"changed the Go modules   `a.example/a` from `v1` to `v2`,  `b.example/b` from `v3` to `v4`",
		}

		for _, entry := range testCases {
			// when
			items, ok := entities.ParseDependencyEntry(entry)

			// then
			require.True(t, ok, entry)
			assert.Equal(t, []entities.DependencyChange{
				goModule("a.example/a", "v1", "v2"),
				goModule("b.example/b", "v3", "v4"),
			}, items, entry)
		}
	})

	t.Run("should read the per-upgrade statements written before this grammar", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			entry string
			want  entities.DependencyChange
		}{
			{
				entry: "- changed the Terraform module `vpc` from `1.0.0` to `1.1.0`",
				want: entities.DependencyChange{
					Subject: entities.SubjectTerraformModule, Name: "vpc", From: "1.0.0", To: "1.1.0",
				},
			},
			{
				entry: "- changed the Docker base image `python` from `3.12-slim` to `3.13-slim`",
				want: entities.DependencyChange{
					Subject: entities.SubjectDockerBaseImage, Name: "python", From: "3.12-slim", To: "3.13-slim",
				},
			},
			{
				entry: "- changed the golang pipeline version from `1.22.0` to `1.24.1`",
				want: entities.DependencyChange{
					Subject: entities.SubjectPipelineRuntime, Name: "golang", From: "1.22.0", To: "1.24.1",
				},
			},
			{
				entry: "- changed the action:actions/checkout pipeline version from `v4` to `v5`",
				want: entities.DependencyChange{
					Subject: entities.SubjectGitHubAction, Name: "actions/checkout", From: "v4", To: "v5",
				},
			},
		}

		for _, testCase := range testCases {
			// when
			items, ok := entities.ParseDependencyEntry(testCase.entry)

			// then
			require.True(t, ok, testCase.entry)
			assert.Equal(t, []entities.DependencyChange{testCase.want}, items)
		}
	})

	t.Run("should not read prose, generic statements or malformed lists", func(t *testing.T) {
		t.Parallel()

		testCases := []string{
			"- changed the default timeout from `30s` to `60s`",
			"- changed the Go module dependencies to their latest versions",
			"- changed the Go version to `1.27.1` and updated all module dependencies",
			"- changed the Go modules `a` from `v1` to `v2` and more",
			"- changed the Go modules `a` from `v1` to v2",
			"- changed the Go modules `a` from `v1` to `v2`\n- changed the Go module `b` from `v1` to `v2`",
			"- updated the Go module `a` from `v1` to `v2`",
			"",
		}

		for _, entry := range testCases {
			// when
			items, ok := entities.ParseDependencyEntry(entry)

			// then
			assert.False(t, ok, entry)
			assert.Nil(t, items, entry)
		}
	})
}

func TestDependencyChangeKey(t *testing.T) {
	t.Parallel()

	t.Run("should match names case-insensitively", func(t *testing.T) {
		t.Parallel()

		// given
		upper := entities.DependencyChange{Subject: entities.SubjectNuGetPackage, Name: "Serilog", To: "3.1.0"}
		lower := entities.DependencyChange{Subject: entities.SubjectNuGetPackage, Name: "serilog", To: "4.0.0"}

		// when / then
		assert.Equal(t, upper.Key(), lower.Key())
	})

	t.Run("should match Python names under PEP 503", func(t *testing.T) {
		t.Parallel()

		// given
		names := []string{"Foo_Bar", "foo-bar", "foo.bar", "FOO--bar"}

		// when
		keys := make(map[string]bool, len(names))
		for _, name := range names {
			keys[entities.DependencyChange{Subject: entities.SubjectPythonPackage, Name: name}.Key()] = true
		}

		// then
		assert.Len(t, keys, 1)
	})

	t.Run("should keep image variants apart", func(t *testing.T) {
		t.Parallel()

		// given
		plain := entities.DependencyChange{Subject: entities.SubjectDockerBaseImage, Name: "golang", To: "1.23"}
		alpine := entities.DependencyChange{
			Subject: entities.SubjectDockerBaseImage, Name: "golang", To: "1.23-alpine",
		}
		laterAlpine := entities.DependencyChange{
			Subject: entities.SubjectDockerBaseImage, Name: "golang", To: "1.24-alpine",
		}

		// when / then
		assert.NotEqual(t, plain.Key(), alpine.Key())
		assert.Equal(t, alpine.Key(), laterAlpine.Key())
	})

	t.Run("should key a version pin by its subject alone", func(t *testing.T) {
		t.Parallel()

		// given
		first := entities.DependencyChange{Subject: entities.SubjectGoVersion, From: "1.25.0", To: "1.26.0"}
		second := entities.DependencyChange{Subject: entities.SubjectGoVersion, From: "1.26.0", To: "1.27.1"}

		// when / then
		assert.Equal(t, first.Key(), second.Key())
	})

	t.Run("should keep the same name apart under different subjects", func(t *testing.T) {
		t.Parallel()

		// given
		runtime := entities.DependencyChange{Subject: entities.SubjectPipelineRuntime, Name: "golang"}
		image := entities.DependencyChange{Subject: entities.SubjectDockerBaseImage, Name: "golang"}

		// when / then
		assert.NotEqual(t, runtime.Key(), image.Key())
	})
}

// TestDependencyEntriesSurviveReleaseDeduplication runs the statements autoupdate
// writes through the de-duplication autobump applies when it cuts a release.
// That pass compares entries by their words with versions and backticks
// stripped, and drops one of two entries whose words overlap enough; a
// statement it swallowed would be a dependency the release notes never name.
func TestDependencyEntriesSurviveReleaseDeduplication(t *testing.T) {
	t.Parallel()

	t.Run("should keep every statement autoupdate writes side by side", func(t *testing.T) {
		t.Parallel()

		// given: each statement rendered on its own line, the shape most at risk
		groups := [][]entities.DependencyChange{
			{{Subject: entities.SubjectPythonVersion, From: "3.12", To: "3.13"}},
			{{Subject: entities.SubjectPipelineRuntime, Name: "python", From: "3.12", To: "3.13"}},
			{{Subject: entities.SubjectJavaVersion, From: "21", To: "25"}},
			{{Subject: entities.SubjectPipelineRuntime, Name: "java", From: "21", To: "25"}},
			{{Subject: entities.SubjectGoVersion, From: "1.26.0", To: "1.27.1"}},
			{{Subject: entities.SubjectPipelineRuntime, Name: "golang", From: "1.26", To: "1.27"}},
			{goModule("github.com/foo/bar", "v1.2.0", "v1.3.0")},
			{goModule("github.com/foo/bar/v2", "v2.0.0", "v2.1.0")},
			{{Subject: entities.SubjectDockerBaseImage, Name: "redis", From: "7.2-alpine", To: "7.4-alpine"}},
			{{Subject: entities.SubjectDockerBaseImage, Name: "nginx", From: "1.25-alpine", To: "1.27-alpine"}},
			{{Subject: entities.SubjectContainerImage, Name: "redis", From: "7.2-alpine", To: "7.4-alpine"}},
			{{Subject: entities.SubjectContainerImage, Name: "nginx", From: "1.25-alpine", To: "1.27-alpine"}},
			{{Subject: entities.SubjectTerraformModule, Name: "vpc", From: "5.0.0", To: "5.1.0"}},
			{{Subject: entities.SubjectTerraformModule, Name: "eks", From: "20.0.0", To: "20.1.0"}},
			{{Subject: entities.SubjectGitHubAction, Name: "actions/checkout", From: "v4", To: "v5"}},
			{{Subject: entities.SubjectGitHubAction, Name: "actions/setup-go", From: "v5", To: "v6"}},
		}
		var entries []string
		for _, group := range groups {
			entries = append(entries, entities.RenderDependencyEntries(group)...)
		}

		// when
		kept := changelogEntities.DeduplicateEntries(entries)

		// then
		assert.Equal(t, entries, kept)
	})
}
