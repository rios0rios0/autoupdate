package pipeline_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/pipeline"
	"github.com/rios0rios0/autoupdate/internal/support"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

const changelogFixture = `# Changelog

## [Unreleased]

### Added
- added something earlier

## [1.0.0] - 2026-01-01
`

// writeWorkflowRepo lays out a repository whose only pipeline file is a GitHub
// Actions workflow, which is exactly the shape the local walk used to miss.
func writeWorkflowRepo(t *testing.T, workflow string) string {
	t.Helper()

	root := t.TempDir()
	workflows := filepath.Join(root, ".github", "workflows")
	// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
	require.NoError(t, os.MkdirAll(workflows, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(workflows, "ci.yaml"), []byte(workflow), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "CHANGELOG.md"), []byte(changelogFixture), 0o600))

	return root
}

func readFile(t *testing.T, parts ...string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(parts...))
	require.NoError(t, err)

	return string(data)
}

// TestLocalGitHubActionsUpgrade walks the whole clone-based path a GitHub
// Actions repository takes: the filesystem scan, the classification of
// `.github/workflows/`, the rewrite applied to disk, the changelog entry, and
// the branch, commit and pull request text the run reports back. Composing the
// same steps `ApplyUpdates` composes keeps the network fetch of the latest
// versions out of the test while covering everything after it.
func TestLocalGitHubActionsUpgrade(t *testing.T) {
	t.Parallel()

	t.Run("should upgrade a language version and report PR metadata", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeWorkflowRepo(t, `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22.0'
`)
		// No tags published means the action pin has nothing to move to, leaving
		// the Go toolchain version as the only upgrade.
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy()
		latestVersions := map[string]string{"golang": "1.24.1"}

		// when
		upgrades, fileContents := pipeline.LocalScanAndDetermineUpgrades(
			t.Context(), root, provider, latestVersions,
		)
		fileChanges := pipeline.ApplyUpgrades(upgrades, fileContents)
		require.NoError(t, support.WriteFileChanges(root, fileChanges))
		changelogWritten := support.LocalChangelogUpdate(root, []string{
			"- changed the golang pipeline version from `1.22.0` to `1.24.1`",
		})

		// then
		require.Len(t, upgrades, 1)
		require.Len(t, fileChanges, 1)
		assert.Equal(t, ".github/workflows/ci.yaml", fileChanges[0].Path)

		workflow := readFile(t, root, ".github", "workflows", "ci.yaml")
		assert.Contains(t, workflow, "go-version: '1.24.1'")
		assert.NotContains(t, workflow, "1.22.0")

		assert.True(t, changelogWritten)
		assert.Contains(t, readFile(t, root, "CHANGELOG.md"),
			"- changed the golang pipeline version from `1.22.0` to `1.24.1`")

		assert.Equal(t, "chore/upgrade-pipeline-golang-1.24.1", pipeline.GenerateBranchName(upgrades))
		assert.Equal(t,
			"chore(deps): upgraded golang pipeline version from `1.22.0` to `1.24.1`",
			pipeline.GenerateCommitMessage(upgrades))
		assert.Equal(t,
			"chore(deps): upgraded golang pipeline version to `1.24.1`",
			pipeline.GeneratePRTitle(upgrades))
		assert.Contains(t, pipeline.GeneratePRDescription(upgrades),
			"| golang | 1.22.0 | 1.24.1 | .github/workflows/ci.yaml |")
	})

	t.Run("should upgrade a pinned action reference", func(t *testing.T) {
		t.Parallel()

		// given
		// The action-pin scan only ever runs for files classified as GitHub
		// Actions, so on the clone-based path it was unreachable entirely.
		root := writeWorkflowRepo(t, `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
`)
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithTags([]string{"v3.6.0", "v4.1.7", "v5.0.0"}).
			BuildSpy()

		// when
		upgrades, fileContents := pipeline.LocalScanAndDetermineUpgrades(
			t.Context(), root, provider, map[string]string{"golang": "1.24.1"},
		)
		fileChanges := pipeline.ApplyUpgrades(upgrades, fileContents)
		require.NoError(t, support.WriteFileChanges(root, fileChanges))

		// then
		require.Len(t, upgrades, 1)
		assert.Equal(t, "action:actions/checkout", pipeline.UpgradeTaskLanguage(upgrades[0]))
		assert.Equal(t, "v4", pipeline.UpgradeTaskCurrentVer(upgrades[0]))
		assert.Equal(t, "v5", pipeline.UpgradeTaskNewVersion(upgrades[0]))

		assert.Contains(t, readFile(t, root, ".github", "workflows", "ci.yaml"),
			"uses: actions/checkout@v5")
		assert.Equal(t,
			"chore/upgrade-pipeline-action-actions-checkout-v5",
			pipeline.GenerateBranchName(upgrades))
	})

	t.Run("should upgrade both a language version and an action in one run", func(t *testing.T) {
		t.Parallel()

		// given
		root := writeWorkflowRepo(t, `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-python@v4
        with:
          python-version: '3.11'
`)
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithTags([]string{"v4.7.1", "v5.1.0"}).
			BuildSpy()

		// when
		upgrades, fileContents := pipeline.LocalScanAndDetermineUpgrades(
			t.Context(), root, provider, map[string]string{"python": "3.13.1"},
		)
		fileChanges := pipeline.ApplyUpgrades(upgrades, fileContents)
		require.NoError(t, support.WriteFileChanges(root, fileChanges))

		// then
		require.Len(t, upgrades, 2)
		require.Len(t, fileChanges, 1)

		workflow := readFile(t, root, ".github", "workflows", "ci.yaml")
		assert.Contains(t, workflow, "uses: actions/setup-python@v5")
		assert.Contains(t, workflow, "python-version: '3.13'")

		assert.Equal(t, "chore/upgrade-2-pipeline-versions", pipeline.GenerateBranchName(upgrades))
		assert.Equal(t,
			"chore(deps): upgraded 2 pipeline version references",
			pipeline.GenerateCommitMessage(upgrades))

		description := pipeline.GeneratePRDescription(upgrades)
		assert.Contains(t, description, "| python | 3.11 | 3.13 | .github/workflows/ci.yaml |")
		assert.Contains(t, description, "| action:actions/setup-python | v4 | v5 | .github/workflows/ci.yaml |")
	})
}
