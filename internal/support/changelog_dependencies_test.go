package support_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
)

// entitiesChange shortens the dependency change type in test tables.
type entitiesChange = entities.DependencyChange

// goModuleChange builds a Go module change.
func goModuleChange(name, from, to string) entities.DependencyChange {
	return entities.DependencyChange{Subject: entities.SubjectGoModule, Name: name, From: from, To: to}
}

// chlogFragment renders a fragment the way chlog writes one.
func chlogFragment(kind, body, at string) string {
	return "kind: '" + kind + "'\nbody: '" + body + "'\ntime: '" + at + "'\n"
}

// goGitFragmentName is the file a pending fragment about go-git is filed in.
const goGitFragmentName = "1791000000000000000-aaaa.yaml"

// goGitFragment is a pending fragment naming go-git, as a run wrote it.
const goGitFragment = "kind: 'Changed'\n" +
	"body: 'changed the Go module `github.com/go-git/go-git/v5` from `v5.19.2` to `v5.19.3`'\n" +
	"time: '2026-10-05T09:15:24Z'\n"

// readFragmentBodies returns the bodies of the fragments pending under the
// default unreleased directory.
func readFragmentBodies(t *testing.T, root string) []string {
	t.Helper()

	var bodies []string
	for _, content := range readChlogFragments(t, root, ".changes/unreleased") {
		var fragment entities.ChlogFragment
		require.NoError(t, yaml.Unmarshal([]byte(content), &fragment))
		bodies = append(bodies, fragment.Body)
	}
	return bodies
}

func TestLocalDependencyChangelogUpdateWithChlog(t *testing.T) {
	t.Parallel()

	t.Run("should rewrite a pending fragment in place keeping its file, kind and time", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{".changes/unreleased/" + goGitFragmentName: goGitFragment})

		// when
		updated := support.LocalDependencyChangelogUpdate(root, []entities.DependencyChange{
			goModuleChange("github.com/go-git/go-git/v5", "v5.19.3", "v5.19.4"),
		})

		// then
		assert.True(t, updated)
		content, err := os.ReadFile(filepath.Join(root, ".changes", "unreleased", goGitFragmentName))
		require.NoError(t, err)
		assert.Equal(t, chlogFragment("Changed",
			"changed the Go module `github.com/go-git/go-git/v5` from `v5.19.2` to `v5.19.4`",
			"2026-10-05T09:15:24Z"), string(content))
		assert.Len(t, readChlogFragments(t, root, ".changes/unreleased"), 1)
	})

	t.Run("should file new dependencies as one fragment per line of five", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{".changes/unreleased/": ""})
		var changes []entities.DependencyChange
		for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
			changes = append(changes, goModuleChange("example.com/"+name, "v1.0.0", "v1.1.0"))
		}

		// when
		updated := support.LocalDependencyChangelogUpdate(root, changes)

		// then
		assert.True(t, updated)
		assert.ElementsMatch(t, []string{
			"changed the Go modules `example.com/a` from `v1.0.0` to `v1.1.0`, " +
				"`example.com/b` from `v1.0.0` to `v1.1.0`, `example.com/c` from `v1.0.0` to `v1.1.0`, " +
				"`example.com/d` from `v1.0.0` to `v1.1.0` and `example.com/e` from `v1.0.0` to `v1.1.0`",
			"changed the Go module `example.com/f` from `v1.0.0` to `v1.1.0`",
		}, readFragmentBodies(t, root))
	})

	t.Run("should change nothing when the same changes are recorded again", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{".changes/unreleased/": ""})
		changes := []entities.DependencyChange{goModuleChange("example.com/a", "v1.0.0", "v1.1.0")}
		require.True(t, support.LocalDependencyChangelogUpdate(root, changes))
		before := readChlogFragments(t, root, ".changes/unreleased")

		// when
		updated := support.LocalDependencyChangelogUpdate(root, changes)

		// then
		assert.False(t, updated)
		assert.Equal(t, before, readChlogFragments(t, root, ".changes/unreleased"))
	})

	t.Run("should remove a fragment left naming nothing", func(t *testing.T) {
		t.Parallel()

		// given: the same module filed by two runs
		older := chlogFragment("Changed",
			"changed the Go module `example.com/a` from `v1.0.0` to `v1.1.0`", "2026-10-01T00:00:00Z")
		newer := chlogFragment("Changed",
			"changed the Go module `example.com/a` from `v1.1.0` to `v1.2.0`", "2026-10-02T00:00:00Z")
		root := writeChlogRepo(t, map[string]string{
			".changes/unreleased/1-older.yaml": older,
			".changes/unreleased/2-newer.yaml": newer,
		})

		// when
		updated := support.LocalDependencyChangelogUpdate(root, []entities.DependencyChange{
			goModuleChange("example.com/a", "v1.2.0", "v1.3.0"),
		})

		// then: the oldest mention keeps it, from where the cycle started
		assert.True(t, updated)
		assert.NoFileExists(t, filepath.Join(root, ".changes", "unreleased", "2-newer.yaml"))
		assert.Equal(t,
			[]string{"changed the Go module `example.com/a` from `v1.0.0` to `v1.3.0`"},
			readFragmentBodies(t, root))
	})

	t.Run("should not edit fragments autoupdate does not own", func(t *testing.T) {
		t.Parallel()

		// given: a fragment flagged breaking, and one chlog would never compile
		breaking := "kind: 'Changed'\n" +
			"body: 'changed the Go module `github.com/go-git/go-git/v5` from `v5.19.2` to `v5.19.3`'\n" +
			"breaking: true\ntime: '2026-10-05T09:15:24Z'\n"
		root := writeChlogRepo(t, map[string]string{
			".changes/unreleased/1-breaking.yaml": breaking,
			".changes/unreleased/2-other.yml":     goGitFragment,
		})

		// when
		updated := support.LocalDependencyChangelogUpdate(root, []entities.DependencyChange{
			goModuleChange("github.com/go-git/go-git/v5", "v5.19.3", "v5.19.4"),
		})

		// then: both are left exactly as they were, and the run files its own
		assert.True(t, updated)
		content, err := os.ReadFile(filepath.Join(root, ".changes", "unreleased", "1-breaking.yaml"))
		require.NoError(t, err)
		assert.Equal(t, breaking, string(content))
		content, err = os.ReadFile(filepath.Join(root, ".changes", "unreleased", "2-other.yml"))
		require.NoError(t, err)
		assert.Equal(t, goGitFragment, string(content))
		assert.Len(t, readChlogFragments(t, root, ".changes/unreleased"), 3)
	})

	t.Run("should count a fragment it cannot parse as pending without editing it", func(t *testing.T) {
		t.Parallel()

		// given
		broken := "kind: [unterminated\n"
		root := writeChlogRepo(t, map[string]string{".changes/unreleased/1-broken.yaml": broken})
		observe := func(context.Context, string) ([]entities.DependencyChange, error) { return nil, nil }

		// when
		recorded := support.RecordObservedDependencyChanges(t.Context(), root, observe, "- fallback", "- summary")
		updated := support.LocalDependencyChangelogUpdate(root, []entities.DependencyChange{
			goModuleChange("example.com/a", "v1.0.0", "v1.1.0"),
		})

		// then: the broken fragment kept the summary out, and stays as it was
		assert.False(t, recorded)
		assert.True(t, updated)
		content, err := os.ReadFile(filepath.Join(root, ".changes", "unreleased", "1-broken.yaml"))
		require.NoError(t, err)
		assert.Equal(t, broken, string(content))
	})

	t.Run("should never read or write through a symlinked fragment", func(t *testing.T) {
		t.Parallel()

		// given: a fragment that is a link to a file outside the repository
		outside := filepath.Join(t.TempDir(), "outside.yaml")
		require.NoError(t, os.WriteFile(outside, []byte(goGitFragment), 0o600))
		root := writeChlogRepo(t, map[string]string{".changes/unreleased/": ""})
		require.NoError(t, os.Symlink(outside, filepath.Join(root, ".changes", "unreleased", "link.yaml")))

		// when
		updated := support.LocalDependencyChangelogUpdate(root, []entities.DependencyChange{
			goModuleChange("github.com/go-git/go-git/v5", "v5.19.3", "v5.19.4"),
		})

		// then
		assert.True(t, updated)
		content, err := os.ReadFile(outside)
		require.NoError(t, err)
		assert.Equal(t, goGitFragment, string(content))
	})
}

func TestLocalDependencyChangelogUpdateWithKeepAChangelog(t *testing.T) {
	t.Parallel()

	t.Run("should write nothing for changes that end where they started", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": baseChangelog})

		// when
		updated := support.LocalDependencyChangelogUpdate(root, []entities.DependencyChange{
			goModuleChange("example.com/a", "v1.0.0", "v1.0.0"),
		})

		// then
		assert.False(t, updated)
		assert.Equal(t, baseChangelog, readChangelog(t, root))
	})

	t.Run("should fold the same dependency moving in several modules", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": baseChangelog})

		// when
		updated := support.LocalDependencyChangelogUpdate(root, []entities.DependencyChange{
			goModuleChange("example.com/a", "v1.1.0", "v1.3.0"),
			goModuleChange("example.com/a", "v1.0.0", "v1.2.0"),
		})

		// then
		assert.True(t, updated)
		changelog := readChangelog(t, root)
		assert.Contains(t, changelog, "- changed the Go module `example.com/a` from `v1.0.0` to `v1.3.0`\n")
		assert.Equal(t, 1, strings.Count(changelog, "example.com/a"))
	})
}

func TestRecordObservedDependencyChanges(t *testing.T) {
	t.Parallel()

	const (
		fallback = "- changed the Go module dependencies to their latest versions"
		summary  = "- changed the Go module checksums to match the declared versions"
	)

	t.Run("should name what the observer read", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": baseChangelog})
		observe := func(context.Context, string) ([]entities.DependencyChange, error) {
			return []entities.DependencyChange{goModuleChange("example.com/a", "v1.0.0", "v1.1.0")}, nil
		}

		// when
		updated := support.RecordObservedDependencyChanges(t.Context(), root, observe, fallback, summary)

		// then
		assert.True(t, updated)
		assert.Contains(t, readChangelog(t, root),
			"- changed the Go module `example.com/a` from `v1.0.0` to `v1.1.0`")
	})

	t.Run("should record the generic statement when the observer fails", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": baseChangelog})
		observe := func(context.Context, string) ([]entities.DependencyChange, error) {
			return nil, errors.New("unsupported lockfile version")
		}

		// when
		updated := support.RecordObservedDependencyChanges(t.Context(), root, observe, fallback, summary)

		// then
		assert.True(t, updated)
		assert.Contains(t, readChangelog(t, root), fallback)
	})

	t.Run("should record the generic statement when the observer panics", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": baseChangelog})
		observe := func(context.Context, string) ([]entities.DependencyChange, error) {
			panic("index out of range")
		}

		// when
		updated := support.RecordObservedDependencyChanges(t.Context(), root, observe, fallback, summary)

		// then
		assert.True(t, updated)
		assert.Contains(t, readChangelog(t, root), fallback)
	})

	t.Run("should write nothing when nothing moved and entries are pending", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": changelogRecording})
		observe := func(context.Context, string) ([]entities.DependencyChange, error) { return nil, nil }

		// when
		updated := support.RecordObservedDependencyChanges(t.Context(), root, observe, fallback, summary)

		// then
		assert.False(t, updated)
		assert.Equal(t, changelogRecording, readChangelog(t, root))
	})

	t.Run("should record the summary when nothing moved and nothing is pending", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{"CHANGELOG.md": baseChangelog})
		observe := func(context.Context, string) ([]entities.DependencyChange, error) { return nil, nil }

		// when
		updated := support.RecordObservedDependencyChanges(t.Context(), root, observe, fallback, summary)

		// then
		assert.True(t, updated)
		assert.Contains(t, readChangelog(t, root), summary)
	})

	t.Run("should file the summary as a fragment for an empty chlog directory", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeChlogRepo(t, map[string]string{".changes/unreleased/": ""})
		observe := func(context.Context, string) ([]entities.DependencyChange, error) { return nil, nil }

		// when
		updated := support.RecordObservedDependencyChanges(t.Context(), root, observe, fallback, summary)

		// then
		assert.True(t, updated)
		assert.Equal(t, []string{strings.TrimPrefix(summary, "- ")}, readFragmentBodies(t, root))
	})
}
