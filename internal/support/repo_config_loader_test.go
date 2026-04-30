//go:build unit

package support_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
	doubles "github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestLoadLocalRepoConfig(t *testing.T) {
	t.Parallel()

	t.Run("should return empty config when file is missing", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()

		// when
		cfg, err := support.LoadLocalRepoConfig(dir)

		// then
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.False(t, cfg.IsSkipped())
	})

	t.Run("should parse skip directive when file exists", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()
		path := filepath.Join(dir, entities.RepoConfigFile)
		require.NoError(t, os.WriteFile(path,
			[]byte("skip: true\nreason: \"fork\"\n"), 0o600))

		// when
		cfg, err := support.LoadLocalRepoConfig(dir)

		// then
		require.NoError(t, err)
		assert.True(t, cfg.IsSkipped())
		assert.Equal(t, "fork", cfg.Reason)
	})

	t.Run("should propagate parse errors for malformed YAML", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()
		path := filepath.Join(dir, entities.RepoConfigFile)
		require.NoError(t, os.WriteFile(path,
			[]byte("skip: : not-yaml"), 0o600))

		// when
		_, err := support.LoadLocalRepoConfig(dir)

		// then
		require.Error(t, err)
	})
}

func TestLoadRemoteRepoConfig(t *testing.T) {
	t.Parallel()

	repo := entities.Repository{Organization: "org", Name: "repo"}

	t.Run("should return empty config when provider reports no file", func(t *testing.T) {
		t.Parallel()

		// given
		spy := doubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()

		// when
		cfg, err := support.LoadRemoteRepoConfig(context.Background(), spy, repo)

		// then
		require.NoError(t, err)
		assert.False(t, cfg.IsSkipped())
	})

	t.Run("should parse remote skip directive", func(t *testing.T) {
		t.Parallel()

		// given
		spy := doubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{entities.RepoConfigFile: true}).
			WithFileContents(map[string]string{
				entities.RepoConfigFile: "skip: true\nreason: \"manually maintained\"\n",
			}).
			BuildSpy()

		// when
		cfg, err := support.LoadRemoteRepoConfig(context.Background(), spy, repo)

		// then
		require.NoError(t, err)
		assert.True(t, cfg.IsSkipped())
		assert.Equal(t, "manually maintained", cfg.Reason)
	})

	t.Run("should return error when GetFileContent fails", func(t *testing.T) {
		t.Parallel()

		// given
		spy := doubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{entities.RepoConfigFile: true}).
			WithFileContentErr(errors.New("api blew up")).
			BuildSpy()

		// when
		_, err := support.LoadRemoteRepoConfig(context.Background(), spy, repo)

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), entities.RepoConfigFile)
	})
}
