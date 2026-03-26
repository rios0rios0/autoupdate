//go:build unit

package pipeline_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/pipeline"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestLocalScanAndDetermineUpgrades(t *testing.T) {
	t.Parallel()

	t.Run("should find version references in Azure DevOps pipeline files", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		adoDir := root + "/azure-devops"
		require.NoError(t, os.MkdirAll(adoDir, 0o755))

		content := `steps:
  - task: UsePythonVersion@0
    inputs:
      versionSpec: '3.12'
    displayName: 'Use Python 3.12'
`
		require.NoError(t, os.WriteFile(adoDir+"/build.yaml", []byte(content), 0o644))

		latestVersions := map[string]string{
			"python": "3.13.1",
		}

		// when
		upgrades, fileContents := pipeline.LocalScanAndDetermineUpgrades(
			t.Context(), root, nil, latestVersions,
		)

		// then
		require.Len(t, upgrades, 1)
		assert.Equal(t, "python", pipeline.UpgradeTaskLanguage(upgrades[0]))
		assert.Equal(t, "3.12", pipeline.UpgradeTaskCurrentVer(upgrades[0]))
		assert.Equal(t, "3.13", pipeline.UpgradeTaskNewVersion(upgrades[0]))
		assert.Contains(t, fileContents, "azure-devops/build.yaml")
	})

	t.Run("should skip hidden directories like .github in local filesystem walk", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		ghDir := root + "/.github/workflows"
		require.NoError(t, os.MkdirAll(ghDir, 0o755))

		content := `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22.0'
`
		require.NoError(t, os.WriteFile(ghDir+"/ci.yaml", []byte(content), 0o644))

		latestVersions := map[string]string{
			"golang": "1.24.1",
		}

		// when
		upgrades, _ := pipeline.LocalScanAndDetermineUpgrades(
			t.Context(), root, nil, latestVersions,
		)

		// then
		// WalkFilesByExtension skips hidden directories (.github),
		// so no upgrades are found for GitHub Actions in local mode
		assert.Empty(t, upgrades)
	})

	t.Run("should return empty for files with no version refs", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		adoDir := root + "/azure-devops"
		require.NoError(t, os.MkdirAll(adoDir, 0o755))

		content := `steps:
  - script: echo "Hello World"
    displayName: 'Run a script'
`
		require.NoError(t, os.WriteFile(adoDir+"/build.yaml", []byte(content), 0o644))

		latestVersions := map[string]string{
			"python": "3.13.1",
			"golang": "1.24.1",
		}

		// when
		upgrades, fileContents := pipeline.LocalScanAndDetermineUpgrades(
			t.Context(), root, nil, latestVersions,
		)

		// then
		assert.Empty(t, upgrades)
		assert.Empty(t, fileContents)
	})

	t.Run("should return empty when all versions are up to date", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		adoDir := root + "/azure-devops"
		require.NoError(t, os.MkdirAll(adoDir, 0o755))

		content := `steps:
  - task: UsePythonVersion@0
    inputs:
      versionSpec: '3.13'
    displayName: 'Use Python 3.13'
`
		require.NoError(t, os.WriteFile(adoDir+"/build.yaml", []byte(content), 0o644))

		latestVersions := map[string]string{
			"python": "3.13.1",
		}

		// when
		upgrades, _ := pipeline.LocalScanAndDetermineUpgrades(
			t.Context(), root, nil, latestVersions,
		)

		// then
		assert.Empty(t, upgrades)
	})

	t.Run("should find multiple version references across files", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		adoDir := root + "/azure-devops"
		require.NoError(t, os.MkdirAll(adoDir, 0o755))

		pythonContent := `steps:
  - task: UsePythonVersion@0
    inputs:
      versionSpec: '3.11'
`
		goContent := `steps:
  - task: GoTool@0
    inputs:
      version: '1.21'
`
		require.NoError(t, os.WriteFile(adoDir+"/python.yaml", []byte(pythonContent), 0o644))
		require.NoError(t, os.WriteFile(adoDir+"/go.yml", []byte(goContent), 0o644))

		latestVersions := map[string]string{
			"python": "3.13.1",
			"golang": "1.24.1",
		}

		// when
		upgrades, fileContents := pipeline.LocalScanAndDetermineUpgrades(
			t.Context(), root, nil, latestVersions,
		)

		// then
		assert.Len(t, upgrades, 2)
		assert.Len(t, fileContents, 2)
	})

	t.Run("should find version references in .yml files", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		adoDir := root + "/azure-devops"
		require.NoError(t, os.MkdirAll(adoDir, 0o755))

		content := `steps:
  - task: NodeTool@0
    inputs:
      version: '18.0.0'
`
		require.NoError(t, os.WriteFile(adoDir+"/build.yml", []byte(content), 0o644))

		latestVersions := map[string]string{
			"nodejs": "22.0.0",
		}

		// when
		upgrades, fileContents := pipeline.LocalScanAndDetermineUpgrades(
			t.Context(), root, nil, latestVersions,
		)

		// then
		require.Len(t, upgrades, 1)
		assert.Equal(t, "nodejs", pipeline.UpgradeTaskLanguage(upgrades[0]))
		assert.Equal(t, "18.0.0", pipeline.UpgradeTaskCurrentVer(upgrades[0]))
		assert.Equal(t, "22.0.0", pipeline.UpgradeTaskNewVersion(upgrades[0]))
		assert.Contains(t, fileContents, "azure-devops/build.yml")
	})

	t.Run("should skip non-pipeline YAML files", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		require.NoError(t, os.WriteFile(root+"/config.yaml", []byte("python-version: '3.10'"), 0o644))

		latestVersions := map[string]string{
			"python": "3.13.1",
		}

		// when
		upgrades, _ := pipeline.LocalScanAndDetermineUpgrades(
			t.Context(), root, nil, latestVersions,
		)

		// then
		assert.Empty(t, upgrades)
	})
}

func TestApplyUpdatesLocal(t *testing.T) {
	t.Parallel()

	t.Run("should return ErrNoUpdatesNeeded when no versions need upgrading", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		adoDir := root + "/azure-devops"
		require.NoError(t, os.MkdirAll(adoDir, 0o755))

		content := `steps:
  - script: echo "Hello"
`
		require.NoError(t, os.WriteFile(adoDir+"/build.yaml", []byte(content), 0o644))

		updater := pipeline.NewUpdaterRepository()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}

		// when
		result, err := updater.(*pipeline.UpdaterRepository).ApplyUpdates(
			t.Context(), root, nil, repo, opts,
		)

		// then
		assert.Nil(t, result)
		assert.ErrorIs(t, err, repositories.ErrNoUpdatesNeeded)
	})
}

func TestCreateUpgradePR(t *testing.T) {
	t.Parallel()

	t.Run("should create branch and PR with correct parameters", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": false}).
			BuildSpy()
		repo := entities.Repository{
			Organization:  "org",
			Name:          "repo",
			DefaultBranch: "refs/heads/main",
		}
		opts := entities.UpdateOptions{AutoComplete: true}
		upgrades := []pipeline.UpgradeTask{
			pipeline.NewUpgradeTaskWithFullMatch("python", "3.12", "3.13", "azure-devops/build.yml", "versionSpec: '3.12'"),
		}
		fileContents := map[string]string{
			"azure-devops/build.yml": "versionSpec: '3.12'",
		}

		// when
		prs, err := pipeline.CreateUpgradePR(t.Context(), provider, repo, opts, upgrades, fileContents)

		// then
		require.NoError(t, err)
		require.Len(t, prs, 1)
		assert.Equal(t, 1, prs[0].ID)

		require.Len(t, provider.BranchInputs, 1)
		assert.Equal(t, "chore/upgrade-pipeline-python-3.13", provider.BranchInputs[0].BranchName)
		assert.Equal(t, "refs/heads/main", provider.BranchInputs[0].BaseBranch)
		assert.Contains(t, provider.BranchInputs[0].CommitMessage, "python")

		require.Len(t, provider.PRInputs, 1)
		assert.Equal(t, "refs/heads/chore/upgrade-pipeline-python-3.13", provider.PRInputs[0].SourceBranch)
		assert.Equal(t, "refs/heads/main", provider.PRInputs[0].TargetBranch)
		assert.True(t, provider.PRInputs[0].AutoComplete)
		assert.Contains(t, provider.PRInputs[0].Title, "python")
	})

	t.Run("should skip when PR already exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithPRExistsResult(true).
			BuildSpy()
		repo := entities.Repository{
			Organization:  "org",
			Name:          "repo",
			DefaultBranch: "refs/heads/main",
		}
		opts := entities.UpdateOptions{}
		upgrades := []pipeline.UpgradeTask{
			pipeline.NewUpgradeTaskWithFullMatch("golang", "1.21", "1.22", ".github/workflows/ci.yml", "go-version: '1.21'"),
		}
		fileContents := map[string]string{
			".github/workflows/ci.yml": "go-version: '1.21'",
		}

		// when
		prs, err := pipeline.CreateUpgradePR(t.Context(), provider, repo, opts, upgrades, fileContents)

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
		assert.Empty(t, provider.BranchInputs)
		assert.Empty(t, provider.PRInputs)
	})

	t.Run("should use target branch from options when specified", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": false}).
			BuildSpy()
		repo := entities.Repository{
			Organization:  "org",
			Name:          "repo",
			DefaultBranch: "refs/heads/main",
		}
		opts := entities.UpdateOptions{TargetBranch: "develop"}
		upgrades := []pipeline.UpgradeTask{
			pipeline.NewUpgradeTaskWithFullMatch("python", "3.12", "3.13", "azure-devops/build.yml", "versionSpec: '3.12'"),
		}
		fileContents := map[string]string{
			"azure-devops/build.yml": "versionSpec: '3.12'",
		}

		// when
		prs, err := pipeline.CreateUpgradePR(t.Context(), provider, repo, opts, upgrades, fileContents)

		// then
		require.NoError(t, err)
		require.Len(t, prs, 1)

		assert.Equal(t, "refs/heads/develop", provider.BranchInputs[0].BaseBranch)
		assert.Equal(t, "refs/heads/develop", provider.PRInputs[0].TargetBranch)
	})

	t.Run("should return error when branch creation fails", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithCreateBranchErr(fmt.Errorf("branch creation failed")).
			BuildSpy()
		repo := entities.Repository{
			Organization:  "org",
			Name:          "repo",
			DefaultBranch: "refs/heads/main",
		}
		opts := entities.UpdateOptions{}
		upgrades := []pipeline.UpgradeTask{
			pipeline.NewUpgradeTaskWithFullMatch("python", "3.12", "3.13", "azure-devops/build.yml", "versionSpec: '3.12'"),
		}
		fileContents := map[string]string{
			"azure-devops/build.yml": "versionSpec: '3.12'",
		}

		// when
		prs, err := pipeline.CreateUpgradePR(t.Context(), provider, repo, opts, upgrades, fileContents)

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create branch")
		assert.Nil(t, prs)
	})

	t.Run("should return error when PR creation fails", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithCreatePRErr(fmt.Errorf("PR creation failed")).
			WithExistingFiles(map[string]bool{"CHANGELOG.md": false}).
			BuildSpy()
		repo := entities.Repository{
			Organization:  "org",
			Name:          "repo",
			DefaultBranch: "refs/heads/main",
		}
		opts := entities.UpdateOptions{}
		upgrades := []pipeline.UpgradeTask{
			pipeline.NewUpgradeTaskWithFullMatch("python", "3.12", "3.13", "azure-devops/build.yml", "versionSpec: '3.12'"),
		}
		fileContents := map[string]string{
			"azure-devops/build.yml": "versionSpec: '3.12'",
		}

		// when
		prs, err := pipeline.CreateUpgradePR(t.Context(), provider, repo, opts, upgrades, fileContents)

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create PR")
		assert.Nil(t, prs)
	})

	t.Run("should append CHANGELOG entry when CHANGELOG.md exists", func(t *testing.T) {
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
		repo := entities.Repository{
			Organization:  "org",
			Name:          "repo",
			DefaultBranch: "refs/heads/main",
		}
		opts := entities.UpdateOptions{}
		upgrades := []pipeline.UpgradeTask{
			pipeline.NewUpgradeTaskWithFullMatch("python", "3.12", "3.13", "azure-devops/build.yml", "versionSpec: '3.12'"),
		}
		fileContents := map[string]string{
			"azure-devops/build.yml": "versionSpec: '3.12'",
		}

		// when
		prs, err := pipeline.CreateUpgradePR(t.Context(), provider, repo, opts, upgrades, fileContents)

		// then
		require.NoError(t, err)
		require.Len(t, prs, 1)

		// Verify changelog was included in the branch changes
		require.Len(t, provider.BranchInputs, 1)
		changes := provider.BranchInputs[0].Changes
		hasChangelog := false
		for _, change := range changes {
			if change.Path == "CHANGELOG.md" {
				hasChangelog = true
				assert.Contains(t, change.Content, "changed the python pipeline version")
			}
		}
		assert.True(t, hasChangelog)
	})
}

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

	t.Run("should return false when provider returns error from ListFiles", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithListFileErr(fmt.Errorf("API rate limit exceeded")).
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

func TestClassifyFile(t *testing.T) {
	t.Parallel()

	t.Run("should classify GitHub Actions workflow files", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pipeline.ClassifyFile(".github/workflows/ci.yml")

		// then
		assert.Equal(t, pipeline.CIGitHubActions, result)
	})

	t.Run("should classify azure-devops directory files", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pipeline.ClassifyFile("azure-devops/build.yml")

		// then
		assert.Equal(t, pipeline.CIAzureDevOps, result)
	})

	t.Run("should classify azure-pipelines.yml", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pipeline.ClassifyFile("azure-pipelines.yml")

		// then
		assert.Equal(t, pipeline.CIAzureDevOps, result)
	})

	t.Run("should return empty for unknown paths", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := pipeline.ClassifyFile("src/main.go")

		// then
		assert.Equal(t, pipeline.CISystem(""), result)
	})
}

func TestGenerateBranchName(t *testing.T) {
	t.Parallel()

	t.Run("should format single upgrade branch name", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []pipeline.UpgradeTask{
			pipeline.NewUpgradeTask("python", "3.12", "3.13"),
		}

		// when
		result := pipeline.GenerateBranchName(tasks)

		// then
		assert.Contains(t, result, "python")
		assert.Contains(t, result, "3.13")
	})

	t.Run("should format batch upgrade branch name", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []pipeline.UpgradeTask{
			pipeline.NewUpgradeTask("python", "3.12", "3.13"),
			pipeline.NewUpgradeTask("go", "1.24", "1.25"),
			pipeline.NewUpgradeTask("java", "21", "22"),
		}

		// when
		result := pipeline.GenerateBranchName(tasks)

		// then
		assert.Contains(t, result, "3")
	})
}

func TestGenerateCommitMessage(t *testing.T) {
	t.Parallel()

	t.Run("should format single upgrade commit message", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []pipeline.UpgradeTask{
			pipeline.NewUpgradeTask("python", "3.12", "3.13"),
		}

		// when
		result := pipeline.GenerateCommitMessage(tasks)

		// then
		assert.Contains(t, result, "python")
		assert.Contains(t, result, "3.12")
		assert.Contains(t, result, "3.13")
	})

	t.Run("should format batch upgrade commit message", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []pipeline.UpgradeTask{
			pipeline.NewUpgradeTask("python", "3.12", "3.13"),
			pipeline.NewUpgradeTask("go", "1.24", "1.25"),
		}

		// when
		result := pipeline.GenerateCommitMessage(tasks)

		// then
		assert.Contains(t, result, "2")
	})
}

func TestGeneratePRTitle(t *testing.T) {
	t.Parallel()

	t.Run("should format single upgrade PR title", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []pipeline.UpgradeTask{
			pipeline.NewUpgradeTask("python", "3.12", "3.13"),
		}

		// when
		result := pipeline.GeneratePRTitle(tasks)

		// then
		assert.Contains(t, result, "python")
		assert.Contains(t, result, "3.13")
	})

	t.Run("should format batch upgrade PR title", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []pipeline.UpgradeTask{
			pipeline.NewUpgradeTask("python", "3.12", "3.13"),
			pipeline.NewUpgradeTask("go", "1.24", "1.25"),
		}

		// when
		result := pipeline.GeneratePRTitle(tasks)

		// then
		assert.Contains(t, result, "2")
	})
}

func TestGeneratePRDescription(t *testing.T) {
	t.Parallel()

	t.Run("should generate table for few upgrades", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []pipeline.UpgradeTask{
			pipeline.NewUpgradeTaskWithFullMatch("python", "3.12", "3.13", "azure-pipelines.yml", "versionSpec: '3.12'"),
			pipeline.NewUpgradeTaskWithFullMatch("go", "1.24", "1.25", ".github/workflows/ci.yml", "go-version: '1.24'"),
		}

		// when
		result := pipeline.GeneratePRDescription(tasks)

		// then
		assert.Contains(t, result, "| Language |")
		assert.Contains(t, result, "python")
		assert.Contains(t, result, "go")
		assert.Contains(t, result, "3.12")
		assert.Contains(t, result, "1.25")
	})

	t.Run("should generate summary for many upgrades", func(t *testing.T) {
		t.Parallel()

		// given
		var tasks []pipeline.UpgradeTask
		for i := range 6 {
			tasks = append(tasks, pipeline.NewUpgradeTask(
				fmt.Sprintf("lang-%d", i), "1.0", "2.0",
			))
		}

		// when
		result := pipeline.GeneratePRDescription(tasks)

		// then
		assert.Contains(t, result, "**6**")
		assert.NotContains(t, result, "| Language |")
	})
}
