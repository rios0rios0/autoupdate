package dockerfile_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/dockerfile"
)

// Digest fixtures. Only the shape matters: nothing under test resolves one, it
// is carried from the registry listing to the FROM clause.
const (
	oldDigest = "sha256:" +
		"1111111111111111111111111111111111111111111111111111111111111111"
	newDigest = "sha256:" +
		"2222222222222222222222222222222222222222222222222222222222222222"
)

func TestScanDockerfileDigest(t *testing.T) {
	t.Parallel()

	t.Run("should capture the digest pinned beside the tag", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM python:3.13-slim@" + oldDigest + "\n"

		// when
		results := dockerfile.ScanDockerfile(content, "Dockerfile")

		// then
		require.Len(t, results, 1)
		assert.Equal(t, "3.13-slim", results[0].CurrentVer)
		assert.Equal(t, oldDigest, results[0].Digest)
	})

	t.Run("should capture the digest on a named build stage", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM python:3.13-slim@" + oldDigest + " AS builder\n"

		// when
		results := dockerfile.ScanDockerfile(content, "Dockerfile")

		// then
		require.Len(t, results, 1)
		assert.Equal(t, oldDigest, results[0].Digest)
	})

	t.Run("should report no digest when the clause pins the tag alone", func(t *testing.T) {
		t.Parallel()

		// given
		content := "FROM python:3.13-slim\n"

		// when
		results := dockerfile.ScanDockerfile(content, "Dockerfile")

		// then
		require.Len(t, results, 1)
		assert.Empty(t, results[0].Digest)
	})
}

// TestDetermineUpgradesWithDigest is sequential because it overrides a
// package-level function variable.
func TestDetermineUpgradesWithDigest(t *testing.T) {
	t.Run("should resolve the digest of the tag it upgrades to", func(t *testing.T) {
		// given
		cleanup := dockerfile.SetFetchRegistryTagsFunc(registryReporting(map[string]string{
			"3.13-slim": oldDigest,
			"3.14-slim": newDigest,
		}))
		defer cleanup()

		content := "FROM python:3.13-slim@" + oldDigest + "\n"
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefWithDigest(content, "Dockerfile", "python", "3.13-slim", oldDigest),
		}

		// when
		upgrades := dockerfile.DetermineUpgrades(t.Context(), allRefs)

		// then
		require.Len(t, upgrades, 1)
		assert.Equal(t, newDigest, dockerfile.UpgradeTaskDigest(upgrades[0]))
	})

	t.Run("should skip the upgrade when the registry reports no digest for the new tag",
		func(t *testing.T) {
			// given: rewriting the tag alone would leave the previous manifest
			// pinned beside a version it is not
			cleanup := dockerfile.SetFetchTagsFunc(
				func(_ context.Context, _ *dockerfile.ParsedImageRef) ([]string, error) {
					return []string{"3.13-slim", "3.14-slim"}, nil
				},
			)
			defer cleanup()

			content := "FROM python:3.13-slim@" + oldDigest + "\n"
			allRefs := []dockerfile.ImageRef{
				dockerfile.NewImageRefWithDigest(content, "Dockerfile", "python", "3.13-slim", oldDigest),
			}

			// when
			upgrades := dockerfile.DetermineUpgrades(t.Context(), allRefs)

			// then
			assert.Empty(t, upgrades)
		})

	t.Run("should still upgrade a clause that pins no digest", func(t *testing.T) {
		// given
		cleanup := dockerfile.SetFetchTagsFunc(
			func(_ context.Context, _ *dockerfile.ParsedImageRef) ([]string, error) {
				return []string{"3.13-slim", "3.14-slim"}, nil
			},
		)
		defer cleanup()

		content := "FROM python:3.13-slim\n"
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefFromContent(content, "Dockerfile", "python", "python", "3.13-slim"),
		}

		// when
		upgrades := dockerfile.DetermineUpgrades(t.Context(), allRefs)

		// then
		require.Len(t, upgrades, 1)
		assert.Empty(t, dockerfile.UpgradeTaskDigest(upgrades[0]))
	})
}

// TestApplyUpgradesWithDigest is sequential because it overrides a
// package-level function variable.
func TestApplyUpgradesWithDigest(t *testing.T) {
	t.Run("should rewrite the tag and the digest together", func(t *testing.T) {
		// given
		cleanup := dockerfile.SetFetchRegistryTagsFunc(registryReporting(map[string]string{
			"3.13-slim": oldDigest,
			"3.14-slim": newDigest,
		}))
		defer cleanup()

		content := "FROM python:3.13-slim@" + oldDigest + "\nRUN true\n"
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefWithDigest(content, "Dockerfile", "python", "3.13-slim", oldDigest),
		}
		upgrades := dockerfile.DetermineUpgrades(t.Context(), allRefs)

		// when
		changes := dockerfile.ApplyUpgrades(upgrades, allRefs)

		// then
		require.Len(t, changes, 1)
		assert.Equal(t, "FROM python:3.14-slim@"+newDigest+"\nRUN true\n", changes[0].Content)
	})

	t.Run("should rewrite a digest-pinned and a plain clause in the same file", func(t *testing.T) {
		// given: the same file pins python twice, once by digest and once by tag
		cleanup := dockerfile.SetFetchRegistryTagsFunc(registryReporting(map[string]string{
			"3.13-slim": oldDigest,
			"3.14-slim": newDigest,
		}))
		defer cleanup()

		content := "FROM python:3.13-slim@" + oldDigest + " AS builder\n" +
			"FROM python:3.13-slim AS runtime\n"
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefWithDigest(content, "Dockerfile", "python", "3.13-slim", oldDigest),
			dockerfile.NewImageRefFromContent(content, "Dockerfile", "python", "python", "3.13-slim"),
		}
		upgrades := dockerfile.DetermineUpgrades(t.Context(), allRefs)

		// when
		changes := dockerfile.ApplyUpgrades(upgrades, allRefs)

		// then
		require.Len(t, changes, 1)
		assert.Equal(t,
			"FROM python:3.14-slim@"+newDigest+" AS builder\n"+
				"FROM python:3.14-slim AS runtime\n",
			changes[0].Content)
	})

	t.Run("should not rewrite a digest-pinned clause it skipped", func(t *testing.T) {
		// given: the registry publishes the new tag but reports no digest, so the
		// digest-pinned clause is skipped -- and the plain clause below it must
		// not be rewritten through it, since "python:3.13-slim" also occurs
		// inside "python:3.13-slim@sha256:..."
		cleanup := dockerfile.SetFetchTagsFunc(
			func(_ context.Context, _ *dockerfile.ParsedImageRef) ([]string, error) {
				return []string{"3.13-slim", "3.14-slim"}, nil
			},
		)
		defer cleanup()

		content := "FROM python:3.13-slim@" + oldDigest + " AS builder\n" +
			"FROM python:3.13-slim AS runtime\n"
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefWithDigest(content, "Dockerfile", "python", "3.13-slim", oldDigest),
			dockerfile.NewImageRefFromContent(content, "Dockerfile", "python", "python", "3.13-slim"),
		}
		upgrades := dockerfile.DetermineUpgrades(t.Context(), allRefs)

		// when
		changes := dockerfile.ApplyUpgrades(upgrades, allRefs)

		// then
		require.Len(t, changes, 1)
		assert.Equal(t,
			"FROM python:3.13-slim@"+oldDigest+" AS builder\n"+
				"FROM python:3.14-slim AS runtime\n",
			changes[0].Content)
	})

	t.Run("should not rewrite a tag that merely starts with the pinned one", func(t *testing.T) {
		// given: "python:3.1" also occurs inside "python:3.13"
		cleanup := dockerfile.SetFetchTagsFunc(
			func(_ context.Context, _ *dockerfile.ParsedImageRef) ([]string, error) {
				return []string{"3.1", "3.9"}, nil
			},
		)
		defer cleanup()

		content := "FROM python:3.13 AS runtime\nFROM python:3.1 AS tools\n"
		allRefs := []dockerfile.ImageRef{
			dockerfile.NewImageRefFromContent(content, "Dockerfile", "python", "python", "3.1"),
		}
		upgrades := dockerfile.DetermineUpgrades(t.Context(), allRefs)

		// when
		changes := dockerfile.ApplyUpgrades(upgrades, allRefs)

		// then
		require.Len(t, changes, 1)
		assert.Equal(t, "FROM python:3.13 AS runtime\nFROM python:3.9 AS tools\n",
			changes[0].Content)
	})
}

func TestGeneratePRDescriptionWithDigest(t *testing.T) {
	t.Parallel()

	t.Run("should not mention digests when no clause pins one", func(t *testing.T) {
		t.Parallel()

		// given
		tasks := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTaskFull("python", "python", "3.13-slim", "3.14-slim", "Dockerfile"),
		}

		// when
		description := dockerfile.GeneratePRDescription(tasks)

		// then
		assert.NotContains(t, description, "digest")
	})

	t.Run("should explain the changed sha when a clause pins a digest", func(t *testing.T) {
		t.Parallel()

		// given: a reviewer seeing a rewritten sha256 in the diff should not have
		// to work out where it came from
		tasks := []dockerfile.UpgradeTask{
			dockerfile.NewUpgradeTaskWithDigest(
				"python", "3.13-slim", "3.14-slim", "Dockerfile", newDigest),
		}

		// when
		description := dockerfile.GeneratePRDescription(tasks)

		// then
		assert.Contains(t, description, "digest")
	})
}

// registryReporting returns a tag lister answering with the given tag/digest
// pairs, standing in for the Docker Hub listing.
func registryReporting(
	digests map[string]string,
) func(context.Context, *dockerfile.ParsedImageRef) ([]dockerfile.RegistryTag, error) {
	return func(_ context.Context, _ *dockerfile.ParsedImageRef) ([]dockerfile.RegistryTag, error) {
		tags := make([]dockerfile.RegistryTag, 0, len(digests))
		for name, digest := range digests {
			tags = append(tags, dockerfile.RegistryTag{Name: name, Digest: digest})
		}
		return tags, nil
	}
}
