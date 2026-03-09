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
