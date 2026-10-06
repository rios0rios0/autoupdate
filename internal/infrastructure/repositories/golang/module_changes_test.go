package golang_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	goUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
	"github.com/rios0rios0/autoupdate/internal/support"
	"github.com/rios0rios0/autoupdate/test/infrastructure/gitrepo"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

// appGoMod is a go.mod with a direct and an indirect requirement.
const appGoMod = "module example.com/app\n\ngo 1.26.0\n\n" +
	"require (\n\tgithub.com/spf13/cobra v1.8.0\n\tgolang.org/x/mod v0.20.0\n)\n\n" +
	"require golang.org/x/sys v0.20.0 // indirect\n"

// genericFragment is the fragment this repository's 2026-10-01 run filed, the
// sentence that named nothing.
const genericFragment = "kind: 'Changed'\n" +
	"body: 'changed the Go module dependencies to their latest versions'\n" +
	"time: '2026-10-01T09:09:58.7700743Z'\n"

// history returns this repository's own go.mod as one of its commits left it.
func history(t *testing.T, commit string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join("testdata", "history", commit+".mod"))
	require.NoError(t, err)
	return string(content)
}

// pendingBodies returns the bodies of the fragments pending in root, sorted.
func pendingBodies(t *testing.T, root string) []string {
	t.Helper()

	dir := filepath.Join(root, ".changes", "unreleased")
	dirEntries, err := os.ReadDir(dir)
	require.NoError(t, err)

	bodies := make([]string, 0, len(dirEntries))
	for _, dirEntry := range dirEntries {
		content, readErr := os.ReadFile(filepath.Join(dir, dirEntry.Name()))
		require.NoError(t, readErr)

		var fragment entities.ChlogFragment
		require.NoError(t, yaml.Unmarshal(content, &fragment))
		bodies = append(bodies, fragment.Body)
	}
	sort.Strings(bodies)
	return bodies
}

func TestDiffGoMod(t *testing.T) {
	t.Parallel()

	t.Run("should report every requirement that moved, indirect ones included", func(t *testing.T) {
		t.Parallel()

		// given
		after := strings.NewReplacer(
			"cobra v1.8.0", "cobra v1.9.1",
			"x/sys v0.20.0", "x/sys v0.21.0",
		).Replace(appGoMod)

		// when
		changes, err := goUpdater.DiffGoMod("go.mod", []byte(appGoMod), []byte(after))

		// then
		require.NoError(t, err)
		assert.Equal(t, []entities.DependencyChange{
			{Subject: entities.SubjectGoModule, Name: "github.com/spf13/cobra", From: "v1.8.0", To: "v1.9.1"},
			{Subject: entities.SubjectGoModule, Name: "golang.org/x/sys", From: "v0.20.0", To: "v0.21.0"},
		}, changes)
	})

	t.Run("should report the go directive as the Go version", func(t *testing.T) {
		t.Parallel()

		// given
		after := strings.Replace(appGoMod, "go 1.26.0", "go 1.27.1", 1)

		// when
		changes, err := goUpdater.DiffGoMod("go.mod", []byte(appGoMod), []byte(after))

		// then
		require.NoError(t, err)
		assert.Equal(t, []entities.DependencyChange{
			{Subject: entities.SubjectGoVersion, From: "1.26.0", To: "1.27.1"},
		}, changes)
	})

	t.Run("should leave out added and removed requirements and other directives", func(t *testing.T) {
		t.Parallel()

		// given
		after := strings.Replace(appGoMod, "\tgolang.org/x/mod v0.20.0\n", "\tgolang.org/x/text v0.20.0\n", 1) +
			"\ntoolchain go1.27.1\n\nreplace github.com/spf13/cobra => ../cobra\n"

		// when
		changes, err := goUpdater.DiffGoMod("go.mod", []byte(appGoMod), []byte(after))

		// then
		require.NoError(t, err)
		assert.Empty(t, changes)
	})

	t.Run("should not report a go directive one side does not declare", func(t *testing.T) {
		t.Parallel()

		// given
		before := strings.Replace(appGoMod, "go 1.26.0\n", "", 1)

		// when
		changes, err := goUpdater.DiffGoMod("go.mod", []byte(before), []byte(appGoMod))

		// then
		require.NoError(t, err)
		assert.Empty(t, changes)
	})

	t.Run("should fail on a go.mod it cannot parse", func(t *testing.T) {
		t.Parallel()

		// given
		broken := "module example.com/app\n\nrequire (\n\tgithub.com/spf13/cobra\n"

		// when
		changes, err := goUpdater.DiffGoMod("go.mod", []byte(appGoMod), []byte(broken))

		// then
		require.Error(t, err)
		assert.Nil(t, changes)
	})
}

func TestObserveModuleChanges(t *testing.T) {
	t.Parallel()

	t.Run("should read every modified module and skip vendored and fixture ones", func(t *testing.T) {
		t.Parallel()

		// given
		root := gitrepo.New(t, map[string]string{
			"go.mod":                    appGoMod,
			"tools/go.mod":              appGoMod,
			"vendor/example.com/go.mod": appGoMod,
			"testdata/go.mod":           appGoMod,
		})
		bumped := strings.Replace(appGoMod, "cobra v1.8.0", "cobra v1.9.1", 1)
		gitrepo.Write(t, root, map[string]string{
			"go.mod":                    bumped,
			"tools/go.mod":              strings.Replace(appGoMod, "cobra v1.8.0", "cobra v1.10.0", 1),
			"vendor/example.com/go.mod": bumped,
			"testdata/go.mod":           bumped,
			"go.sum":                    "irrelevant\n",
		})

		// when
		changes, err := goUpdater.ObserveModuleChanges(t.Context(), root)

		// then: one change per module; the changelog writer folds them
		require.NoError(t, err)
		assert.ElementsMatch(t, []entities.DependencyChange{
			{Subject: entities.SubjectGoModule, Name: "github.com/spf13/cobra", From: "v1.8.0", To: "v1.9.1"},
			{Subject: entities.SubjectGoModule, Name: "github.com/spf13/cobra", From: "v1.8.0", To: "v1.10.0"},
		}, changes)
	})

	t.Run("should report nothing when only go.sum changed", func(t *testing.T) {
		t.Parallel()

		// given
		root := gitrepo.New(t, map[string]string{"go.mod": appGoMod, "go.sum": "before\n"})
		gitrepo.Write(t, root, map[string]string{"go.sum": "after\n"})

		// when
		changes, err := goUpdater.ObserveModuleChanges(t.Context(), root)

		// then
		require.NoError(t, err)
		assert.Empty(t, changes)
	})

	t.Run("should fail outside a repository", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()

		// when
		changes, err := goUpdater.ObserveModuleChanges(t.Context(), root)

		// then
		require.Error(t, err)
		assert.Nil(t, changes)
	})
}

// TestModuleChangesReplay replays the runs this repository's own changelog
// recorded nothing for: after the 2026-10-01 run filed its generic sentence,
// the exact-match de-duplication dropped every later run.
func TestModuleChangesReplay(t *testing.T) {
	t.Parallel()

	t.Run("should name what each run moved and update a module named before", func(t *testing.T) {
		t.Parallel()

		// given: the state right after the 2026-10-01 run
		root := gitrepo.New(t, map[string]string{
			"go.mod": history(t, "59f0af8"),
			".changes/unreleased/1790845798770074300-b6a2.yaml": genericFragment,
		})
		record := func(goMod string) {
			gitrepo.Write(t, root, map[string]string{"go.mod": goMod})
			support.RecordObservedDependencyChanges(t.Context(), root, goUpdater.ObserveModuleChanges,
				goUpdater.ChangelogEntry(false, ""), goUpdater.GoChangelogSummary)
			gitrepo.Commit(t, root, "autoupdate run")
		}

		// when: the runs of 2026-10-04, -05 and -06, then a later go-git bump
		for _, commit := range []string{"6d90b95", "92b78ad", "7a371ad"} {
			record(history(t, commit))
		}
		record(strings.Replace(history(t, "7a371ad"), "go-git/v5 v5.19.3", "go-git/v5 v5.19.4", 1))

		// then
		assert.Equal(t, []string{
			"changed the Go module `github.com/go-git/go-git/v5` from `v5.19.2` to `v5.19.4`",
			"changed the Go module `github.com/rios0rios0/cliforge` from `v0.4.6` to `v0.4.7`",
			"changed the Go module dependencies to their latest versions",
			"changed the Go modules `github.com/go-git/go-billy/v5` from `v5.9.1` to `v5.9.2` and " +
				"`golang.org/x/tools` from `v0.50.0` to `v0.51.0`",
		}, pendingBodies(t, root))
	})
}

func TestApplyUpdatesChangelog(t *testing.T) {
	t.Parallel()

	t.Run("should record the modules the upgrade moved", func(t *testing.T) {
		t.Parallel()

		// given
		changelog := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n"
		root := gitrepo.New(t, map[string]string{"go.mod": history(t, "92b78ad"), "CHANGELOG.md": changelog})
		runner := repositorydoubles.NewFakeUpgradeRunner(map[string]string{"go.mod": history(t, "7a371ad")}, "")
		updater := goUpdater.NewUpdaterRepositoryForTest(
			&repositorydoubles.StubVersionFetcher{Version: "1.27.1"}, runner,
		)
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy()

		// when
		result, err := updater.ApplyUpdates(t.Context(), root, provider,
			entities.Repository{Organization: "org", Name: "repo"}, entities.UpdateOptions{})

		// then
		require.NoError(t, err)
		require.NotNil(t, result)
		content, readErr := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
		require.NoError(t, readErr)
		assert.Equal(t, "# Changelog\n\n## [Unreleased]\n\n### Changed\n\n"+
			"- changed the Go module `github.com/rios0rios0/cliforge` from `v0.4.6` to `v0.4.7`\n\n"+
			"## [1.0.0] - 2026-01-01\n", string(content))
	})
}
