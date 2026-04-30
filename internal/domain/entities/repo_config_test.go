//go:build unit

package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

func TestRepoConfigIsSkipped(t *testing.T) {
	t.Parallel()

	t.Run("should return false for nil receiver", func(t *testing.T) {
		t.Parallel()

		// given
		var cfg *entities.RepoConfig

		// when
		result := cfg.IsSkipped()

		// then
		assert.False(t, result)
	})

	t.Run("should return false for zero-value config", func(t *testing.T) {
		t.Parallel()

		// given
		cfg := &entities.RepoConfig{}

		// when
		result := cfg.IsSkipped()

		// then
		assert.False(t, result)
	})

	t.Run("should return true when Skip is set", func(t *testing.T) {
		t.Parallel()

		// given
		cfg := &entities.RepoConfig{Skip: true, Reason: "fork; rebase manually"}

		// when
		result := cfg.IsSkipped()

		// then
		assert.True(t, result)
	})
}

func TestParseRepoConfig(t *testing.T) {
	t.Parallel()

	t.Run("should return zero-value config for empty input", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte("")

		// when
		cfg, err := entities.ParseRepoConfig(data)

		// then
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.False(t, cfg.IsSkipped())
		assert.Empty(t, cfg.Reason)
	})

	t.Run("should decode skip and reason", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte("skip: true\nreason: \"fork of upstream; rebase manually\"\n")

		// when
		cfg, err := entities.ParseRepoConfig(data)

		// then
		require.NoError(t, err)
		assert.True(t, cfg.IsSkipped())
		assert.Equal(t, "fork of upstream; rebase manually", cfg.Reason)
	})

	t.Run("should decode skip without reason", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte("skip: true\n")

		// when
		cfg, err := entities.ParseRepoConfig(data)

		// then
		require.NoError(t, err)
		assert.True(t, cfg.IsSkipped())
		assert.Empty(t, cfg.Reason)
	})

	t.Run("should ignore unknown keys for forward compatibility", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte("skip: false\nfuture_key: something\n")

		// when
		cfg, err := entities.ParseRepoConfig(data)

		// then
		require.NoError(t, err)
		assert.False(t, cfg.IsSkipped())
	})

	t.Run("should return error for malformed YAML", func(t *testing.T) {
		t.Parallel()

		// given
		data := []byte("skip: : not-yaml")

		// when
		_, err := entities.ParseRepoConfig(data)

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), entities.RepoConfigFile)
	})
}

func TestRepoConfigFileName(t *testing.T) {
	t.Parallel()

	// given/when/then
	assert.Equal(t, ".autoupdate.yaml", entities.RepoConfigFile)
}
