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

func TestDefaultChlogConfig(t *testing.T) {
	t.Parallel()

	t.Run("should mirror the labels chlog ships", func(t *testing.T) {
		t.Parallel()

		// given
		expected := []string{"Added", "Changed", "Deprecated", "Removed", "Fixed", "Security"}

		// when
		config := entities.DefaultChlogConfig()

		// then
		labels := make([]string, 0, len(config.Kinds))
		for _, kind := range config.Kinds {
			labels = append(labels, kind.Label)
		}
		assert.Equal(t, expected, labels)
	})

	t.Run("should never infer a major bump from a kind", func(t *testing.T) {
		t.Parallel()

		// given
		// A major release means a backward-incompatible change, which chlog
		// signals per fragment with --breaking rather than deriving from the
		// category an entry is filed under. This mirrors the invariant chlog
		// asserts on its own defaults.

		// when
		config := entities.DefaultChlogConfig()

		// then
		for _, kind := range config.Kinds {
			assert.NotEqual(t, "major", kind.Auto,
				"kind %q must not infer a major bump; major is reserved for breaking changes",
				kind.Label)
		}
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

	t.Run("should render the fragment byte for byte the way chlog writes it", func(t *testing.T) {
		t.Parallel()

		// given
		config := entities.DefaultChlogConfig()
		entries := []string{"- changed the Go module dependencies to their latest versions"}

		// when
		fragments, err := config.NewChlogFragments(entries, at)

		// then
		require.NoError(t, err)
		require.Len(t, fragments, 1)
		assert.Equal(t,
			"kind: 'Changed'\n"+
				"body: 'changed the Go module dependencies to their latest versions'\n"+
				"time: '2026-07-29T10:30:00Z'\n",
			fragments[0].Content)
	})

	t.Run("should type every value as a quoted string", func(t *testing.T) {
		t.Parallel()

		// given
		config := entities.DefaultChlogConfig()
		entries := []string{"- changed the Docker base image `alpine` from `3.19` to `3.20`"}

		// when
		fragments, err := config.NewChlogFragments(entries, at)

		// then
		require.NoError(t, err)
		require.Len(t, fragments, 1)

		var document yaml.Node
		require.NoError(t, yaml.Unmarshal([]byte(fragments[0].Content), &document))
		require.Len(t, document.Content, 1)
		mapping := document.Content[0]
		require.Equal(t, yaml.MappingNode, mapping.Kind)
		require.Len(t, mapping.Content, 6)

		// The time is the one that used to differ: marshalling the struct emits
		// it as a bare YAML timestamp, which resolves to !!timestamp where chlog
		// wrote a !!str.
		for i := 0; i+1 < len(mapping.Content); i += 2 {
			key, value := mapping.Content[i], mapping.Content[i+1]
			assert.Equal(t, "!!str", value.Tag, key.Value)
			assert.Equal(t, yaml.SingleQuotedStyle, value.Style, key.Value)
		}
		assert.Equal(t,
			[]string{"kind", "body", "time"},
			[]string{mapping.Content[0].Value, mapping.Content[2].Value, mapping.Content[4].Value})
	})

	t.Run("should double an embedded single quote instead of escaping it", func(t *testing.T) {
		t.Parallel()

		// given
		config := entities.DefaultChlogConfig()
		entries := []string{"- changed the tool's default so it doesn't warn"}

		// when
		fragments, err := config.NewChlogFragments(entries, at)

		// then
		require.NoError(t, err)
		require.Len(t, fragments, 1)
		assert.Contains(t, fragments[0].Content,
			"body: 'changed the tool''s default so it doesn''t warn'\n")

		var fragment entities.ChlogFragment
		require.NoError(t, yaml.Unmarshal([]byte(fragments[0].Content), &fragment))
		assert.Equal(t, "changed the tool's default so it doesn't warn", fragment.Body)
	})

	t.Run("should keep the sub-second precision chlog records", func(t *testing.T) {
		t.Parallel()

		// given
		config := entities.DefaultChlogConfig()
		precise := time.Date(2026, time.July, 29, 10, 30, 0, 71365791, time.UTC)

		// when
		fragments, err := config.NewChlogFragments([]string{"- changed a dependency"}, precise)

		// then
		require.NoError(t, err)
		require.Len(t, fragments, 1)
		assert.Contains(t, fragments[0].Content, "time: '2026-07-29T10:30:00.071365791Z'\n")
	})

	t.Run("should record the timestamp in UTC whatever zone it arrives in", func(t *testing.T) {
		t.Parallel()

		// given
		config := entities.DefaultChlogConfig()
		offset := time.Date(2026, time.July, 29, 7, 30, 0, 0, time.FixedZone("BRT", -3*60*60))

		// when
		fragments, err := config.NewChlogFragments([]string{"- changed a dependency"}, offset)

		// then
		require.NoError(t, err)
		require.Len(t, fragments, 1)
		assert.Contains(t, fragments[0].Content, "time: '2026-07-29T10:30:00Z'\n")
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
