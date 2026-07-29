//go:build unit

package entities_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

func TestParseChlogConfig(t *testing.T) {
	t.Parallel()

	t.Run("should return chlog defaults when the file is empty", func(t *testing.T) {
		t.Parallel()

		// given
		var data []byte

		// when
		config, err := entities.ParseChlogConfig(data)

		// then
		require.NoError(t, err)
		assert.Equal(t, ".changes", config.ChangesDir)
		assert.Equal(t, "unreleased", config.UnreleasedDir)
		assert.Equal(t, "CHANGELOG.md", config.ChangelogPath)
		assert.Len(t, config.Kinds, 6)
	})

	t.Run("should honour the directories declared by the file", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte("changesDir: docs/changes\nunreleasedDir: pending\n")

		// when
		config, err := entities.ParseChlogConfig(data)

		// then
		require.NoError(t, err)
		assert.Equal(t, "docs/changes/pending", config.UnreleasedPath())
	})

	t.Run("should fill the omitted keys from the defaults", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte("changelogPath: docs/CHANGELOG.md\n")

		// when
		config, err := entities.ParseChlogConfig(data)

		// then
		require.NoError(t, err)
		assert.Equal(t, "docs/CHANGELOG.md", config.ChangelogPath)
		assert.Equal(t, ".changes/unreleased", config.UnreleasedPath())
	})

	t.Run("should ignore the keys autoupdate does not read", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte("versionFormat: '## {{.Version}}'\nchangeFormat: '* {{.Body}}'\n")

		// when
		config, err := entities.ParseChlogConfig(data)

		// then
		require.NoError(t, err)
		assert.Equal(t, ".changes/unreleased", config.UnreleasedPath())
	})

	t.Run("should reject a directory that escapes the repository root", func(t *testing.T) {
		t.Parallel()

		// given
		escaping := []string{
			"changesDir: ../../etc\n",
			"changesDir: /etc\n",
			"unreleasedDir: ../../../tmp\n",
			"changelogPath: /tmp/CHANGELOG.md\n",
			"changesDir: .changes\nunreleasedDir: ../../..\n",
		}

		for _, data := range escaping {
			// when
			config, err := entities.ParseChlogConfig([]byte(data))

			// then
			require.ErrorIs(t, err, entities.ErrChlogPathEscapesRepo, data)
			assert.Nil(t, config)
		}
	})

	t.Run("should reject a backslash path that escapes the repository root", func(t *testing.T) {
		t.Parallel()

		// A configuration is committed once and read wherever autoupdate runs, so
		// these have to be rejected on Linux too even though only Windows resolves
		// the separator. `filepath.ToSlash` is a no-op on Linux, so nothing but an
		// unconditional normalization catches them.
		// given
		escaping := []string{
			`changesDir: ..\..\etc` + "\n",
			`unreleasedDir: ..\..\..\tmp` + "\n",
			`changelogPath: \tmp\evil` + "\n",
			`changesDir: \\server\share` + "\n",
			`changesDir: .changes` + "\n" + `unreleasedDir: ..\..` + "\n",
		}

		for _, data := range escaping {
			// when
			config, err := entities.ParseChlogConfig([]byte(data))

			// then
			require.ErrorIs(t, err, entities.ErrChlogPathEscapesRepo, data)
			assert.Nil(t, config)
		}
	})

	t.Run("should read a backslash directory as the same path a slash one names", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte(`changesDir: docs\changes` + "\n" + `unreleasedDir: pending` + "\n")

		// when
		config, err := entities.ParseChlogConfig(data)

		// then
		require.NoError(t, err)
		assert.Equal(t, "docs/changes/pending", config.UnreleasedPath())
	})

	t.Run("should return an error when the file is not valid YAML", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte("changesDir: [unterminated\n")

		// when
		config, err := entities.ParseChlogConfig(data)

		// then
		require.Error(t, err)
		assert.Nil(t, config)
	})
}

func TestChlogConfigKindLabel(t *testing.T) {
	t.Parallel()

	t.Run("should return the spelling the repository configured", func(t *testing.T) {
		t.Parallel()

		// given
		config, err := entities.ParseChlogConfig([]byte("kinds:\n  - label: CHANGED\n  - label: Fixed\n"))
		require.NoError(t, err)

		// when
		label := config.KindLabel("Changed")

		// then
		assert.Equal(t, "CHANGED", label)
	})

	t.Run("should fall back to the canonical label when the kind is not configured", func(t *testing.T) {
		t.Parallel()

		// given
		config, err := entities.ParseChlogConfig([]byte("kinds:\n  - label: Fixed\n"))
		require.NoError(t, err)

		// when
		label := config.KindLabel("Changed")

		// then
		assert.Equal(t, "Changed", label)
	})
}

func TestNewChlogFragments(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.July, 29, 10, 30, 0, 0, time.UTC)

	t.Run("should render one fragment per entry under the unreleased directory", func(t *testing.T) {
		t.Parallel()

		// given
		config := entities.DefaultChlogConfig()
		entries := []string{
			"- changed the Go module dependencies to their latest versions",
			"- changed the Docker base image `alpine` from `3.19` to `3.20`",
		}

		// when
		fragments, err := config.NewChlogFragments(entries, at)

		// then
		require.NoError(t, err)
		require.Len(t, fragments, 2)
		for _, fragment := range fragments {
			assert.True(t, strings.HasPrefix(fragment.Path, ".changes/unreleased/"), fragment.Path)
			assert.True(t, strings.HasSuffix(fragment.Path, ".yaml"), fragment.Path)
			assert.Equal(t, "add", fragment.ChangeType)
		}
		assert.NotEqual(t, fragments[0].Path, fragments[1].Path)
	})

	t.Run("should store the entry as a chlog fragment without its bullet marker", func(t *testing.T) {
		t.Parallel()

		// given
		config := entities.DefaultChlogConfig()
		entries := []string{"- changed the Go module dependencies to their latest versions"}

		// when
		fragments, err := config.NewChlogFragments(entries, at)

		// then
		require.NoError(t, err)
		require.Len(t, fragments, 1)

		var fragment entities.ChlogFragment
		require.NoError(t, yaml.Unmarshal([]byte(fragments[0].Content), &fragment))
		assert.Equal(t, "Changed", fragment.Kind)
		assert.Equal(t, "changed the Go module dependencies to their latest versions", fragment.Body)
		assert.Equal(t, at, fragment.Time.UTC())
	})

	t.Run("should use the kind label the repository configured", func(t *testing.T) {
		t.Parallel()

		// given
		config, err := entities.ParseChlogConfig([]byte("kinds:\n  - label: Updated\n  - label: changed\n"))
		require.NoError(t, err)

		// when
		fragments, err := config.NewChlogFragments([]string{"- changed a dependency"}, at)

		// then
		require.NoError(t, err)
		require.Len(t, fragments, 1)

		var fragment entities.ChlogFragment
		require.NoError(t, yaml.Unmarshal([]byte(fragments[0].Content), &fragment))
		assert.Equal(t, "changed", fragment.Kind)
	})

	t.Run("should skip an entry that carries no text", func(t *testing.T) {
		t.Parallel()

		// given
		config := entities.DefaultChlogConfig()
		entries := []string{"-", "   ", "- changed a dependency"}

		// when
		fragments, err := config.NewChlogFragments(entries, at)

		// then
		require.NoError(t, err)
		assert.Len(t, fragments, 1)
	})

	t.Run("should write into the directory the configuration declares", func(t *testing.T) {
		t.Parallel()

		// given
		config, err := entities.ParseChlogConfig(
			[]byte("changesDir: docs/changes\nunreleasedDir: pending\n"))
		require.NoError(t, err)

		// when
		fragments, err := config.NewChlogFragments([]string{"- changed a dependency"}, at)

		// then
		require.NoError(t, err)
		require.Len(t, fragments, 1)
		assert.True(t, strings.HasPrefix(fragments[0].Path, "docs/changes/pending/"), fragments[0].Path)
	})
}

func TestStripBulletPrefix(t *testing.T) {
	t.Parallel()

	t.Run("should remove the leading bullet marker", func(t *testing.T) {
		t.Parallel()

		// given
		entry := "- changed a dependency"

		// when
		body := entities.StripBulletPrefix(entry)

		// then
		assert.Equal(t, "changed a dependency", body)
	})

	t.Run("should return the text unchanged when there is no bullet marker", func(t *testing.T) {
		t.Parallel()

		// given
		entry := "  changed a dependency  "

		// when
		body := entities.StripBulletPrefix(entry)

		// then
		assert.Equal(t, "changed a dependency", body)
	})
}
