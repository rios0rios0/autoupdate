//go:build unit

package terraform_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/terraform"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestExtractChangelogVersions(t *testing.T) {
	t.Parallel()

	t.Run("should extract version headings from a changelog", func(t *testing.T) {
		t.Parallel()

		// given
		content := `# Changelog

## [Unreleased]

## [6.15.0] - 2026-03-15

### Changed
- changed something

## [6.14.0] - 2026-03-01

### Added
- added something
`

		// when
		versions := terraform.ExtractChangelogVersions(content)

		// then
		assert.True(t, versions["6.15.0"])
		assert.True(t, versions["6.14.0"])
		assert.False(t, versions["Unreleased"])
		assert.Len(t, versions, 2)
	})

	t.Run("should return empty map when changelog has no version headings", func(t *testing.T) {
		t.Parallel()

		// given
		content := "# Changelog\n\nNo releases yet.\n"

		// when
		versions := terraform.ExtractChangelogVersions(content)

		// then
		assert.Empty(t, versions)
	})
}

func TestFindLatestChangelogVersion(t *testing.T) {
	t.Parallel()

	t.Run("should pick the highest tag present in changelog, skipping non-production tags", func(t *testing.T) {
		t.Parallel()

		// given
		changelog := `# Changelog

## [Unreleased]

## [6.15.0] - 2026-03-15

## [6.14.0] - 2026-03-01
`
		depRepo := entities.Repository{Organization: "org", Name: "app"}
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": changelog}).
			BuildSpy()
		tags := []string{"6.16.0", "6.15.0", "6.14.0"}

		// when
		result := terraform.FindLatestChangelogVersion(t.Context(), provider, &depRepo, tags)

		// then
		assert.Equal(t, "6.15.0", result)
	})

	t.Run("should match v-prefixed tags against changelog headings without v prefix", func(t *testing.T) {
		t.Parallel()

		// given
		changelog := `# Changelog

## [Unreleased]

## [6.15.0] - 2026-03-15

## [6.14.0] - 2026-03-01
`
		depRepo := entities.Repository{Organization: "org", Name: "app"}
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": changelog}).
			BuildSpy()
		tags := []string{"v6.16.0", "v6.15.0", "v6.14.0"}

		// when
		result := terraform.FindLatestChangelogVersion(t.Context(), provider, &depRepo, tags)

		// then
		assert.Equal(t, "v6.15.0", result)
	})

	t.Run("should match plain tags against v-prefixed changelog headings", func(t *testing.T) {
		t.Parallel()

		// given
		changelog := `# Changelog

## [Unreleased]

## [v6.15.0] - 2026-03-15
`
		depRepo := entities.Repository{Organization: "org", Name: "app"}
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": changelog}).
			BuildSpy()
		tags := []string{"6.16.0", "6.15.0"}

		// when
		result := terraform.FindLatestChangelogVersion(t.Context(), provider, &depRepo, tags)

		// then
		assert.Equal(t, "6.15.0", result)
	})

	t.Run("should fall back to tags[0] when dependency repo has no changelog", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		depRepo := entities.Repository{Organization: "org", Name: "app"}
		tags := []string{"6.16.0", "6.15.0"}

		// when
		result := terraform.FindLatestChangelogVersion(t.Context(), provider, &depRepo, tags)

		// then
		assert.Equal(t, "6.16.0", result)
	})

	t.Run("should fall back to tags[0] when dep repo is nil", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy()
		tags := []string{"6.16.0", "6.15.0"}

		// when
		result := terraform.FindLatestChangelogVersion(t.Context(), provider, nil, tags)

		// then
		assert.Equal(t, "6.16.0", result)
	})

	t.Run("should fall back to tags[0] when no tags match any changelog version", func(t *testing.T) {
		t.Parallel()

		// given
		changelog := `# Changelog

## [Unreleased]

## [5.0.0] - 2026-01-01
`
		depRepo := entities.Repository{Organization: "org", Name: "app"}
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": changelog}).
			BuildSpy()
		tags := []string{"6.16.0", "6.15.0"}

		// when
		result := terraform.FindLatestChangelogVersion(t.Context(), provider, &depRepo, tags)

		// then
		assert.Equal(t, "6.16.0", result)
	})
}

func TestName(t *testing.T) {
	t.Parallel()

	t.Run("should return terraform as updater name", func(t *testing.T) {
		t.Parallel()

		// given
		updater := terraform.NewUpdaterRepository()

		// when
		name := updater.Name()

		// then
		assert.Equal(t, "terraform", name)
	})
}

func TestDetect(t *testing.T) {
	t.Parallel()

	t.Run("should return true when .tf files exist", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{
				{Path: "main.tf", IsDir: false},
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := terraform.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.True(t, detected)
	})

	t.Run("should return false when no Terraform files exist", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := terraform.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.False(t, detected)
	})
}

func TestCreateUpdatePRs(t *testing.T) {
	t.Parallel()

	t.Run("should return empty when no Terraform files found", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}

		// when
		prs, err := terraform.NewUpdaterRepository().CreateUpdatePRs(t.Context(), provider, repo, opts)

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
	})
}

func TestIsSemverLike(t *testing.T) {
	t.Parallel()

	t.Run("should return true for standard semver", func(t *testing.T) {
		t.Parallel()

		// given
		version := "1.2.3"

		// when
		result := terraform.IsSemverLike(version)

		// then
		assert.True(t, result)
	})

	t.Run("should return true for v-prefixed semver", func(t *testing.T) {
		t.Parallel()

		// given
		version := "v0.7.0"

		// when
		result := terraform.IsSemverLike(version)

		// then
		assert.True(t, result)
	})

	t.Run("should return false for non-semver string", func(t *testing.T) {
		t.Parallel()

		// given
		version := "latest"

		// when
		result := terraform.IsSemverLike(version)

		// then
		assert.False(t, result)
	})

	t.Run("should return false for empty string", func(t *testing.T) {
		t.Parallel()

		// given
		version := ""

		// when
		result := terraform.IsSemverLike(version)

		// then
		assert.False(t, result)
	})
}

func TestIsGitModule(t *testing.T) {
	t.Parallel()

	t.Run("should return true for git:: prefix", func(t *testing.T) {
		t.Parallel()

		// given
		source := "git::https://example.com/module"

		// when
		result := terraform.IsGitModule(source)

		// then
		assert.True(t, result)
	})

	t.Run("should return true for git@ prefix", func(t *testing.T) {
		t.Parallel()

		// given
		source := "git@github.com:org/module"

		// when
		result := terraform.IsGitModule(source)

		// then
		assert.True(t, result)
	})

	t.Run("should return true for github.com URL", func(t *testing.T) {
		t.Parallel()

		// given
		source := "github.com/org/module"

		// when
		result := terraform.IsGitModule(source)

		// then
		assert.True(t, result)
	})

	t.Run("should return true for gitlab.com URL", func(t *testing.T) {
		t.Parallel()

		// given
		source := "gitlab.com/org/module"

		// when
		result := terraform.IsGitModule(source)

		// then
		assert.True(t, result)
	})

	t.Run("should return true for dev.azure.com URL", func(t *testing.T) {
		t.Parallel()

		// given
		source := "dev.azure.com/org/project/_git/repo"

		// when
		result := terraform.IsGitModule(source)

		// then
		assert.True(t, result)
	})

	t.Run("should return true for _git/ path", func(t *testing.T) {
		t.Parallel()

		// given
		source := "ssh://org@dev.azure.com/v3/org/project/_git/repo"

		// when
		result := terraform.IsGitModule(source)

		// then
		assert.True(t, result)
	})

	t.Run("should return false for registry module", func(t *testing.T) {
		t.Parallel()

		// given
		source := "hashicorp/consul/aws"

		// when
		result := terraform.IsGitModule(source)

		// then
		assert.False(t, result)
	})
}

func TestExtractVersion(t *testing.T) {
	t.Parallel()

	t.Run("should extract version from ?ref= parameter", func(t *testing.T) {
		t.Parallel()

		// given
		source := "git::https://github.com/org/mod.git?ref=v1.0.0"

		// when
		result := terraform.ExtractVersion(source)

		// then
		assert.Equal(t, "v1.0.0", result)
	})

	t.Run("should extract version from ref= without question mark", func(t *testing.T) {
		t.Parallel()

		// given
		source := "git::https://github.com/org/mod.git?depth=1&ref=v2.0.0"

		// when
		result := terraform.ExtractVersion(source)

		// then
		assert.Equal(t, "v2.0.0", result)
	})

	t.Run("should return empty when no ref parameter exists", func(t *testing.T) {
		t.Parallel()

		// given
		source := "git::https://github.com/org/mod.git"

		// when
		result := terraform.ExtractVersion(source)

		// then
		assert.Empty(t, result)
	})
}

func TestRemoveVersionFromSource(t *testing.T) {
	t.Parallel()

	t.Run("should remove ?ref= from source", func(t *testing.T) {
		t.Parallel()

		// given
		source := "git::https://github.com/org/mod.git?ref=v1.0.0"

		// when
		result := terraform.RemoveVersionFromSource(source)

		// then
		assert.Equal(t, "git::https://github.com/org/mod.git", result)
	})

	t.Run("should return source unchanged when no ref", func(t *testing.T) {
		t.Parallel()

		// given
		source := "git::https://github.com/org/mod.git"

		// when
		result := terraform.RemoveVersionFromSource(source)

		// then
		assert.Equal(t, "git::https://github.com/org/mod.git", result)
	})
}

func TestExtractRepoName(t *testing.T) {
	t.Parallel()

	t.Run("should extract last path segment", func(t *testing.T) {
		t.Parallel()

		// given
		source := "github.com/org/my-module"

		// when
		result := terraform.ExtractRepoName(source)

		// then
		assert.Equal(t, "my-module", result)
	})

	t.Run("should return source when no slashes", func(t *testing.T) {
		t.Parallel()

		// given
		source := "my-module"

		// when
		result := terraform.ExtractRepoName(source)

		// then
		assert.Equal(t, "my-module", result)
	})
}

func TestIsNewerVersion(t *testing.T) {
	t.Parallel()

	t.Run("should return true when new version is higher", func(t *testing.T) {
		t.Parallel()

		// given
		current := "1.0.0"
		newVersion := "2.0.0"

		// when
		result := terraform.IsNewerVersion(current, newVersion)

		// then
		assert.True(t, result)
	})

	t.Run("should return false when new version is lower", func(t *testing.T) {
		t.Parallel()

		// given
		current := "2.0.0"
		newVersion := "1.0.0"

		// when
		result := terraform.IsNewerVersion(current, newVersion)

		// then
		assert.False(t, result)
	})

	t.Run("should return false when versions are equal", func(t *testing.T) {
		t.Parallel()

		// given
		current := "1.0.0"
		newVersion := "1.0.0"

		// when
		result := terraform.IsNewerVersion(current, newVersion)

		// then
		assert.False(t, result)
	})

	t.Run("should handle v-prefixed versions", func(t *testing.T) {
		t.Parallel()

		// given
		current := "v1.0.0"
		newVersion := "v2.0.0"

		// when
		result := terraform.IsNewerVersion(current, newVersion)

		// then
		assert.True(t, result)
	})

	t.Run("should fall back to string comparison for non-semver versions", func(t *testing.T) {
		t.Parallel()

		// given
		current := "abc-20240101"
		newVersion := "abc-20250101"

		// when
		result := terraform.IsNewerVersion(current, newVersion)

		// then
		assert.True(t, result)
	})

	t.Run("should return false for equal non-semver strings", func(t *testing.T) {
		t.Parallel()

		// given
		current := "main-snapshot"
		newVersion := "main-snapshot"

		// when
		result := terraform.IsNewerVersion(current, newVersion)

		// then
		assert.False(t, result)
	})
}

func TestNormalizeVersion(t *testing.T) {
	t.Parallel()

	t.Run("should add v prefix when missing", func(t *testing.T) {
		t.Parallel()

		// given
		version := "1.2.3"

		// when
		result := terraform.NormalizeVersion(version)

		// then
		assert.Equal(t, "v1.2.3", result)
	})

	t.Run("should keep existing v prefix", func(t *testing.T) {
		t.Parallel()

		// given
		version := "v1.2.3"

		// when
		result := terraform.NormalizeVersion(version)

		// then
		assert.Equal(t, "v1.2.3", result)
	})

	t.Run("should trim whitespace", func(t *testing.T) {
		t.Parallel()

		// given
		version := "  1.2.3  "

		// when
		result := terraform.NormalizeVersion(version)

		// then
		assert.Equal(t, "v1.2.3", result)
	})
}

func TestApplyVersionUpgrade(t *testing.T) {
	t.Parallel()

	t.Run("should replace version in source string", func(t *testing.T) {
		t.Parallel()

		// given
		content := `module "my_mod" {
  source = "git::https://github.com/org/my-module.git?ref=v1.0.0"
}`
		dep := entities.Dependency{
			Name:       "my_mod",
			Source:     "git::https://github.com/org/my-module.git",
			CurrentVer: "v1.0.0",
			FilePath:   "modules/main.tf",
			Line:       1,
		}

		// when
		result := terraform.ApplyVersionUpgrade(content, dep, "v2.0.0")

		// then
		assert.Contains(t, result, "?ref=v2.0.0")
		assert.NotContains(t, result, "?ref=v1.0.0")
	})

	t.Run("should use regex fallback for named module", func(t *testing.T) {
		t.Parallel()

		// given
		content := `module "my_mod" {
  source = "git::https://github.com/org/my-module.git?ref=v1.0.0"
}`
		dep := entities.Dependency{
			Name:       "my_mod",
			Source:     "git::https://github.com/org/different-source.git",
			CurrentVer: "v1.0.0",
			FilePath:   "modules/main.tf",
			Line:       1,
		}

		// when
		result := terraform.ApplyVersionUpgrade(content, dep, "v2.0.0")

		// then
		assert.Contains(t, result, "v2.0.0")
		assert.NotContains(t, result, "?ref=v1.0.0")
	})

	t.Run("should use generic ref pattern fallback when source and module name do not match", func(t *testing.T) {
		t.Parallel()

		// given
		content := `source = "git::https://github.com/org/my-module.git?ref=v1.0.0"`
		dep := entities.Dependency{
			Name:       "unrelated_name",
			Source:     "git::https://github.com/org/totally-different.git",
			CurrentVer: "v1.0.0",
			FilePath:   "modules/main.tf",
			Line:       1,
		}

		// when
		result := terraform.ApplyVersionUpgrade(content, dep, "v3.0.0")

		// then
		assert.Contains(t, result, "?ref=v3.0.0")
		assert.NotContains(t, result, "?ref=v1.0.0")
	})
}

func TestApplyImageVersionUpgrade(t *testing.T) {
	t.Parallel()

	t.Run("should replace image version", func(t *testing.T) {
		t.Parallel()

		// given
		content := `my_image = "app:1.0.0"`
		dep := entities.Dependency{
			Name:       "my_image",
			Source:     "app",
			CurrentVer: "1.0.0",
			FilePath:   "vars.hcl",
			Line:       1,
		}

		// when
		result := terraform.ApplyImageVersionUpgrade(content, dep, "2.0.0")

		// then
		assert.Contains(t, result, `"app:2.0.0"`)
		assert.NotContains(t, result, `"app:1.0.0"`)
	})
}

func TestBuildSourceWithVersion(t *testing.T) {
	t.Parallel()

	t.Run("should append ?ref= when source has no query", func(t *testing.T) {
		t.Parallel()

		// given
		source := "github.com/org/mod"
		version := "v1.0.0"

		// when
		result := terraform.BuildSourceWithVersion(source, version)

		// then
		assert.Equal(t, "github.com/org/mod?ref=v1.0.0", result)
	})

	t.Run("should replace existing ref", func(t *testing.T) {
		t.Parallel()

		// given
		source := "github.com/org/mod?ref=v0.5.0"
		version := "v1.0.0"

		// when
		result := terraform.BuildSourceWithVersion(source, version)

		// then
		assert.Equal(t, "github.com/org/mod?ref=v1.0.0", result)
	})

	t.Run("should append &ref= when source has other query params", func(t *testing.T) {
		t.Parallel()

		// given
		source := "github.com/org/mod?depth=1"
		version := "v1.0.0"

		// when
		result := terraform.BuildSourceWithVersion(source, version)

		// then
		assert.Equal(t, "github.com/org/mod?depth=1&ref=v1.0.0", result)
	})
}

func TestScanTerraformFile(t *testing.T) {
	t.Parallel()

	t.Run("should parse HCL module blocks", func(t *testing.T) {
		t.Parallel()

		// given
		content := `module "my_mod" {
  source = "git::https://github.com/org/my-module.git?ref=v1.0.0"
}`

		// when
		deps := terraform.ScanTerraformFile(content, "main.tf")

		// then
		require.Len(t, deps, 1)
		assert.Equal(t, "my_mod", deps[0].Name)
		assert.Equal(t, "git::https://github.com/org/my-module.git", deps[0].Source)
		assert.Equal(t, "v1.0.0", deps[0].CurrentVer)
		assert.Equal(t, "main.tf", deps[0].FilePath)
	})

	t.Run("should fall back to regex for invalid HCL", func(t *testing.T) {
		t.Parallel()

		// given
		content := `module "my_mod" {
  source = "git::https://github.com/org/my-module.git?ref=v1.0.0"
  # unclosed interpolation ${
}`

		// when
		deps := terraform.ScanTerraformFile(content, "main.tf")

		// then
		require.Len(t, deps, 1)
		assert.Equal(t, "my_mod", deps[0].Name)
		assert.Equal(t, "v1.0.0", deps[0].CurrentVer)
	})

	t.Run("should return empty for no modules", func(t *testing.T) {
		t.Parallel()

		// given
		content := `resource "aws_instance" "example" {
  ami           = "ami-12345"
  instance_type = "t2.micro"
}`

		// when
		deps := terraform.ScanTerraformFile(content, "main.tf")

		// then
		assert.Empty(t, deps)
	})

	t.Run("should skip non-git modules", func(t *testing.T) {
		t.Parallel()

		// given
		content := `module "consul" {
  source  = "hashicorp/consul/aws"
  version = "0.1.0"
}`

		// when
		deps := terraform.ScanTerraformFile(content, "main.tf")

		// then
		assert.Empty(t, deps)
	})
}

func TestScanWithRegex(t *testing.T) {
	t.Parallel()

	t.Run("should extract module from regex pattern", func(t *testing.T) {
		t.Parallel()

		// given
		content := `module "foo" {
  source = "git::https://github.com/org/my-module.git?ref=v1.0.0"
}`

		// when
		deps := terraform.ScanWithRegex(content, "main.tf")

		// then
		require.Len(t, deps, 1)
		assert.Equal(t, "foo", deps[0].Name)
		assert.Equal(t, "git::https://github.com/org/my-module.git", deps[0].Source)
		assert.Equal(t, "v1.0.0", deps[0].CurrentVer)
		assert.Equal(t, "main.tf", deps[0].FilePath)
	})

	t.Run("should return empty when no modules match", func(t *testing.T) {
		t.Parallel()

		// given
		content := `resource "aws_s3_bucket" "example" {
  bucket = "my-bucket"
}`

		// when
		deps := terraform.ScanWithRegex(content, "main.tf")

		// then
		assert.Empty(t, deps)
	})
}

func TestScanHCLFile(t *testing.T) {
	t.Parallel()

	t.Run("should extract image references", func(t *testing.T) {
		t.Parallel()

		// given
		content := `my_image = "app:0.7.0"`

		// when
		deps := terraform.ScanHCLFile(content, "vars.hcl")

		// then
		require.Len(t, deps, 1)
		assert.Equal(t, "my_image", deps[0].Name)
		assert.Equal(t, "app", deps[0].Source)
		assert.Equal(t, "0.7.0", deps[0].CurrentVer)
		assert.Equal(t, "vars.hcl", deps[0].FilePath)
	})

	t.Run("should skip non-semver tags", func(t *testing.T) {
		t.Parallel()

		// given
		content := `my_image = "app:latest"`

		// when
		deps := terraform.ScanHCLFile(content, "vars.hcl")

		// then
		assert.Empty(t, deps)
	})

	t.Run("should extract multiple images", func(t *testing.T) {
		t.Parallel()

		// given
		content := `first_image = "app-one:1.0.0"
second_image = "app-two:2.3.4"`

		// when
		deps := terraform.ScanHCLFile(content, "vars.hcl")

		// then
		require.Len(t, deps, 2)
		assert.Equal(t, "first_image", deps[0].Name)
		assert.Equal(t, "app-one", deps[0].Source)
		assert.Equal(t, "1.0.0", deps[0].CurrentVer)
		assert.Equal(t, "second_image", deps[1].Name)
		assert.Equal(t, "app-two", deps[1].Source)
		assert.Equal(t, "2.3.4", deps[1].CurrentVer)
	})
}

func TestCountByKind(t *testing.T) {
	t.Parallel()

	t.Run("should count modules and images separately", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(entities.Dependency{Name: "mod1", Source: "github.com/org/mod1"}, "v2.0.0", "", terraform.DepKindModule),
			terraform.NewUpgradeTask(entities.Dependency{Name: "img1", Source: "app"}, "2.0.0", "", terraform.DepKindImage),
			terraform.NewUpgradeTask(entities.Dependency{Name: "mod2", Source: "github.com/org/mod2"}, "v3.0.0", "", terraform.DepKindModule),
		}

		// when
		moduleCount, imageCount := terraform.CountByKind(tasks)

		// then
		assert.Equal(t, 2, moduleCount)
		assert.Equal(t, 1, imageCount)
	})

	t.Run("should handle all modules", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(entities.Dependency{Name: "mod1", Source: "github.com/org/mod1"}, "v2.0.0", "", terraform.DepKindModule),
			terraform.NewUpgradeTask(entities.Dependency{Name: "mod2", Source: "github.com/org/mod2"}, "v3.0.0", "", terraform.DepKindModule),
		}

		// when
		moduleCount, imageCount := terraform.CountByKind(tasks)

		// then
		assert.Equal(t, 2, moduleCount)
		assert.Equal(t, 0, imageCount)
	})

	t.Run("should handle all images", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(entities.Dependency{Name: "img1", Source: "app1"}, "2.0.0", "", terraform.DepKindImage),
			terraform.NewUpgradeTask(entities.Dependency{Name: "img2", Source: "app2"}, "3.0.0", "", terraform.DepKindImage),
		}

		// when
		moduleCount, imageCount := terraform.CountByKind(tasks)

		// then
		assert.Equal(t, 0, moduleCount)
		assert.Equal(t, 2, imageCount)
	})
}

func TestGenerateBranchName(t *testing.T) {
	t.Parallel()

	t.Run("should format single upgrade branch", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(
				entities.Dependency{Name: "my_mod", Source: "github.com/org/my-module"},
				"v2.0.0", "", terraform.DepKindModule,
			),
		}

		// when
		result := terraform.GenerateBranchName(tasks)

		// then
		assert.Equal(t, "chore/upgrade-my-module-v2.0.0", result)
	})

	t.Run("should format batch upgrade branch", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(entities.Dependency{Name: "mod1", Source: "github.com/org/mod1"}, "v2.0.0", "", terraform.DepKindModule),
			terraform.NewUpgradeTask(entities.Dependency{Name: "mod2", Source: "github.com/org/mod2"}, "v3.0.0", "", terraform.DepKindModule),
			terraform.NewUpgradeTask(entities.Dependency{Name: "img1", Source: "app"}, "2.0.0", "", terraform.DepKindImage),
		}

		// when
		result := terraform.GenerateBranchName(tasks)

		// then
		assert.Equal(t, "chore/upgrade-3-dependencies", result)
	})
}

func TestGenerateCommitMessage(t *testing.T) {
	t.Parallel()

	t.Run("should format single upgrade message", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(
				entities.Dependency{Name: "my_mod", Source: "github.com/org/my-module", CurrentVer: "v1.0.0"},
				"v2.0.0", "", terraform.DepKindModule,
			),
		}

		// when
		result := terraform.GenerateCommitMessage(tasks)

		// then
		assert.Equal(t, "chore(deps): upgraded `my-module` from `v1.0.0` to `v2.0.0`", result)
	})

	t.Run("should format batch upgrade message", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(entities.Dependency{Name: "mod1", Source: "github.com/org/mod1"}, "v2.0.0", "", terraform.DepKindModule),
			terraform.NewUpgradeTask(entities.Dependency{Name: "mod2", Source: "github.com/org/mod2"}, "v3.0.0", "", terraform.DepKindModule),
			terraform.NewUpgradeTask(entities.Dependency{Name: "img1", Source: "app"}, "2.0.0", "", terraform.DepKindImage),
		}

		// when
		result := terraform.GenerateCommitMessage(tasks)

		// then
		assert.Equal(t, "chore(deps): upgraded 3 Terraform dependencies", result)
	})
}

func TestGeneratePRTitle(t *testing.T) {
	t.Parallel()

	t.Run("should format single upgrade title", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(
				entities.Dependency{Name: "my_mod", Source: "github.com/org/my-module"},
				"v2.0.0", "", terraform.DepKindModule,
			),
		}

		// when
		result := terraform.GeneratePRTitle(tasks)

		// then
		assert.Equal(t, "chore(deps): upgraded `my-module` to `v2.0.0`", result)
	})

	t.Run("should format batch upgrade title", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(entities.Dependency{Name: "mod1", Source: "github.com/org/mod1"}, "v2.0.0", "", terraform.DepKindModule),
			terraform.NewUpgradeTask(entities.Dependency{Name: "mod2", Source: "github.com/org/mod2"}, "v3.0.0", "", terraform.DepKindModule),
			terraform.NewUpgradeTask(entities.Dependency{Name: "img1", Source: "app"}, "2.0.0", "", terraform.DepKindImage),
		}

		// when
		result := terraform.GeneratePRTitle(tasks)

		// then
		assert.Equal(t, "chore(deps): upgraded 3 Terraform dependencies", result)
	})
}

func TestGeneratePRDescription(t *testing.T) {
	t.Parallel()

	t.Run("should generate table for 5 or fewer upgrades", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(
				entities.Dependency{Name: "mod1", Source: "github.com/org/mod1", CurrentVer: "v1.0.0", FilePath: "main.tf"},
				"v2.0.0", "", terraform.DepKindModule,
			),
			terraform.NewUpgradeTask(
				entities.Dependency{Name: "mod2", Source: "github.com/org/mod2", CurrentVer: "v0.5.0", FilePath: "modules/infra.tf"},
				"v1.0.0", "", terraform.DepKindModule,
			),
		}

		// when
		result := terraform.GeneratePRDescription(tasks)

		// then
		assert.Contains(t, result, "## Summary")
		assert.Contains(t, result, "| Name | Type | Current Version | New Version | File |")
		assert.Contains(t, result, "| mod1 | module | v1.0.0 | v2.0.0 | main.tf |")
		assert.Contains(t, result, "| mod2 | module | v0.5.0 | v1.0.0 | modules/infra.tf |")
	})

	t.Run("should generate summary for more than 5 upgrades", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(entities.Dependency{Name: "mod1", Source: "github.com/org/mod1", CurrentVer: "v1.0.0", FilePath: "a.tf"}, "v2.0.0", "", terraform.DepKindModule),
			terraform.NewUpgradeTask(entities.Dependency{Name: "mod2", Source: "github.com/org/mod2", CurrentVer: "v1.0.0", FilePath: "b.tf"}, "v2.0.0", "", terraform.DepKindModule),
			terraform.NewUpgradeTask(entities.Dependency{Name: "mod3", Source: "github.com/org/mod3", CurrentVer: "v1.0.0", FilePath: "c.tf"}, "v2.0.0", "", terraform.DepKindModule),
			terraform.NewUpgradeTask(entities.Dependency{Name: "mod4", Source: "github.com/org/mod4", CurrentVer: "v1.0.0", FilePath: "d.tf"}, "v2.0.0", "", terraform.DepKindModule),
			terraform.NewUpgradeTask(entities.Dependency{Name: "img1", Source: "app1", CurrentVer: "1.0.0", FilePath: "e.hcl"}, "2.0.0", "", terraform.DepKindImage),
			terraform.NewUpgradeTask(entities.Dependency{Name: "img2", Source: "app2", CurrentVer: "1.0.0", FilePath: "f.hcl"}, "2.0.0", "", terraform.DepKindImage),
		}

		// when
		result := terraform.GeneratePRDescription(tasks)

		// then
		assert.Contains(t, result, "**6** Terraform dependencies")
		assert.Contains(t, result, "**4** module upgrades")
		assert.Contains(t, result, "**2** container image upgrades")
	})
}

func TestApplyUpgrades(t *testing.T) {
	t.Parallel()

	t.Run("should apply module version upgrade to file content", func(t *testing.T) {
		t.Parallel()

		// given
		content := `module "my_mod" {
  source = "git::https://github.com/org/my-module.git?ref=v1.0.0"
}`
		tasks := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(
				entities.Dependency{
					Name:       "my_mod",
					Source:     "git::https://github.com/org/my-module.git",
					CurrentVer: "v1.0.0",
					FilePath:   "main.tf",
					Line:       1,
				},
				"v2.0.0", content, terraform.DepKindModule,
			),
		}

		// when
		changes := terraform.ApplyUpgrades(tasks)

		// then
		require.Len(t, changes, 1)
		assert.Equal(t, "main.tf", changes[0].Path)
		assert.Contains(t, changes[0].Content, "?ref=v2.0.0")
		assert.NotContains(t, changes[0].Content, "?ref=v1.0.0")
	})

	t.Run("should apply image version upgrade", func(t *testing.T) {
		t.Parallel()

		// given
		content := `my_image = "app:1.0.0"`
		tasks := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(
				entities.Dependency{
					Name:       "my_image",
					Source:     "app",
					CurrentVer: "1.0.0",
					FilePath:   "vars.hcl",
					Line:       1,
				},
				"2.0.0", content, terraform.DepKindImage,
			),
		}

		// when
		changes := terraform.ApplyUpgrades(tasks)

		// then
		require.Len(t, changes, 1)
		assert.Equal(t, "vars.hcl", changes[0].Path)
		assert.Contains(t, changes[0].Content, `"app:2.0.0"`)
		assert.NotContains(t, changes[0].Content, `"app:1.0.0"`)
	})

	t.Run("should group changes by file path", func(t *testing.T) {
		t.Parallel()

		// given
		content := `module "mod_a" {
  source = "git::https://github.com/org/mod-a.git?ref=v1.0.0"
}

module "mod_b" {
  source = "git::https://github.com/org/mod-b.git?ref=v1.0.0"
}`
		tasks := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(
				entities.Dependency{
					Name:       "mod_a",
					Source:     "git::https://github.com/org/mod-a.git",
					CurrentVer: "v1.0.0",
					FilePath:   "main.tf",
					Line:       1,
				},
				"v2.0.0", content, terraform.DepKindModule,
			),
			terraform.NewUpgradeTask(
				entities.Dependency{
					Name:       "mod_b",
					Source:     "git::https://github.com/org/mod-b.git",
					CurrentVer: "v1.0.0",
					FilePath:   "main.tf",
					Line:       5,
				},
				"v3.0.0", content, terraform.DepKindModule,
			),
		}

		// when
		changes := terraform.ApplyUpgrades(tasks)

		// then
		require.Len(t, changes, 1)
		assert.Equal(t, "main.tf", changes[0].Path)
	})
}

func TestAppendChangelogEntry(t *testing.T) {
	t.Parallel()

	t.Run("should add changelog entries when CHANGELOG.md exists", func(t *testing.T) {
		t.Parallel()

		// given
		changelogContent := `# Changelog

## [Unreleased]

## [1.0.0] - 2026-01-01

### Added
- added initial release
`
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": changelogContent}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		upgrades := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(
				entities.Dependency{
					Name:       "my_mod",
					Source:     "github.com/org/my-module",
					CurrentVer: "v1.0.0",
				},
				"v2.0.0", "", terraform.DepKindModule,
			),
		}
		fileChanges := []entities.FileChange{}

		// when
		result := terraform.AppendChangelogEntry(t.Context(), provider, repo, upgrades, fileChanges)

		// then
		require.NotEmpty(t, result)
		var changelogChange *entities.FileChange
		for i := range result {
			if result[i].Path == "CHANGELOG.md" {
				changelogChange = &result[i]
				break
			}
		}
		require.NotNil(t, changelogChange)
		assert.Contains(t, changelogChange.Content, "Terraform module")
		assert.Contains(t, changelogChange.Content, "my-module")
		assert.Contains(t, changelogChange.Content, "v1.0.0")
		assert.Contains(t, changelogChange.Content, "v2.0.0")
	})

	t.Run("should skip when no CHANGELOG.md exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		upgrades := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(
				entities.Dependency{Name: "my_mod", Source: "github.com/org/my-module", CurrentVer: "v1.0.0"},
				"v2.0.0", "", terraform.DepKindModule,
			),
		}
		fileChanges := []entities.FileChange{{Path: "main.tf", Content: "updated", ChangeType: "edit"}}

		// when
		result := terraform.AppendChangelogEntry(t.Context(), provider, repo, upgrades, fileChanges)

		// then
		assert.Len(t, result, 1)
		assert.Equal(t, "main.tf", result[0].Path)
	})

	t.Run("should include image upgrade label", func(t *testing.T) {
		t.Parallel()

		// given
		changelogContent := `# Changelog

## [Unreleased]

## [1.0.0] - 2026-01-01

### Added
- added initial release
`
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": changelogContent}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		upgrades := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(
				entities.Dependency{
					Name:       "my_image",
					Source:     "app",
					CurrentVer: "1.0.0",
				},
				"2.0.0", "", terraform.DepKindImage,
			),
		}
		fileChanges := []entities.FileChange{}

		// when
		result := terraform.AppendChangelogEntry(t.Context(), provider, repo, upgrades, fileChanges)

		// then
		require.NotEmpty(t, result)
		var changelogChange *entities.FileChange
		for i := range result {
			if result[i].Path == "CHANGELOG.md" {
				changelogChange = &result[i]
				break
			}
		}
		require.NotNil(t, changelogChange)
		assert.Contains(t, changelogChange.Content, "container image")
	})
}

func TestLocalScanAllDependencies(t *testing.T) {
	t.Parallel()

	t.Run("should find dependencies in .tf files", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		content := `module "foo" {
  source = "git::https://github.com/org/mod.git?ref=v1.0.0"
}
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "main.tf"), []byte(content), 0o600))

		updater := &terraform.UpdaterRepository{}

		// when
		deps := terraform.LocalScanAllDependencies(updater, tmpDir)

		// then
		require.Len(t, deps, 1)
		assert.Equal(t, "foo", deps[0].Dependency.Name)
		assert.Equal(t, "git::https://github.com/org/mod.git", deps[0].Dependency.Source)
		assert.Equal(t, "v1.0.0", deps[0].Dependency.CurrentVer)
		assert.Equal(t, "main.tf", deps[0].Dependency.FilePath)
	})

	t.Run("should return empty for directory with no terraform files", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "readme.md"), []byte("# Hello"), 0o600))

		updater := &terraform.UpdaterRepository{}

		// when
		deps := terraform.LocalScanAllDependencies(updater, tmpDir)

		// then
		assert.Empty(t, deps)
	})

	t.Run("should find dependencies in .hcl files", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		content := `my_image = "app:0.7.0"
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "vars.hcl"), []byte(content), 0o600))

		updater := &terraform.UpdaterRepository{}

		// when
		deps := terraform.LocalScanAllDependencies(updater, tmpDir)

		// then
		require.Len(t, deps, 1)
		assert.Equal(t, "my_image", deps[0].Dependency.Name)
		assert.Equal(t, "app", deps[0].Dependency.Source)
		assert.Equal(t, "0.7.0", deps[0].Dependency.CurrentVer)
	})
}

func TestDetermineUpgrades(t *testing.T) {
	t.Parallel()

	t.Run("should return upgrade when newer tag exists", func(t *testing.T) {
		t.Parallel()

		// given
		changelog := `# Changelog

## [Unreleased]

## [2.0.0] - 2026-03-01

### Changed
- changed something
`
		depRepo := entities.Repository{Organization: "org", Name: "mod"}
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{depRepo}).
			WithTags([]string{"v2.0.0", "v1.0.0"}).
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": changelog}).
			BuildSpy()

		allDeps := []terraform.DepWithContent{
			terraform.NewDepWithContent(
				entities.Dependency{
					Name:       "my_mod",
					Source:     "git::https://github.com/org/mod",
					CurrentVer: "v1.0.0",
					FilePath:   "main.tf",
				},
				`module "my_mod" {
  source = "git::https://github.com/org/mod?ref=v1.0.0"
}`,
				terraform.DepKindModule,
			),
		}
		repo := entities.Repository{Organization: "org", Name: "repo"}
		updater := &terraform.UpdaterRepository{}

		// when
		upgrades := terraform.DetermineUpgrades(updater, t.Context(), provider, repo, allDeps)

		// then
		require.Len(t, upgrades, 1)
		assert.Equal(t, "v2.0.0", terraform.UpgradeTaskNewVersion(upgrades[0]))
	})

	t.Run("should return empty when already up to date", func(t *testing.T) {
		t.Parallel()

		// given
		changelog := `# Changelog

## [Unreleased]

## [2.0.0] - 2026-03-01
`
		depRepo := entities.Repository{Organization: "org", Name: "mod"}
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{depRepo}).
			WithTags([]string{"v2.0.0"}).
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": changelog}).
			BuildSpy()

		allDeps := []terraform.DepWithContent{
			terraform.NewDepWithContent(
				entities.Dependency{
					Name:       "my_mod",
					Source:     "git::https://github.com/org/mod",
					CurrentVer: "v2.0.0",
					FilePath:   "main.tf",
				},
				`module "my_mod" {
  source = "git::https://github.com/org/mod?ref=v2.0.0"
}`,
				terraform.DepKindModule,
			),
		}
		repo := entities.Repository{Organization: "org", Name: "repo"}
		updater := &terraform.UpdaterRepository{}

		// when
		upgrades := terraform.DetermineUpgrades(updater, t.Context(), provider, repo, allDeps)

		// then
		assert.Empty(t, upgrades)
	})

	t.Run("should return empty when no tags are available", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{}).
			BuildSpy()

		allDeps := []terraform.DepWithContent{
			terraform.NewDepWithContent(
				entities.Dependency{
					Name:       "my_mod",
					Source:     "git::https://github.com/org/unknown-mod",
					CurrentVer: "v1.0.0",
					FilePath:   "main.tf",
				},
				"content",
				terraform.DepKindModule,
			),
		}
		repo := entities.Repository{Organization: "org", Name: "repo"}
		updater := &terraform.UpdaterRepository{}

		// when
		upgrades := terraform.DetermineUpgrades(updater, t.Context(), provider, repo, allDeps)

		// then
		assert.Empty(t, upgrades)
	})
}

func TestResolveTagsForSource(t *testing.T) {
	t.Parallel()

	t.Run("should resolve tags from matching repository", func(t *testing.T) {
		t.Parallel()

		// given
		depRepo := entities.Repository{Organization: "org", Name: "my-module"}
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{depRepo}).
			WithTags([]string{"v2.0.0", "v1.0.0"}).
			BuildSpy()
		currentRepo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		tags, repo := terraform.ResolveTagsForSource(t.Context(), provider, currentRepo, "git::https://github.com/org/my-module")

		// then
		require.NotNil(t, repo)
		assert.Equal(t, "my-module", repo.Name)
		assert.Equal(t, []string{"v2.0.0", "v1.0.0"}, tags)
	})

	t.Run("should return nil when no matching repository found", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithRepositories([]entities.Repository{
				{Organization: "org", Name: "other-module"},
			}).
			BuildSpy()
		currentRepo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		tags, repo := terraform.ResolveTagsForSource(t.Context(), provider, currentRepo, "git::https://github.com/org/my-module")

		// then
		assert.Nil(t, repo)
		assert.Nil(t, tags)
	})

	t.Run("should return nil when discover fails", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithDiscoverErr(fmt.Errorf("API error")).
			BuildSpy()
		currentRepo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		tags, repo := terraform.ResolveTagsForSource(t.Context(), provider, currentRepo, "git::https://github.com/org/my-module")

		// then
		assert.Nil(t, repo)
		assert.Nil(t, tags)
	})
}

func TestCreateUpgradePR(t *testing.T) {
	t.Parallel()

	t.Run("should create branch and PR", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithPRExistsResult(false).
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}
		upgrades := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(
				entities.Dependency{
					Name:       "my_mod",
					Source:     "git::https://github.com/org/my-module.git",
					CurrentVer: "v1.0.0",
					FilePath:   "main.tf",
					Line:       1,
				},
				"v2.0.0",
				`module "my_mod" {
  source = "git::https://github.com/org/my-module.git?ref=v1.0.0"
}`,
				terraform.DepKindModule,
			),
		}
		updater := &terraform.UpdaterRepository{}

		// when
		prs, err := terraform.CreateUpgradePR(updater, t.Context(), provider, repo, opts, upgrades)

		// then
		require.NoError(t, err)
		require.Len(t, prs, 1)
		assert.Equal(t, 1, prs[0].ID)
		require.Len(t, provider.BranchInputs, 1)
		assert.Equal(t, "chore/upgrade-my-module.git-v2.0.0", provider.BranchInputs[0].BranchName)
		require.Len(t, provider.PRInputs, 1)
		assert.Contains(t, provider.PRInputs[0].Title, "my-module.git")
	})

	t.Run("should skip when PR already exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithPRExistsResult(true).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}
		upgrades := []terraform.UpgradeTask{
			terraform.NewUpgradeTask(
				entities.Dependency{
					Name:       "my_mod",
					Source:     "git::https://github.com/org/my-module.git",
					CurrentVer: "v1.0.0",
					FilePath:   "main.tf",
					Line:       1,
				},
				"v2.0.0",
				`module "my_mod" {
  source = "git::https://github.com/org/my-module.git?ref=v1.0.0"
}`,
				terraform.DepKindModule,
			),
		}
		updater := &terraform.UpdaterRepository{}

		// when
		prs, err := terraform.CreateUpgradePR(updater, t.Context(), provider, repo, opts, upgrades)

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
		assert.Empty(t, provider.BranchInputs)
		assert.Empty(t, provider.PRInputs)
	})
}

func TestStripVersionPrefix(t *testing.T) {
	t.Parallel()

	t.Run("should strip v prefix", func(t *testing.T) {
		t.Parallel()

		// given
		version := "v1.0.0"

		// when
		result := terraform.StripVersionPrefix(version)

		// then
		assert.Equal(t, "1.0.0", result)
	})

	t.Run("should strip V prefix", func(t *testing.T) {
		t.Parallel()

		// given
		version := "V1.0.0"

		// when
		result := terraform.StripVersionPrefix(version)

		// then
		assert.Equal(t, "1.0.0", result)
	})

	t.Run("should return version without prefix unchanged", func(t *testing.T) {
		t.Parallel()

		// given
		version := "1.0.0"

		// when
		result := terraform.StripVersionPrefix(version)

		// then
		assert.Equal(t, "1.0.0", result)
	})
}
