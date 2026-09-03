package dockerfile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/dockerfile"
)

func TestParseTag(t *testing.T) {
	t.Parallel()

	t.Run("should parse 3-part version tag", func(t *testing.T) {
		t.Parallel()

		// given / when
		version, suffix, precision, ok := dockerfile.ParseTag("1.25.7")

		// then
		assert.True(t, ok)
		assert.Equal(t, "1.25.7", version)
		assert.Empty(t, suffix)
		assert.Equal(t, 3, precision)
	})

	t.Run("should parse 2-part version tag", func(t *testing.T) {
		t.Parallel()

		// given / when
		version, suffix, precision, ok := dockerfile.ParseTag("3.13")

		// then
		assert.True(t, ok)
		assert.Equal(t, "3.13", version)
		assert.Empty(t, suffix)
		assert.Equal(t, 2, precision)
	})

	t.Run("should parse tag with suffix", func(t *testing.T) {
		t.Parallel()

		// given / when
		version, suffix, precision, ok := dockerfile.ParseTag("3.13-slim-bullseye")

		// then
		assert.True(t, ok)
		assert.Equal(t, "3.13", version)
		assert.Equal(t, "-slim-bullseye", suffix)
		assert.Equal(t, 2, precision)
	})

	t.Run("should reject non-version tag", func(t *testing.T) {
		t.Parallel()

		// given / when
		_, _, _, ok := dockerfile.ParseTag("alpine")

		// then
		assert.False(t, ok)
	})

	t.Run("should parse tag with alpine suffix", func(t *testing.T) {
		t.Parallel()

		// given / when
		version, suffix, precision, ok := dockerfile.ParseTag("1.26.0-alpine")

		// then
		assert.True(t, ok)
		assert.Equal(t, "1.26.0", version)
		assert.Equal(t, "-alpine", suffix)
		assert.Equal(t, 3, precision)
	})
}

func TestFindBestUpgrade(t *testing.T) {
	t.Parallel()

	t.Run("should stay within the same minor for a patch-pin when majors are refused", func(t *testing.T) {
		t.Parallel()

		// given
		current := &dockerfile.ParsedImageRef{
			Namespace: "",
			Image:     "golang",
			Tag:       "1.25.7",
			Version:   "1.25.7",
			Suffix:    "",
			Precision: 3,
		}
		tags := []string{"1.25.8", "1.25.9", "1.26.0", "1.25.6", "latest"}

		// when
		best := dockerfile.FindBestUpgrade(current, tags, false)

		// then
		assert.Equal(t, "1.25.9", best)
	})

	t.Run("should stay within the same major for a 2-part tag when majors are refused", func(t *testing.T) {
		t.Parallel()

		// given
		current := &dockerfile.ParsedImageRef{
			Namespace: "",
			Image:     "golang",
			Tag:       "1.25",
			Version:   "1.25",
			Suffix:    "",
			Precision: 2,
		}
		tags := []string{"1.26", "1.27", "2.0", "1.24", "latest"}

		// when
		best := dockerfile.FindBestUpgrade(current, tags, false)

		// then
		assert.Equal(t, "1.27", best)
	})

	t.Run("should match suffix when upgrading", func(t *testing.T) {
		t.Parallel()

		// given
		current := &dockerfile.ParsedImageRef{
			Namespace: "",
			Image:     "python",
			Tag:       "3.12-slim-bullseye",
			Version:   "3.12",
			Suffix:    "-slim-bullseye",
			Precision: 2,
		}
		tags := []string{"3.13", "3.13-slim-bullseye", "3.13-alpine", "3.12-slim-bullseye"}

		// when
		best := dockerfile.FindBestUpgrade(current, tags, true)

		// then
		assert.Equal(t, "3.13-slim-bullseye", best)
	})

	t.Run("should not cross a major boundary when majors are refused", func(t *testing.T) {
		t.Parallel()

		// given
		current := &dockerfile.ParsedImageRef{
			Namespace: "",
			Image:     "node",
			Tag:       "20",
			Version:   "20",
			Suffix:    "",
			Precision: 1,
		}
		tags := []string{"21", "22", "20", "19"}

		// when
		best := dockerfile.FindBestUpgrade(current, tags, false)

		// then
		assert.Empty(t, best)
	})

	t.Run("should return empty when already at latest", func(t *testing.T) {
		t.Parallel()

		// given
		current := &dockerfile.ParsedImageRef{
			Namespace: "",
			Image:     "golang",
			Tag:       "1.26.0",
			Version:   "1.26.0",
			Suffix:    "",
			Precision: 3,
		}
		tags := []string{"1.26.0", "1.25.9", "1.25.8"}

		// when
		best := dockerfile.FindBestUpgrade(current, tags, true)

		// then
		assert.Empty(t, best)
	})

	t.Run("should not cross a minor for a patch-pin when majors are refused", func(t *testing.T) {
		t.Parallel()

		// given
		current := &dockerfile.ParsedImageRef{
			Namespace: "",
			Image:     "golang",
			Tag:       "1.25.7",
			Version:   "1.25.7",
			Suffix:    "",
			Precision: 3,
		}
		tags := []string{"1.26.0", "1.25.8"}

		// when
		best := dockerfile.FindBestUpgrade(current, tags, false)

		// then
		assert.Equal(t, "1.25.8", best)
	})
}

func TestParsedImageRefFullName(t *testing.T) {
	t.Parallel()

	t.Run("should return image name for official images", func(t *testing.T) {
		t.Parallel()

		// given
		ref := &dockerfile.ParsedImageRef{
			Namespace: "",
			Image:     "golang",
		}

		// when
		name := ref.FullName()

		// then
		assert.Equal(t, "golang", name)
	})

	t.Run("should return namespace/image for third-party images", func(t *testing.T) {
		t.Parallel()

		// given
		ref := &dockerfile.ParsedImageRef{
			Namespace: "bitnami",
			Image:     "redis",
		}

		// when
		name := ref.FullName()

		// then
		assert.Equal(t, "bitnami/redis", name)
	})
}

// TestFindBestUpgradeMajorMode covers the mode this updater never had. It
// refused every major unconditionally, which made it the strictest of the ten
// and the only one whose answer no configuration could change — a Dockerfile on
// `node:20` stayed on 20 for ever, including past its end of life.
func TestFindBestUpgradeMajorMode(t *testing.T) {
	t.Parallel()

	t.Run("should cross a major when allowed", func(t *testing.T) {
		t.Parallel()

		// given
		current := &dockerfile.ParsedImageRef{
			Namespace: "", Image: "node", Tag: "20",
			Version: "20", Suffix: "", Precision: 1,
		}
		tags := []string{"18", "20", "21", "22"}

		// when
		best := dockerfile.FindBestUpgrade(current, tags, true)

		// then
		assert.Equal(t, "22", best)
	})

	t.Run("should hold the major when refused", func(t *testing.T) {
		t.Parallel()

		// given
		current := &dockerfile.ParsedImageRef{
			Namespace: "", Image: "node", Tag: "20",
			Version: "20", Suffix: "", Precision: 1,
		}
		tags := []string{"18", "20", "21", "22"}

		// when
		best := dockerfile.FindBestUpgrade(current, tags, false)

		// then -- 20 is already the newest on its own major line
		assert.Empty(t, best)
	})

	t.Run("should still match suffix and precision when majors are allowed", func(t *testing.T) {
		t.Parallel()

		// given -- the suffix encodes what the Dockerfile chose, not how far
		// behind it is, so allowing majors must not start crossing variants
		current := &dockerfile.ParsedImageRef{
			Namespace: "", Image: "node", Tag: "20-alpine",
			Version: "20", Suffix: "-alpine", Precision: 1,
		}
		tags := []string{"22", "22-slim", "22-alpine"}

		// when
		best := dockerfile.FindBestUpgrade(current, tags, true)

		// then
		assert.Equal(t, "22-alpine", best)
	})
}
