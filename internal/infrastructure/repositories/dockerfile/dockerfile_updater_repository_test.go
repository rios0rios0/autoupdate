//go:build unit

package dockerfile_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

	t.Run("should return false when provider returns error from ListFiles", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithListFileErr(fmt.Errorf("network timeout")).
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

func TestAppendChangelogEntry(t *testing.T) {
	t.Parallel()

	t.Run("should wrap image name and tags in backticks in changelog entry", func(t *testing.T) {
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
		upgrades := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTask("golang", "1.21.0", "1.22.0"),
		}

		// when
		result := dockerfile.AppendChangelogEntry(t.Context(), provider, repo, upgrades, nil)

		// then
		require.Len(t, result, 1)
		assert.Equal(t, "CHANGELOG.md", result[0].Path)
		assert.Contains(t, result[0].Content, "- changed the Docker base image `golang` from `1.21.0` to `1.22.0`")
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

func TestGenerateBranchName(t *testing.T) {
	t.Parallel()

	t.Run("should format single upgrade branch name", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTask("golang", "1.25-alpine", "1.26-alpine"),
		}

		// when
		result := dockerfile.GenerateBranchName(tasks)

		// then
		assert.Contains(t, result, "golang")
		assert.Contains(t, result, "1.26-alpine")
	})

	t.Run("should format batch upgrade branch name", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTask("golang", "1.25", "1.26"),
			dockerfile.NewUpgradeTask("python", "3.12", "3.13"),
		}

		// when
		result := dockerfile.GenerateBranchName(tasks)

		// then
		assert.Contains(t, result, "2")
	})
}

func TestGenerateCommitMessage(t *testing.T) {
	t.Parallel()

	t.Run("should format single upgrade commit message", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTask("golang", "1.25-alpine", "1.26-alpine"),
		}

		// when
		result := dockerfile.GenerateCommitMessage(tasks)

		// then
		assert.Contains(t, result, "golang")
		assert.Contains(t, result, "1.25-alpine")
		assert.Contains(t, result, "1.26-alpine")
	})

	t.Run("should format batch upgrade commit message", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTask("golang", "1.25", "1.26"),
			dockerfile.NewUpgradeTask("python", "3.12", "3.13"),
		}

		// when
		result := dockerfile.GenerateCommitMessage(tasks)

		// then
		assert.Contains(t, result, "2")
		assert.Contains(t, result, "Docker base images")
	})
}

func TestGeneratePRTitle(t *testing.T) {
	t.Parallel()

	t.Run("should format single upgrade PR title", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTask("golang", "1.25", "1.26"),
		}

		// when
		result := dockerfile.GeneratePRTitle(tasks)

		// then
		assert.Contains(t, result, "golang")
		assert.Contains(t, result, "1.26")
	})

	t.Run("should format batch upgrade PR title", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTask("golang", "1.25", "1.26"),
			dockerfile.NewUpgradeTask("python", "3.12", "3.13"),
		}

		// when
		result := dockerfile.GeneratePRTitle(tasks)

		// then
		assert.Contains(t, result, "2")
	})
}

func TestGeneratePRDescription(t *testing.T) {
	t.Parallel()

	t.Run("should generate table for few upgrades", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTask("golang", "1.25-alpine", "1.26-alpine"),
			dockerfile.NewUpgradeTask("python", "3.12-slim", "3.13-slim"),
		}

		// when
		result := dockerfile.GeneratePRDescription(tasks)

		// then
		assert.Contains(t, result, "| Image |")
		assert.Contains(t, result, "golang")
		assert.Contains(t, result, "python")
	})

	t.Run("should generate summary for many upgrades", func(t *testing.T) {
		t.Parallel()

		// given
		var tasks []dockerfile.UpgradeTask
		for range 6 {
			tasks = append(tasks, dockerfile.NewUpgradeTask("img", "1.0", "2.0"))
		}

		// when
		result := dockerfile.GeneratePRDescription(tasks)

		// then
		assert.Contains(t, result, "**6**")
		assert.NotContains(t, result, "| Image |")
	})
}

func TestApplyUpgrades(t *testing.T) {
	t.Parallel()

	t.Run("should replace image version in Dockerfile content", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM golang:1.21.0\nRUN go build\n"
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefFromContent(content, "Dockerfile", "golang", "golang", "1.21.0"),
		}
		tasks := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTaskFull("golang", "golang", "1.21.0", "1.22.0", "Dockerfile"),
		}

		// when
		changes := dockerfile.ApplyUpgrades(tasks, allRefs)

		// then
		require.Len(t, changes, 1)
		assert.Equal(t, "Dockerfile", changes[0].Path)
		assert.Contains(t, changes[0].Content, "golang:1.22.0")
		assert.NotContains(t, changes[0].Content, "golang:1.21.0")
		assert.Equal(t, "edit", changes[0].ChangeType)
	})

	t.Run("should replace multiple images in same Dockerfile", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM golang:1.21.0 AS builder\nRUN go build\nFROM alpine:3.18\nCOPY --from=builder /app /app\n"
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefFromContent(content, "Dockerfile", "golang", "golang", "1.21.0"),
			dockerfile.NewImageRefFromContent(content, "Dockerfile", "alpine", "alpine", "3.18"),
		}
		tasks := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTaskFull("golang", "golang", "1.21.0", "1.22.0", "Dockerfile"),
			dockerfile.NewUpgradeTaskFull("alpine", "alpine", "3.18", "3.19", "Dockerfile"),
		}

		// when
		changes := dockerfile.ApplyUpgrades(tasks, allRefs)

		// then
		require.Len(t, changes, 1)
		assert.Contains(t, changes[0].Content, "golang:1.22.0")
		assert.Contains(t, changes[0].Content, "alpine:3.19")
	})

	t.Run("should handle upgrades across multiple files", func(t *testing.T) {
		t.Parallel()

		// given
		content1 := "FROM golang:1.21.0\nRUN go build\n"
		content2 := "FROM python:3.11\nRUN pip install app\n"
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefFromContent(content1, "Dockerfile", "golang", "golang", "1.21.0"),
			dockerfile.NewImageRefFromContent(content2, "Dockerfile.dev", "python", "python", "3.11"),
		}
		tasks := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTaskFull("golang", "golang", "1.21.0", "1.22.0", "Dockerfile"),
			dockerfile.NewUpgradeTaskFull("python", "python", "3.11", "3.12", "Dockerfile.dev"),
		}

		// when
		changes := dockerfile.ApplyUpgrades(tasks, allRefs)

		// then
		require.Len(t, changes, 2)
	})

	t.Run("should return empty when no upgrades needed", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM golang:1.22.0\nRUN go build\n"
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefFromContent(content, "Dockerfile", "golang", "golang", "1.22.0"),
		}
		var tasks []dockerfile.UpgradeTask

		// when
		changes := dockerfile.ApplyUpgrades(tasks, allRefs)

		// then
		assert.Empty(t, changes)
	})
}

func TestCreateUpgradePR(t *testing.T) {
	t.Parallel()

	t.Run("should create branch and PR when no existing PR", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM golang:1.21.0\nRUN go build\n"
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithPRExistsResult(false).
			WithExistingFiles(map[string]bool{"CHANGELOG.md": false}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}
		upgrades := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTaskFull("golang", "golang", "1.21.0", "1.22.0", "Dockerfile"),
		}
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefFromContent(content, "Dockerfile", "golang", "golang", "1.21.0"),
		}

		// when
		prs, err := dockerfile.CreateUpgradePR(t.Context(), provider, repo, opts, upgrades, allRefs)

		// then
		require.NoError(t, err)
		require.Len(t, prs, 1)
		assert.Equal(t, 1, prs[0].ID)
		assert.Len(t, provider.BranchInputs, 1)
		assert.Len(t, provider.PRInputs, 1)
		assert.Contains(t, provider.PRInputs[0].Title, "golang")
	})

	t.Run("should skip when PR already exists", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM golang:1.21.0\nRUN go build\n"
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithPRExistsResult(true).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}
		upgrades := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTaskFull("golang", "golang", "1.21.0", "1.22.0", "Dockerfile"),
		}
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefFromContent(content, "Dockerfile", "golang", "golang", "1.21.0"),
		}

		// when
		prs, err := dockerfile.CreateUpgradePR(t.Context(), provider, repo, opts, upgrades, allRefs)

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
		assert.Empty(t, provider.BranchInputs)
		assert.Empty(t, provider.PRInputs)
	})

	t.Run("should return error when branch creation fails", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM golang:1.21.0\nRUN go build\n"
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithPRExistsResult(false).
			WithExistingFiles(map[string]bool{"CHANGELOG.md": false}).
			WithCreateBranchErr(assert.AnError).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}
		upgrades := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTaskFull("golang", "golang", "1.21.0", "1.22.0", "Dockerfile"),
		}
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefFromContent(content, "Dockerfile", "golang", "golang", "1.21.0"),
		}

		// when
		prs, err := dockerfile.CreateUpgradePR(t.Context(), provider, repo, opts, upgrades, allRefs)

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create branch")
		assert.Nil(t, prs)
	})

	t.Run("should return error when PR creation fails", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM golang:1.21.0\nRUN go build\n"
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithPRExistsResult(false).
			WithExistingFiles(map[string]bool{"CHANGELOG.md": false}).
			WithCreatePRErr(assert.AnError).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}
		upgrades := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTaskFull("golang", "golang", "1.21.0", "1.22.0", "Dockerfile"),
		}
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefFromContent(content, "Dockerfile", "golang", "golang", "1.21.0"),
		}

		// when
		prs, err := dockerfile.CreateUpgradePR(t.Context(), provider, repo, opts, upgrades, allRefs)

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create PR")
		assert.Nil(t, prs)
	})

	t.Run("should use target branch from options when provided", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM golang:1.21.0\nRUN go build\n"
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithPRExistsResult(false).
			WithExistingFiles(map[string]bool{"CHANGELOG.md": false}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{TargetBranch: "develop"}
		upgrades := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTaskFull("golang", "golang", "1.21.0", "1.22.0", "Dockerfile"),
		}
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefFromContent(content, "Dockerfile", "golang", "golang", "1.21.0"),
		}

		// when
		prs, err := dockerfile.CreateUpgradePR(t.Context(), provider, repo, opts, upgrades, allRefs)

		// then
		require.NoError(t, err)
		require.Len(t, prs, 1)
		assert.Equal(t, "refs/heads/develop", provider.BranchInputs[0].BaseBranch)
		assert.Equal(t, "refs/heads/develop", provider.PRInputs[0].TargetBranch)
	})

	t.Run("should include changelog entry when CHANGELOG.md exists", func(t *testing.T) {
		t.Parallel()

		// given
		dockerfileContent := "FROM golang:1.21.0\nRUN go build\n"
		changelogContent := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n"
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithPRExistsResult(false).
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": changelogContent}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}
		upgrades := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTaskFull("golang", "golang", "1.21.0", "1.22.0", "Dockerfile"),
		}
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefFromContent(dockerfileContent, "Dockerfile", "golang", "golang", "1.21.0"),
		}

		// when
		prs, err := dockerfile.CreateUpgradePR(t.Context(), provider, repo, opts, upgrades, allRefs)

		// then
		require.NoError(t, err)
		require.Len(t, prs, 1)
		// Branch input should include both the Dockerfile change and the CHANGELOG.md change
		require.NotEmpty(t, provider.BranchInputs)
		branchChanges := provider.BranchInputs[0].Changes
		changelogFound := false
		for _, c := range branchChanges {
			if c.Path == "CHANGELOG.md" {
				changelogFound = true
				assert.Contains(t, c.Content, "changed the Docker base image `golang` from `1.21.0` to `1.22.0`")
			}
		}
		assert.True(t, changelogFound, "expected CHANGELOG.md in branch changes")
	})
}

func TestAppendChangelogEntryWithUpgrades(t *testing.T) {
	t.Parallel()

	t.Run("should return unchanged when CHANGELOG.md does not exist", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": false}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		upgrades := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTask("golang", "1.21.0", "1.22.0"),
		}
		existingChanges := []entities.FileChange{
			{Path: "Dockerfile", Content: "FROM golang:1.22.0\n", ChangeType: "edit"},
		}

		// when
		result := dockerfile.AppendChangelogEntry(t.Context(), provider, repo, upgrades, existingChanges)

		// then
		assert.Equal(t, existingChanges, result)
	})

	t.Run("should return unchanged when reading CHANGELOG.md fails", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContentErr(assert.AnError).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		upgrades := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTask("golang", "1.21.0", "1.22.0"),
		}
		existingChanges := []entities.FileChange{
			{Path: "Dockerfile", Content: "FROM golang:1.22.0\n", ChangeType: "edit"},
		}

		// when
		result := dockerfile.AppendChangelogEntry(t.Context(), provider, repo, upgrades, existingChanges)

		// then
		assert.Equal(t, existingChanges, result)
	})

	t.Run("should include image upgrade entries for multiple images", func(t *testing.T) {
		t.Parallel()

		// given
		changelog := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n"
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"CHANGELOG.md": true}).
			WithFileContents(map[string]string{"CHANGELOG.md": changelog}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}
		upgrades := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTask("golang", "1.21.0", "1.22.0"),
			dockerfile.NewUpgradeTask("alpine", "3.18", "3.19"),
		}

		// when
		result := dockerfile.AppendChangelogEntry(t.Context(), provider, repo, upgrades, nil)

		// then
		require.Len(t, result, 1)
		assert.Equal(t, "CHANGELOG.md", result[0].Path)
		assert.Contains(t, result[0].Content, "changed the Docker base image `golang` from `1.21.0` to `1.22.0`")
		assert.Contains(t, result[0].Content, "changed the Docker base image `alpine` from `3.18` to `3.19`")
	})
}

// TestDetermineUpgrades tests are sequential because they override a package-level function variable.
func TestDetermineUpgrades(t *testing.T) {
	t.Run("should return upgrade when newer tag available", func(t *testing.T) {
		// given
		cleanup := dockerfile.SetFetchTagsFunc(
			func(_ context.Context, ref *dockerfile.ParsedImageRef) ([]string, error) {
				if ref.Image == "golang" {
					return []string{"1.21.0", "1.21.5", "1.22.0"}, nil
				}
				return nil, fmt.Errorf("unexpected image: %s", ref.Image)
			},
		)
		defer cleanup()

		content := "FROM golang:1.21.0\n"
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefFromContent(content, "Dockerfile", "golang", "golang", "1.21.0"),
		}

		// when
		upgrades := dockerfile.DetermineUpgrades(t.Context(), allRefs)

		// then
		require.Len(t, upgrades, 1)
	})

	t.Run("should return empty when already at latest", func(t *testing.T) {
		// given
		cleanup := dockerfile.SetFetchTagsFunc(
			func(_ context.Context, _ *dockerfile.ParsedImageRef) ([]string, error) {
				return []string{"1.22.0"}, nil
			},
		)
		defer cleanup()

		content := "FROM golang:1.22.0\n"
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefFromContent(content, "Dockerfile", "golang", "golang", "1.22.0"),
		}

		// when
		upgrades := dockerfile.DetermineUpgrades(t.Context(), allRefs)

		// then
		assert.Empty(t, upgrades)
	})

	t.Run("should skip image when fetch tags fails", func(t *testing.T) {
		// given
		cleanup := dockerfile.SetFetchTagsFunc(
			func(_ context.Context, _ *dockerfile.ParsedImageRef) ([]string, error) {
				return nil, fmt.Errorf("network error")
			},
		)
		defer cleanup()

		content := "FROM golang:1.21.0\n"
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefFromContent(content, "Dockerfile", "golang", "golang", "1.21.0"),
		}

		// when
		upgrades := dockerfile.DetermineUpgrades(t.Context(), allRefs)

		// then
		assert.Empty(t, upgrades)
	})

	t.Run("should use cache for duplicate image references", func(t *testing.T) {
		// given
		callCount := 0
		cleanup := dockerfile.SetFetchTagsFunc(
			func(_ context.Context, _ *dockerfile.ParsedImageRef) ([]string, error) {
				callCount++
				return []string{"1.21.0", "1.21.5"}, nil
			},
		)
		defer cleanup()

		content := "FROM golang:1.21.0\n"
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefFromContent(content, "Dockerfile", "golang", "golang", "1.21.0"),
			dockerfile.NewImageRefFromContent(content, "Dockerfile.dev", "golang", "golang", "1.21.0"),
		}

		// when
		upgrades := dockerfile.DetermineUpgrades(t.Context(), allRefs)

		// then
		require.Len(t, upgrades, 2)
		assert.Equal(t, 1, callCount, "fetchTags should be called only once due to caching")
	})
}

// TestCreateUpdatePRsWithDryRun tests are sequential because they override a package-level function variable.
func TestCreateUpdatePRsWithDryRun(t *testing.T) {
	t.Run("should return empty when no Docker Hub images found", func(t *testing.T) {
		// given
		content := "FROM ghcr.io/org/myapp:1.2.3\n"
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

	t.Run("should log upgrades and return empty in dry run mode", func(t *testing.T) {
		// given
		cleanup := dockerfile.SetFetchTagsFunc(
			func(_ context.Context, _ *dockerfile.ParsedImageRef) ([]string, error) {
				return []string{"1.21.0", "1.21.5"}, nil
			},
		)
		defer cleanup()

		content := "FROM golang:1.21.0\nRUN go build\n"
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{
				{Path: "Dockerfile", IsDir: false},
			}).
			WithFileContents(map[string]string{
				"Dockerfile": content,
			}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{DryRun: true}

		// when
		prs, err := dockerfile.NewUpdaterRepository().CreateUpdatePRs(t.Context(), provider, repo, opts)

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
		assert.Empty(t, provider.BranchInputs)
		assert.Empty(t, provider.PRInputs)
	})

	t.Run("should create PR when upgrades found and not dry run", func(t *testing.T) {
		// given
		cleanup := dockerfile.SetFetchTagsFunc(
			func(_ context.Context, _ *dockerfile.ParsedImageRef) ([]string, error) {
				return []string{"1.21.0", "1.21.5"}, nil
			},
		)
		defer cleanup()

		content := "FROM golang:1.21.0\nRUN go build\n"
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{
				{Path: "Dockerfile", IsDir: false},
			}).
			WithFileContents(map[string]string{
				"Dockerfile": content,
			}).
			WithPRExistsResult(false).
			WithExistingFiles(map[string]bool{"CHANGELOG.md": false}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}

		// when
		prs, err := dockerfile.NewUpdaterRepository().CreateUpdatePRs(t.Context(), provider, repo, opts)

		// then
		require.NoError(t, err)
		require.Len(t, prs, 1)
		assert.NotEmpty(t, provider.BranchInputs)
		assert.NotEmpty(t, provider.PRInputs)
	})

	t.Run("should skip when PR already exists for upgrade branch", func(t *testing.T) {
		// given
		cleanup := dockerfile.SetFetchTagsFunc(
			func(_ context.Context, _ *dockerfile.ParsedImageRef) ([]string, error) {
				return []string{"1.21.0", "1.21.5"}, nil
			},
		)
		defer cleanup()

		content := "FROM golang:1.21.0\nRUN go build\n"
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFiles([]entities.File{
				{Path: "Dockerfile", IsDir: false},
			}).
			WithFileContents(map[string]string{
				"Dockerfile": content,
			}).
			WithPRExistsResult(true).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo", DefaultBranch: "refs/heads/main"}
		opts := entities.UpdateOptions{}

		// when
		prs, err := dockerfile.NewUpdaterRepository().CreateUpdatePRs(t.Context(), provider, repo, opts)

		// then
		require.NoError(t, err)
		assert.Empty(t, prs)
		assert.Empty(t, provider.BranchInputs)
	})

	t.Run("should return empty when all images are up to date", func(t *testing.T) {
		// given
		cleanup := dockerfile.SetFetchTagsFunc(
			func(_ context.Context, _ *dockerfile.ParsedImageRef) ([]string, error) {
				return []string{"1.22.0"}, nil
			},
		)
		defer cleanup()

		content := "FROM golang:1.22.0\nRUN go build\n"
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

func TestLocalScanAllDockerfiles(t *testing.T) {
	t.Parallel()

	t.Run("should find Dockerfiles in directory", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		content := "FROM golang:1.25-alpine\nRUN go build\n"
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(content), 0o600))

		// when
		refs := dockerfile.LocalScanAllDockerfiles(tmpDir)

		// then
		require.Len(t, refs, 1)
		assert.Equal(t, "golang", refs[0].Name)
		assert.Equal(t, "1.25-alpine", refs[0].CurrentVer)
		assert.Equal(t, "Dockerfile", refs[0].FilePath)
		assert.Equal(t, 1, refs[0].Line)
	})

	t.Run("should skip hidden directories", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		hiddenDir := filepath.Join(tmpDir, ".hidden")
		require.NoError(t, os.MkdirAll(hiddenDir, 0o750))
		content := "FROM golang:1.25-alpine\n"
		require.NoError(t, os.WriteFile(filepath.Join(hiddenDir, "Dockerfile"), []byte(content), 0o600))

		// when
		refs := dockerfile.LocalScanAllDockerfiles(tmpDir)

		// then
		assert.Empty(t, refs)
	})

	t.Run("should find Dockerfile variants", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		content := "FROM python:3.12-slim\nRUN pip install app\n"
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "Dockerfile.dev"), []byte(content), 0o600))

		// when
		refs := dockerfile.LocalScanAllDockerfiles(tmpDir)

		// then
		require.Len(t, refs, 1)
		assert.Equal(t, "python", refs[0].Name)
		assert.Equal(t, "3.12-slim", refs[0].CurrentVer)
	})

	t.Run("should find Dockerfiles in subdirectories", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		subDir := filepath.Join(tmpDir, "build")
		require.NoError(t, os.MkdirAll(subDir, 0o750))
		content := "FROM alpine:3.19\nRUN apk add curl\n"
		require.NoError(t, os.WriteFile(filepath.Join(subDir, "Dockerfile"), []byte(content), 0o600))

		// when
		refs := dockerfile.LocalScanAllDockerfiles(tmpDir)

		// then
		require.Len(t, refs, 1)
		assert.Equal(t, "alpine", refs[0].Name)
		assert.Equal(t, "3.19", refs[0].CurrentVer)
		assert.Equal(t, "build/Dockerfile", refs[0].FilePath)
	})

	t.Run("should return empty for directory with no Dockerfiles", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0o600))

		// when
		refs := dockerfile.LocalScanAllDockerfiles(tmpDir)

		// then
		assert.Empty(t, refs)
	})

	t.Run("should parse multi-stage Dockerfiles", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		content := "FROM golang:1.25.7 AS builder\nRUN go build\nFROM alpine:3.19\nCOPY --from=builder /app /app\n"
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(content), 0o600))

		// when
		refs := dockerfile.LocalScanAllDockerfiles(tmpDir)

		// then
		require.Len(t, refs, 2)
		assert.Equal(t, "golang", refs[0].Name)
		assert.Equal(t, "1.25.7", refs[0].CurrentVer)
		assert.Equal(t, "alpine", refs[1].Name)
		assert.Equal(t, "3.19", refs[1].CurrentVer)
	})
}
