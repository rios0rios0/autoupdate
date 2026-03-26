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

func TestScanFileForActions(t *testing.T) {
	t.Parallel()

	t.Run("should detect major version action references", func(t *testing.T) {
		t.Parallel()

		// given
		content := "    - uses: actions/checkout@v4\n"

		// when
		refs := pipeline.ScanFileForActions(content, ".github/workflows/ci.yml")

		// then
		require.Len(t, refs, 1)
		assert.Equal(t, "actions", refs[0].Owner)
		assert.Equal(t, "checkout", refs[0].Repo)
		assert.Equal(t, "v4", refs[0].CurrentRef)
		assert.Equal(t, pipeline.RefStyleMajor, refs[0].RefStyle)
	})

	t.Run("should detect full semver action references", func(t *testing.T) {
		t.Parallel()

		// given
		content := "    - uses: actions/setup-go@v5.1.2\n"

		// when
		refs := pipeline.ScanFileForActions(content, ".github/workflows/ci.yml")

		// then
		require.Len(t, refs, 1)
		assert.Equal(t, "v5.1.2", refs[0].CurrentRef)
		assert.Equal(t, pipeline.RefStyleSemver, refs[0].RefStyle)
	})

	t.Run("should skip SHA-pinned actions", func(t *testing.T) {
		t.Parallel()

		// given
		content := "    - uses: actions/checkout@abc123def456789012345678901234567890abcd\n"

		// when
		refs := pipeline.ScanFileForActions(content, ".github/workflows/ci.yml")

		// then
		assert.Empty(t, refs)
	})

	t.Run("should skip branch-pinned actions", func(t *testing.T) {
		t.Parallel()

		// given
		content := "    - uses: actions/checkout@main\n"

		// when
		refs := pipeline.ScanFileForActions(content, ".github/workflows/ci.yml")

		// then
		assert.Empty(t, refs)
	})

	t.Run("should skip reusable workflow references", func(t *testing.T) {
		t.Parallel()

		// given
		content := "    uses: rios0rios0/pipelines/.github/workflows/go-binary.yaml@main\n"

		// when
		refs := pipeline.ScanFileForActions(content, ".github/workflows/ci.yml")

		// then
		assert.Empty(t, refs)
	})

	t.Run("should detect single-quoted action references", func(t *testing.T) {
		t.Parallel()

		// given
		content := "    - uses: 'actions/checkout@v4'\n"

		// when
		refs := pipeline.ScanFileForActions(content, ".github/workflows/ci.yml")

		// then
		require.Len(t, refs, 1)
		assert.Equal(t, "actions", refs[0].Owner)
		assert.Equal(t, "checkout", refs[0].Repo)
		assert.Equal(t, "v4", refs[0].CurrentRef)
	})

	t.Run("should detect double-quoted action references", func(t *testing.T) {
		t.Parallel()

		// given
		content := "    - uses: \"actions/checkout@v4\"\n"

		// when
		refs := pipeline.ScanFileForActions(content, ".github/workflows/ci.yml")

		// then
		require.Len(t, refs, 1)
		assert.Equal(t, "v4", refs[0].CurrentRef)
	})

	t.Run("should detect action references with trailing inline comment excluding comment from FullMatch", func(t *testing.T) {
		t.Parallel()

		// given
		content := "    - uses: actions/checkout@v4 # pinned to v4\n"

		// when
		refs := pipeline.ScanFileForActions(content, ".github/workflows/ci.yml")

		// then
		require.Len(t, refs, 1)
		assert.Equal(t, "v4", refs[0].CurrentRef)
		assert.Equal(t, "uses: actions/checkout@v4", refs[0].FullMatch)
	})

	t.Run("should detect multiple actions in one file", func(t *testing.T) {
		t.Parallel()

		// given
		content := `steps:
  - uses: actions/checkout@v4
  - uses: actions/setup-go@v5
  - uses: docker/build-push-action@v6
`

		// when
		refs := pipeline.ScanFileForActions(content, ".github/workflows/ci.yml")

		// then
		assert.Len(t, refs, 3)
		assert.Equal(t, "actions", refs[0].Owner)
		assert.Equal(t, "checkout", refs[0].Repo)
		assert.Equal(t, "actions", refs[1].Owner)
		assert.Equal(t, "setup-go", refs[1].Repo)
		assert.Equal(t, "docker", refs[2].Owner)
		assert.Equal(t, "build-push-action", refs[2].Repo)
	})
}

func TestClassifyRefStyle(t *testing.T) {
	t.Parallel()

	t.Run("should classify major-only ref", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pipeline.ClassifyRefStyle("v4")

		// then
		assert.Equal(t, pipeline.RefStyleMajor, result)
	})

	t.Run("should classify two-part ref as semver", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pipeline.ClassifyRefStyle("v4.1")

		// then
		assert.Equal(t, pipeline.RefStyleSemver, result)
	})

	t.Run("should classify three-part ref as semver", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pipeline.ClassifyRefStyle("v4.1.2")

		// then
		assert.Equal(t, pipeline.RefStyleSemver, result)
	})
}

func TestNormalizeActionVersion(t *testing.T) {
	t.Parallel()

	t.Run("should expand major-only to three parts", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pipeline.NormalizeActionVersion("v4")

		// then
		assert.Equal(t, "v4.0.0", result)
	})

	t.Run("should expand two-part to three parts", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pipeline.NormalizeActionVersion("v4.1")

		// then
		assert.Equal(t, "v4.1.0", result)
	})

	t.Run("should keep three-part version as is", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pipeline.NormalizeActionVersion("v4.1.2")

		// then
		assert.Equal(t, "v4.1.2", result)
	})

	t.Run("should add v prefix when missing", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pipeline.NormalizeActionVersion("4.1.2")

		// then
		assert.Equal(t, "v4.1.2", result)
	})
}

func TestExtractMajor(t *testing.T) {
	t.Parallel()

	t.Run("should extract major from major-only ref", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, 4, pipeline.ExtractMajor("v4"))
	})

	t.Run("should extract major from full semver ref", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, 5, pipeline.ExtractMajor("v5.1.2"))
	})

	t.Run("should return -1 for invalid ref", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, -1, pipeline.ExtractMajor("main"))
	})
}

func TestDetermineActionUpgrade(t *testing.T) {
	t.Parallel()

	t.Run("should upgrade major version when newer major exists", func(t *testing.T) {
		t.Parallel()

		// given
		ref := pipeline.ActionRef{
			Owner: "actions", Repo: "checkout", CurrentRef: "v4",
			RefStyle: pipeline.RefStyleMajor,
		}
		tags := []string{"v5.0.0", "v4.2.0", "v4.1.0", "v3.0.0"}

		// when
		up := pipeline.DetermineActionUpgrade(ref, tags)

		// then
		require.NotNil(t, up)
		assert.Equal(t, "v5", pipeline.ActionUpgradeNewRef(up))
	})

	t.Run("should return nil when already on latest major", func(t *testing.T) {
		t.Parallel()

		// given
		ref := pipeline.ActionRef{
			Owner: "actions", Repo: "checkout", CurrentRef: "v5",
			RefStyle: pipeline.RefStyleMajor,
		}
		tags := []string{"v5.0.0", "v4.2.0"}

		// when
		up := pipeline.DetermineActionUpgrade(ref, tags)

		// then
		assert.Nil(t, up)
	})

	t.Run("should upgrade semver within same major", func(t *testing.T) {
		t.Parallel()

		// given
		ref := pipeline.ActionRef{
			Owner: "actions", Repo: "setup-go", CurrentRef: "v5.1.0",
			RefStyle: pipeline.RefStyleSemver,
		}
		tags := []string{"v6.0.0", "v5.3.0", "v5.2.0", "v5.1.0"}

		// when
		up := pipeline.DetermineActionUpgrade(ref, tags)

		// then
		require.NotNil(t, up)
		assert.Equal(t, "v5.3.0", pipeline.ActionUpgradeNewRef(up))
	})

	t.Run("should not cross major for semver pins", func(t *testing.T) {
		t.Parallel()

		// given
		ref := pipeline.ActionRef{
			Owner: "actions", Repo: "setup-go", CurrentRef: "v5.3.0",
			RefStyle: pipeline.RefStyleSemver,
		}
		tags := []string{"v6.0.0", "v5.3.0"}

		// when
		up := pipeline.DetermineActionUpgrade(ref, tags)

		// then
		assert.Nil(t, up)
	})

	t.Run("should return nil when no tags available", func(t *testing.T) {
		t.Parallel()

		// given
		ref := pipeline.ActionRef{
			Owner: "actions", Repo: "checkout", CurrentRef: "v4",
			RefStyle: pipeline.RefStyleMajor,
		}

		// when
		up := pipeline.DetermineActionUpgrade(ref, nil)

		// then
		assert.Nil(t, up)
	})
}

func TestFindActionUpgradesInFile(t *testing.T) {
	t.Parallel()

	t.Run("should return upgrade tasks for outdated actions", func(t *testing.T) {
		t.Parallel()

		// given
		content := `steps:
  - uses: actions/checkout@v3
  - uses: actions/setup-go@v4
`
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithTags([]string{"v5.0.0", "v4.0.0", "v3.0.0"}).
			BuildSpy()
		cache := make(pipeline.ActionTagCache)

		// when
		tasks := pipeline.FindActionUpgradesInFile(
			t.Context(), provider, content, ".github/workflows/ci.yml", cache,
		)

		// then
		require.Len(t, tasks, 2)
		assert.Equal(t, "action:actions/checkout", pipeline.UpgradeTaskLanguage(tasks[0]))
		assert.Equal(t, "v3", pipeline.UpgradeTaskCurrentVer(tasks[0]))
		assert.Equal(t, "v5", pipeline.UpgradeTaskNewVersion(tasks[0]))
		assert.Equal(t, "action:actions/setup-go", pipeline.UpgradeTaskLanguage(tasks[1]))
		assert.Equal(t, "v5", pipeline.UpgradeTaskNewVersion(tasks[1]))
	})

	t.Run("should cache tags between calls for same owner/repo", func(t *testing.T) {
		t.Parallel()

		// given
		content := `steps:
  - uses: actions/checkout@v3
`
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithTags([]string{"v5.0.0", "v4.0.0"}).
			BuildSpy()
		cache := make(pipeline.ActionTagCache)

		// when
		_ = pipeline.FindActionUpgradesInFile(
			t.Context(), provider, content, ".github/workflows/ci.yml", cache,
		)
		_ = pipeline.FindActionUpgradesInFile(
			t.Context(), provider, content, ".github/workflows/other.yml", cache,
		)

		// then
		assert.Len(t, cache, 1)
		assert.Contains(t, cache, "actions/checkout")
	})

	t.Run("should emit one task per occurrence for duplicate action refs", func(t *testing.T) {
		t.Parallel()

		// given
		content := `jobs:
  job1:
    steps:
      - uses: actions/checkout@v3
  job2:
    steps:
      - uses: actions/checkout@v3
`
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithTags([]string{"v5.0.0", "v4.0.0"}).
			BuildSpy()
		cache := make(pipeline.ActionTagCache)

		// when
		tasks := pipeline.FindActionUpgradesInFile(
			t.Context(), provider, content, ".github/workflows/ci.yml", cache,
		)

		// then
		assert.Len(t, tasks, 2)
	})
}

func TestSanitizeBranchSegment(t *testing.T) {
	t.Parallel()

	t.Run("should replace colon and slash with dash", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pipeline.SanitizeBranchSegment("action:actions/checkout")

		// then
		assert.Equal(t, "action-actions-checkout", result)
	})

	t.Run("should not modify simple strings", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pipeline.SanitizeBranchSegment("golang")

		// then
		assert.Equal(t, "golang", result)
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
