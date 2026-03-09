//go:build unit

package dockerfile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/dockerfile"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestDockerfileUpdaterName(t *testing.T) {
	t.Parallel()

	t.Run("should return dockerfile as updater name", func(t *testing.T) {
		t.Parallel()

		// given
		updater := dockerfile.NewUpdaterRepository()

		// when
		name := updater.Name()

		// then
		assert.Equal(t, "dockerfile", name)
	})
}

func TestDockerfileDetect(t *testing.T) {
	t.Parallel()

	t.Run("should return true when Dockerfile exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{
				{Path: "Dockerfile", IsDir: false},
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := dockerfile.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.True(t, detected)
	})

	t.Run("should return true when Dockerfile variant exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{
				{Path: "Dockerfile.dev", IsDir: false},
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := dockerfile.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.True(t, detected)
	})

	t.Run("should return false when no Dockerfile exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := dockerfile.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.False(t, detected)
	})
}

func TestDockerfileCreateUpdatePRs(t *testing.T) {
	t.Parallel()

	t.Run("should return empty when no Dockerfiles found", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}

		// when
		prs, err := dockerfile.NewUpdaterRepository().CreateUpdatePRs(t.Context(), provider, repo, opts)

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
	})

	t.Run("should skip non-version tags like latest", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM alpine:latest\n"
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{
				{Path: "Dockerfile", IsDir: false},
			}).
			WithFileContents(map[string]string{
				"Dockerfile": content,
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}

		// when
		prs, err := dockerfile.NewUpdaterRepository().CreateUpdatePRs(t.Context(), provider, repo, opts)

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
	})

	t.Run("should skip scratch images", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM scratch\nCOPY binary /app\n"
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{
				{Path: "Dockerfile", IsDir: false},
			}).
			WithFileContents(map[string]string{
				"Dockerfile": content,
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}

		// when
		prs, err := dockerfile.NewUpdaterRepository().CreateUpdatePRs(t.Context(), provider, repo, opts)

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
	})
}

func TestScanDockerfile(t *testing.T) {
	t.Parallel()

	t.Run("should parse FROM clause with version tag", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM golang:1.25.7\nRUN go build\n"

		// when
		refs := dockerfile.ScanDockerfile(content, "Dockerfile")

		// then
		require.Len(t, refs, 1)
		assert.Equal(t, "golang", refs[0].Name)
		assert.Equal(t, "1.25.7", refs[0].CurrentVer)
		assert.Equal(t, "Dockerfile", refs[0].FilePath)
		assert.Equal(t, 1, refs[0].Line)
	})

	t.Run("should parse FROM clause with platform flag", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM --platform=linux/amd64 golang:1.25.7 AS builder\nRUN go build\n"

		// when
		refs := dockerfile.ScanDockerfile(content, "Dockerfile")

		// then
		require.Len(t, refs, 1)
		assert.Equal(t, "golang", refs[0].Name)
		assert.Equal(t, "1.25.7", refs[0].CurrentVer)
	})

	t.Run("should parse FROM clause with namespace", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM bitnami/redis:7.2.4\n"

		// when
		refs := dockerfile.ScanDockerfile(content, "Dockerfile")

		// then
		require.Len(t, refs, 1)
		assert.Equal(t, "bitnami/redis", refs[0].Name)
		assert.Equal(t, "7.2.4", refs[0].CurrentVer)
	})

	t.Run("should skip build arg references", func(t *testing.T) {
		t.Parallel()

		// given
		content := "ARG BASE=golang:1.25\nFROM ${BASE}\n"

		// when
		refs := dockerfile.ScanDockerfile(content, "Dockerfile")

		// then
		assert.Empty(t, refs)
	})

	t.Run("should skip latest tag", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM alpine:latest\n"

		// when
		refs := dockerfile.ScanDockerfile(content, "Dockerfile")

		// then
		assert.Empty(t, refs)
	})

	t.Run("should parse multi-stage Dockerfiles", func(t *testing.T) {
		t.Parallel()

		// given
		content := `FROM golang:1.25.7 AS builder
RUN go build
FROM alpine:3.19
COPY --from=builder /app /app
`

		// when
		refs := dockerfile.ScanDockerfile(content, "Dockerfile")

		// then
		require.Len(t, refs, 2)
		assert.Equal(t, "golang", refs[0].Name)
		assert.Equal(t, "1.25.7", refs[0].CurrentVer)
		assert.Equal(t, "alpine", refs[1].Name)
		assert.Equal(t, "3.19", refs[1].CurrentVer)
	})

	t.Run("should parse tag with suffix", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM python:3.12-slim-bullseye\n"

		// when
		refs := dockerfile.ScanDockerfile(content, "Dockerfile")

		// then
		require.Len(t, refs, 1)
		assert.Equal(t, "python", refs[0].Name)
		assert.Equal(t, "3.12-slim-bullseye", refs[0].CurrentVer)
	})

	t.Run("should skip non-Docker Hub registry images", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM ghcr.io/org/myapp:1.2.3\n"

		// when
		refs := dockerfile.ScanDockerfile(content, "Dockerfile")

		// then
		assert.Empty(t, refs)
	})
}

func TestIsDockerfilePath(t *testing.T) {
	t.Parallel()

	t.Run("should match Dockerfile", func(t *testing.T) {
		t.Parallel()

		assert.True(t, dockerfile.IsDockerfilePath("Dockerfile"))
	})

	t.Run("should match Dockerfile.dev", func(t *testing.T) {
		t.Parallel()

		assert.True(t, dockerfile.IsDockerfilePath("Dockerfile.dev"))
	})

	t.Run("should match app.Dockerfile", func(t *testing.T) {
		t.Parallel()

		assert.True(t, dockerfile.IsDockerfilePath("app.Dockerfile"))
	})

	t.Run("should match nested path", func(t *testing.T) {
		t.Parallel()

		assert.True(t, dockerfile.IsDockerfilePath("build/Dockerfile"))
	})

	t.Run("should not match random file", func(t *testing.T) {
		t.Parallel()

		assert.False(t, dockerfile.IsDockerfilePath("main.go"))
	})
}

func TestIsDockerHubImage(t *testing.T) {
	t.Parallel()

	t.Run("should accept official Docker Hub images", func(t *testing.T) {
		t.Parallel()

		assert.True(t, dockerfile.IsDockerHubImage("golang"))
		assert.True(t, dockerfile.IsDockerHubImage("python"))
		assert.True(t, dockerfile.IsDockerHubImage("alpine"))
	})

	t.Run("should accept namespaced Docker Hub images", func(t *testing.T) {
		t.Parallel()

		assert.True(t, dockerfile.IsDockerHubImage("bitnami/redis"))
		assert.True(t, dockerfile.IsDockerHubImage("library/golang"))
	})

	t.Run("should reject ghcr.io images", func(t *testing.T) {
		t.Parallel()

		assert.False(t, dockerfile.IsDockerHubImage("ghcr.io/org/image"))
	})

	t.Run("should reject quay.io images", func(t *testing.T) {
		t.Parallel()

		assert.False(t, dockerfile.IsDockerHubImage("quay.io/prometheus/node-exporter"))
	})

	t.Run("should reject registry with port", func(t *testing.T) {
		t.Parallel()

		assert.False(t, dockerfile.IsDockerHubImage("registry.example.com:5000/myimage"))
	})
}
