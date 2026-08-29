package dart_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dartUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/dart"
)

func writeFvmrc(t *testing.T, content string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".fvmrc")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return dir, path
}

func TestParseFvmVersion(t *testing.T) {
	t.Parallel()

	t.Run("should read the pinned Flutter version", func(t *testing.T) {
		t.Parallel()

		// given / when
		version := dartUpdater.ParseFvmVersion(`{"flutter": "3.38.0"}`)

		// then
		assert.Equal(t, "3.38.0", version)
	})

	t.Run("should return empty when the document pins nothing", func(t *testing.T) {
		t.Parallel()

		// given / when
		version := dartUpdater.ParseFvmVersion(`{"flavors": {}}`)

		// then
		assert.Empty(t, version)
	})

	t.Run("should return empty on malformed JSON rather than guessing", func(t *testing.T) {
		t.Parallel()

		// given / when
		version := dartUpdater.ParseFvmVersion(`{flutter: 3.38.0`)

		// then
		assert.Empty(t, version)
	})
}

func TestWriteFvmVersion(t *testing.T) {
	t.Parallel()

	t.Run("should update the pin and keep the keys it does not know about", func(t *testing.T) {
		t.Parallel()

		// given — a flavors block is a real FVM feature; losing it would break
		// every per-flavor SDK the project configured
		dir, path := writeFvmrc(t, `{"flutter": "3.38.0", "flavors": {"prod": "3.35.0"}}`)

		// when
		changed, err := dartUpdater.WriteFvmVersion(dir, "3.47.0")

		// then
		require.NoError(t, err)
		assert.True(t, changed)

		raw, readErr := os.ReadFile(path)
		require.NoError(t, readErr)

		var config map[string]any
		require.NoError(t, json.Unmarshal(raw, &config))
		assert.Equal(t, "3.47.0", config["flutter"])
		assert.Equal(t, map[string]any{"prod": "3.35.0"}, config["flavors"])
	})

	t.Run("should report no change when the pin is already current", func(t *testing.T) {
		t.Parallel()

		// given
		dir, path := writeFvmrc(t, `{"flutter": "3.47.0"}`)
		before, err := os.ReadFile(path)
		require.NoError(t, err)

		// when
		changed, writeErr := dartUpdater.WriteFvmVersion(dir, "3.47.0")

		// then
		require.NoError(t, writeErr)
		assert.False(t, changed)

		after, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.Equal(t, string(before), string(after))
	})

	t.Run("should return an error when there is no pin file", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()

		// when
		_, err := dartUpdater.WriteFvmVersion(dir, "3.47.0")

		// then
		require.Error(t, err)
	})

	t.Run("should return an error rather than overwrite malformed JSON", func(t *testing.T) {
		t.Parallel()

		// given
		dir, path := writeFvmrc(t, `{flutter: 3.38.0`)

		// when
		_, err := dartUpdater.WriteFvmVersion(dir, "3.47.0")

		// then
		require.Error(t, err)
		raw, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.Equal(t, `{flutter: 3.38.0`, string(raw))
	})
}

func TestApplyFvmPin(t *testing.T) {
	t.Parallel()

	t.Run("should bump the pin when the SDK is behind", func(t *testing.T) {
		t.Parallel()

		// given
		dir, _ := writeFvmrc(t, `{"flutter": "3.38.0"}`)
		vCtx := dartUpdater.NewVersionContext("flutter", "3.47.0", "3.38.0")

		// when
		updated := dartUpdater.ApplyFvmPin(dir, vCtx)

		// then
		assert.True(t, updated)
	})

	t.Run("should do nothing when the repository pins no SDK", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()
		vCtx := dartUpdater.NewVersionContext("dart", "3.13.0", "")

		// when
		updated := dartUpdater.ApplyFvmPin(dir, vCtx)

		// then
		assert.False(t, updated)
	})
}
