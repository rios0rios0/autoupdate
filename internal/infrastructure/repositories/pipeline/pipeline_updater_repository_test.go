//go:build unit

package pipeline_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/pipeline"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestName(t *testing.T) {
	t.Parallel()

	t.Run("should return pipeline as updater name", func(t *testing.T) {
		t.Parallel()

		// given
		updater := pipeline.NewUpdaterRepository()

		// when
		name := updater.Name()

		// then
		assert.Equal(t, "pipeline", name)
	})
}

func TestDetect(t *testing.T) {
	t.Parallel()

	t.Run("should return true when GitHub Actions workflow exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{
				{Path: ".github/workflows/ci.yml", IsDir: false},
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := pipeline.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.True(t, detected)
	})

	t.Run("should return true when Azure DevOps pipeline exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{
				{Path: "azure-pipelines.yml", IsDir: false},
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := pipeline.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.True(t, detected)
	})

	t.Run("should return false when no pipeline files exist", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := pipeline.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.False(t, detected)
	})
}

func TestCreateUpdatePRs(t *testing.T) {
	t.Parallel()

	t.Run("should return empty when no YAML files found", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}

		// when
		prs, err := pipeline.NewUpdaterRepository().CreateUpdatePRs(t.Context(), provider, repo, opts)

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
	})

	t.Run("should log dry run upgrades without creating PR", func(t *testing.T) {
		t.Parallel()

		// given
		ghWorkflow := `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-go@v5
        with:
          go-version: '1.20.0'
`
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{
				{Path: ".github/workflows/ci.yml", IsDir: false},
			}).
			WithFileContents(map[string]string{
				".github/workflows/ci.yml": ghWorkflow,
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{DryRun: true}

		// when
		prs, err := pipeline.NewUpdaterRepository().CreateUpdatePRs(t.Context(), provider, repo, opts)

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
		// No PR should be created (no BranchInputs recorded)
		assert.Empty(t, provider.BranchInputs)
	})

	t.Run("should skip when PR already exists for branch", func(t *testing.T) {
		t.Parallel()

		// given
		ghWorkflow := `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-go@v5
        with:
          go-version: '1.20.0'
`
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{
				{Path: ".github/workflows/ci.yml", IsDir: false},
			}).
			WithFileContents(map[string]string{
				".github/workflows/ci.yml": ghWorkflow,
			}).
			WithPRExistsResult(true).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}

		// when
		prs, err := pipeline.NewUpdaterRepository().CreateUpdatePRs(t.Context(), provider, repo, opts)

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
		assert.Empty(t, provider.BranchInputs)
	})
}

func TestAppendChangelogEntry(t *testing.T) {
	t.Parallel()

	t.Run("should wrap language and versions in backticks in changelog entry", func(t *testing.T) {
		t.Parallel()

		// given
		changelog := `# Changelog

## [Unreleased]

## [1.0.0] - 2026-01-01
`
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": changelog}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		upgrades := []pipeline.UpgradeTask{
			pipeline.NewUpgradeTask("Go", "1.20.0", "1.21.0"),
		}

		// when
		result := pipeline.AppendChangelogEntry(t.Context(), provider, repo, upgrades, nil)

		// then
		require.Len(t, result, 1)
		assert.Equal(t, "CHANGELOG.md", result[0].Path)
		assert.Contains(t, result[0].Content, "- changed the Go pipeline version from `1.20.0` to `1.21.0`")
	})
}

func TestTruncateToGranularity(t *testing.T) {
	t.Parallel()

	t.Run("should truncate 3-part version to 2-part when reference is 2-part", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pipeline.TruncateToGranularity("1.26.0", "1.25")

		// then
		assert.Equal(t, "1.26", result)
	})

	t.Run("should return full version when reference has same precision", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pipeline.TruncateToGranularity("1.26.3", "1.25.7")

		// then
		assert.Equal(t, "1.26.3", result)
	})

	t.Run("should return full version when reference has more parts", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pipeline.TruncateToGranularity("1.26", "1.25.7")

		// then
		assert.Equal(t, "1.26", result)
	})

	t.Run("should truncate 3-part to 1-part when reference is 1-part", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pipeline.TruncateToGranularity("21.0.3", "17")

		// then
		assert.Equal(t, "21", result)
	})
}

func TestReplaceLastOccurrence(t *testing.T) {
	t.Parallel()

	t.Run("should replace only the last occurrence of the substring", func(t *testing.T) {
		t.Parallel()

		// given
		s := "version 3.12 and version 3.12"

		// when
		result := pipeline.ReplaceLastOccurrence(s, "3.12", "3.13")

		// then
		assert.Equal(t, "version 3.12 and version 3.13", result)
	})

	t.Run("should replace the only occurrence when there is one", func(t *testing.T) {
		t.Parallel()

		// given
		s := "versionSpec: '3.12'"

		// when
		result := pipeline.ReplaceLastOccurrence(s, "3.12", "3.13")

		// then
		assert.Equal(t, "versionSpec: '3.13'", result)
	})

	t.Run("should return original string when substring not found", func(t *testing.T) {
		t.Parallel()

		// given
		s := "no match here"

		// when
		result := pipeline.ReplaceLastOccurrence(s, "3.12", "3.13")

		// then
		assert.Equal(t, "no match here", result)
	})
}

func TestApplyUpgrades(t *testing.T) {
	t.Parallel()

	t.Run("should replace versionSpec and strip version from displayName", func(t *testing.T) {
		t.Parallel()

		// given
		content := `- task: UsePythonVersion@2
  displayName: 'Install Python 3.12'
  inputs:
    versionSpec: '3.12'
`
		fullMatch := "UsePythonVersion@2\n  displayName: 'Install Python 3.12'\n  inputs:\n    versionSpec: '3.12'"
		upgrades := []pipeline.UpgradeTask{
			pipeline.NewUpgradeTaskWithFullMatch("python", "3.12", "3.13", "azure-pipelines.yml", fullMatch),
		}
		fileContents := map[string]string{"azure-pipelines.yml": content}

		// when
		changes := pipeline.ApplyUpgrades(upgrades, fileContents)

		// then
		require.Len(t, changes, 1)
		assert.Contains(t, changes[0].Content, "displayName: 'Install Python'")
		assert.NotContains(t, changes[0].Content, "displayName: 'Install Python 3.12'")
		assert.Contains(t, changes[0].Content, "versionSpec: '3.13'")
	})
}

func TestIsExactVersion(t *testing.T) {
	t.Parallel()

	t.Run("should accept simple dotted version", func(t *testing.T) {
		t.Parallel()

		assert.True(t, pipeline.IsExactVersion("1.25.7"))
		assert.True(t, pipeline.IsExactVersion("3.13"))
		assert.True(t, pipeline.IsExactVersion("21"))
	})

	t.Run("should reject wildcard versions", func(t *testing.T) {
		t.Parallel()

		assert.False(t, pipeline.IsExactVersion("1.20.x"))
		assert.False(t, pipeline.IsExactVersion("3.x"))
	})

	t.Run("should reject range operators", func(t *testing.T) {
		t.Parallel()

		assert.False(t, pipeline.IsExactVersion(">=1.20"))
		assert.False(t, pipeline.IsExactVersion("~1.20"))
		assert.False(t, pipeline.IsExactVersion("^3.13"))
	})

	t.Run("should reject empty string", func(t *testing.T) {
		t.Parallel()

		assert.False(t, pipeline.IsExactVersion(""))
	})
}
