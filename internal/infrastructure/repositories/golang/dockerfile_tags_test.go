//go:build unit

package golang_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
)

// realisticGolangTags mimics the shape of Docker Hub's `library/golang` tag
// list at the moment Go 1.25.7 is the latest release. Crucially, the
// `-alpine3.20` variant was dropped for the 1.25 line (only 1.24.5-alpine3.20
// remains), which is the exact situation that produced non-existent images.
func realisticGolangTags() []string {
	return []string{
		"latest",
		"1.25.7", "1.25.6", "1.24.5",
		"1.25", "1.24",
		"1.25.7-alpine", "1.25.6-alpine", "1.25-alpine",
		"1.25.7-alpine3.21", "1.25.6-alpine3.21",
		"1.25.7-alpine3.22", "1.25.6-alpine3.22",
		"1.24.5-alpine3.20", // old minor keeps the dropped variant
		"1.25.7-bookworm", "1.25.6-bookworm",
	}
}

func TestResolveClosestGolangTag(t *testing.T) {
	t.Parallel()

	t.Run("should return the exact tag when the target image is published", func(t *testing.T) {
		t.Parallel()

		// given
		tags := realisticGolangTags()

		// when
		got, ok := golang.ResolveClosestGolangTag(tags, "1.25.7", "")

		// then
		require.True(t, ok)
		assert.Equal(t, "1.25.7", got)
	})

	t.Run("should preserve the suffix when the exact suffixed image exists", func(t *testing.T) {
		t.Parallel()

		// given
		tags := realisticGolangTags()

		// when
		got, ok := golang.ResolveClosestGolangTag(tags, "1.25.7", "-alpine3.21")

		// then
		require.True(t, ok)
		assert.Equal(t, "1.25.7-alpine3.21", got)
	})

	t.Run("should fall back to the closest published patch when the target patch lags", func(t *testing.T) {
		t.Parallel()

		// given — Docker Hub has not published 1.25.7-alpine3.21 yet
		tags := []string{"1.25.6-alpine3.21", "1.25.5-alpine3.21", "1.24.9-alpine3.21"}

		// when
		got, ok := golang.ResolveClosestGolangTag(tags, "1.25.7", "-alpine3.21")

		// then
		require.True(t, ok)
		assert.Equal(t, "1.25.6-alpine3.21", got)
	})

	t.Run("should leave unchanged when the requested Alpine variant was dropped", func(t *testing.T) {
		t.Parallel()

		// given — no 1.25.*-alpine3.20 exists (the reported bug)
		tags := realisticGolangTags()

		// when
		got, ok := golang.ResolveClosestGolangTag(tags, "1.25.7", "-alpine3.20")

		// then
		assert.False(t, ok)
		assert.Empty(t, got)
	})

	t.Run("should not cross the minor boundary when falling back", func(t *testing.T) {
		t.Parallel()

		// given — only an older minor carries the suffix
		tags := []string{"1.24.5-alpine3.20", "1.24.4-alpine3.20"}

		// when
		got, ok := golang.ResolveClosestGolangTag(tags, "1.25.7", "-alpine3.20")

		// then
		assert.False(t, ok)
		assert.Empty(t, got)
	})

	t.Run("should never exceed the target version", func(t *testing.T) {
		t.Parallel()

		// given — a newer patch than the target exists but must not be chosen
		tags := []string{"1.25.9", "1.25.8", "1.25.5"}

		// when
		got, ok := golang.ResolveClosestGolangTag(tags, "1.25.7", "")

		// then
		require.True(t, ok)
		assert.Equal(t, "1.25.5", got)
	})

	t.Run("should return not-found when no published tag matches the suffix", func(t *testing.T) {
		t.Parallel()

		// given
		tags := []string{"1.25.7", "1.25.7-alpine"}

		// when
		got, ok := golang.ResolveClosestGolangTag(tags, "1.25.7", "-bookworm")

		// then
		assert.False(t, ok)
		assert.Empty(t, got)
	})
}

func TestParseGolangTag(t *testing.T) {
	t.Parallel()

	t.Run("should parse a three-part version", func(t *testing.T) {
		t.Parallel()

		// when
		version, suffix, ok := golang.ParseGolangTag("1.25.7")

		// then
		require.True(t, ok)
		assert.Equal(t, "1.25.7", version)
		assert.Empty(t, suffix)
	})

	t.Run("should split a suffixed version", func(t *testing.T) {
		t.Parallel()

		// when
		version, suffix, ok := golang.ParseGolangTag("1.24.5-alpine3.20")

		// then
		require.True(t, ok)
		assert.Equal(t, "1.24.5", version)
		assert.Equal(t, "-alpine3.20", suffix)
	})

	t.Run("should reject a non-version tag", func(t *testing.T) {
		t.Parallel()

		// when
		_, _, ok := golang.ParseGolangTag("latest")

		// then
		assert.False(t, ok)
	})
}

func TestRewriteGolangTags(t *testing.T) {
	t.Parallel()

	t.Run("should upgrade a plain golang tag to the published target", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM golang:1.25.6 AS builder\nRUN go build\n"

		// when
		got := golang.RewriteGolangTags(content, "1.25.7", realisticGolangTags(), "Dockerfile")

		// then
		assert.Contains(t, got, "FROM golang:1.25.7 AS builder")
	})

	t.Run("should preserve a suffix and fall back to the closest published patch", func(t *testing.T) {
		t.Parallel()

		// given — target 1.25.7-alpine3.21 not yet published, only 1.25.6 is
		content := "FROM golang:1.25.5-alpine3.21\n"
		tags := []string{"1.25.6-alpine3.21", "1.25.5-alpine3.21"}

		// when
		got := golang.RewriteGolangTags(content, "1.25.7", tags, "Dockerfile")

		// then
		assert.Equal(t, "FROM golang:1.25.6-alpine3.21\n", got)
	})

	t.Run("should leave the clause untouched when the target image does not exist", func(t *testing.T) {
		t.Parallel()

		// given — dropped Alpine variant: no 1.25.*-alpine3.20 exists
		content := "FROM golang:1.24.5-alpine3.20 AS builder\n"

		// when
		got := golang.RewriteGolangTags(content, "1.25.7", realisticGolangTags(), "Dockerfile")

		// then
		assert.Equal(t, content, got, "must not point FROM at a non-existent image")
	})

	t.Run("should ignore registry-qualified golang images", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM ghcr.io/acme/golang:1.25.6\n"

		// when
		got := golang.RewriteGolangTags(content, "1.25.7", realisticGolangTags(), "Dockerfile")

		// then
		assert.Equal(t, content, got)
	})

	t.Run("should skip digest-pinned images to avoid a tag/digest mismatch", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM golang:1.25.6@sha256:0123456789abcdef AS builder\n"

		// when
		got := golang.RewriteGolangTags(content, "1.25.7", realisticGolangTags(), "Dockerfile")

		// then
		assert.Equal(t, content, got)
	})

	t.Run("should not downgrade when the pin is already newer", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM golang:1.25.7\n"

		// when — target resolves to 1.25.6 only
		got := golang.RewriteGolangTags(content, "1.25.6", realisticGolangTags(), "Dockerfile")

		// then
		assert.Equal(t, content, got)
	})

	t.Run("should honour --platform and lowercase from", func(t *testing.T) {
		t.Parallel()

		// given
		content := "from --platform=$BUILDPLATFORM golang:1.25.6-alpine AS build\n"

		// when
		got := golang.RewriteGolangTags(content, "1.25.7", realisticGolangTags(), "Dockerfile")

		// then
		assert.Contains(t, got, "golang:1.25.7-alpine")
		assert.Contains(t, got, "from --platform=$BUILDPLATFORM")
	})
}

func TestUpdateDockerfileGolangTags(t *testing.T) {
	t.Parallel()

	t.Run("should rewrite golang tags across Dockerfiles and report a change", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeFile(t, repoDir, "Dockerfile", "FROM golang:1.25.6 AS builder\n")
		writeFile(t, repoDir, "api.Dockerfile", "FROM golang:1.25.6-alpine\n")
		lister := func(context.Context) ([]string, error) {
			return realisticGolangTags(), nil
		}

		// when
		changed, err := golang.UpdateDockerfileGolangTags(context.Background(), repoDir, "1.25.7", lister)

		// then
		require.NoError(t, err)
		assert.True(t, changed)
		assert.Contains(t, readFile(t, repoDir, "Dockerfile"), "golang:1.25.7")
		assert.Contains(t, readFile(t, repoDir, "api.Dockerfile"), "golang:1.25.7-alpine")
	})

	t.Run("should not fetch tags when no Dockerfile references golang", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeFile(t, repoDir, "Dockerfile", "FROM alpine:3.21\nRUN true\n")
		called := false
		lister := func(context.Context) ([]string, error) {
			called = true
			return nil, errors.New("should not be called")
		}

		// when
		changed, err := golang.UpdateDockerfileGolangTags(context.Background(), repoDir, "1.25.7", lister)

		// then
		require.NoError(t, err)
		assert.False(t, changed)
		assert.False(t, called, "tag list must be fetched lazily")
	})

	t.Run("should leave files untouched when the target image does not exist", func(t *testing.T) {
		t.Parallel()

		// given — dropped Alpine variant
		repoDir := t.TempDir()
		original := "FROM golang:1.24.5-alpine3.20\n"
		writeFile(t, repoDir, "Dockerfile", original)
		lister := func(context.Context) ([]string, error) {
			return realisticGolangTags(), nil
		}

		// when
		changed, err := golang.UpdateDockerfileGolangTags(context.Background(), repoDir, "1.25.7", lister)

		// then
		require.NoError(t, err)
		assert.False(t, changed)
		assert.Equal(t, original, readFile(t, repoDir, "Dockerfile"))
	})

	t.Run("should propagate a tag-fetch error", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		writeFile(t, repoDir, "Dockerfile", "FROM golang:1.25.6\n")
		lister := func(context.Context) ([]string, error) {
			return nil, errors.New("docker hub unavailable")
		}

		// when
		changed, err := golang.UpdateDockerfileGolangTags(context.Background(), repoDir, "1.25.7", lister)

		// then
		require.Error(t, err)
		assert.False(t, changed)
	})
}

func TestIsDockerfileName(t *testing.T) {
	t.Parallel()

	t.Run("should recognise Dockerfile name variants", func(t *testing.T) {
		t.Parallel()

		// given / when / then
		assert.True(t, golang.IsDockerfileName("Dockerfile"))
		assert.True(t, golang.IsDockerfileName("Dockerfile.dev"))
		assert.True(t, golang.IsDockerfileName("api.Dockerfile"))
		assert.False(t, golang.IsDockerfileName("Makefile"))
		assert.False(t, golang.IsDockerfileName("dockerfile.txt"))
	})
}

// --- helpers ---

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	return string(data)
}
