//go:build unit

package terraform_test

import (
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
