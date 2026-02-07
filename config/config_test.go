package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/config"
)

//nolint:tparallel // some subtests use t.Setenv which is incompatible with t.Parallel on parent
func TestResolveToken(t *testing.T) {
	t.Run("should return empty string for empty input", func(t *testing.T) {
		t.Parallel()

		// given
		raw := ""

		// when
		result := config.ResolveToken(raw)

		// then
		assert.Empty(t, result)
	})

	t.Run("should return inline token unchanged", func(t *testing.T) {
		t.Parallel()

		// given
		raw := "ghp_abc123xyz"

		// when
		result := config.ResolveToken(raw)

		// then
		assert.Equal(t, "ghp_abc123xyz", result)
	})

	t.Run("should expand environment variable reference", func(t *testing.T) {
		// NOTE: cannot use t.Parallel() with t.Setenv()

		// given
		t.Setenv("TEST_TOKEN_RESOLVE", "my-secret-token")
		raw := "${TEST_TOKEN_RESOLVE}"

		// when
		result := config.ResolveToken(raw)

		// then
		assert.Equal(t, "my-secret-token", result)
	})

	t.Run("should expand env var embedded in string", func(t *testing.T) {
		// NOTE: cannot use t.Parallel() with t.Setenv()

		// given
		t.Setenv("TEST_PARTIAL_TOKEN", "secret")
		raw := "prefix-${TEST_PARTIAL_TOKEN}-suffix"

		// when
		result := config.ResolveToken(raw)

		// then
		assert.Equal(t, "prefix-secret-suffix", result)
	})

	t.Run("should return empty for unset env var", func(t *testing.T) {
		t.Parallel()

		// given
		raw := "${DEFINITELY_NOT_SET_VAR_12345}"

		// when
		result := config.ResolveToken(raw)

		// then
		assert.Empty(t, result)
	})

	t.Run("should read token from file when path exists", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		tokenFile := filepath.Join(tmpDir, "token.key")
		err := os.WriteFile(tokenFile, []byte("  file-based-token  \n"), 0o600)
		require.NoError(t, err)

		// when
		result := config.ResolveToken(tokenFile)

		// then
		assert.Equal(t, "file-based-token", result)
	})
}

func TestValidate(t *testing.T) {
	t.Parallel()

	t.Run("should fail when no providers configured", func(t *testing.T) {
		t.Parallel()

		// given
		cfg := &config.Config{
			Providers: []config.ProviderConfig{},
		}

		// when
		err := config.Validate(cfg)

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one provider")
	})

	t.Run("should fail when provider type is empty", func(t *testing.T) {
		t.Parallel()

		// given
		cfg := &config.Config{
			Providers: []config.ProviderConfig{
				{Type: "", Token: "tok", Organizations: []string{"org"}},
			},
		}

		// when
		err := config.Validate(cfg)

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "type is required")
	})

	t.Run("should fail when provider token is empty", func(t *testing.T) {
		t.Parallel()

		// given
		cfg := &config.Config{
			Providers: []config.ProviderConfig{
				{Type: "github", Token: "", Organizations: []string{"org"}},
			},
		}

		// when
		err := config.Validate(cfg)

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "token is required")
	})

	t.Run("should fail when organizations list is empty", func(t *testing.T) {
		t.Parallel()

		// given
		cfg := &config.Config{
			Providers: []config.ProviderConfig{
				{Type: "github", Token: "tok", Organizations: []string{}},
			},
		}

		// when
		err := config.Validate(cfg)

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "organizations must have at least one entry")
	})

	t.Run("should pass with valid configuration", func(t *testing.T) {
		t.Parallel()

		// given
		cfg := &config.Config{
			Providers: []config.ProviderConfig{
				{
					Type:          "github",
					Token:         "ghp_token",
					Organizations: []string{"my-org"},
				},
			},
		}

		// when
		err := config.Validate(cfg)

		// then
		require.NoError(t, err)
	})

	t.Run("should pass with multiple valid providers", func(t *testing.T) {
		t.Parallel()

		// given
		cfg := &config.Config{
			Providers: []config.ProviderConfig{
				{
					Type:          "github",
					Token:         "ghp_token",
					Organizations: []string{"org1"},
				},
				{
					Type:          "gitlab",
					Token:         "glpat_token",
					Organizations: []string{"group1"},
				},
			},
		}

		// when
		err := config.Validate(cfg)

		// then
		require.NoError(t, err)
	})
}

//nolint:tparallel // some subtests use t.Setenv which is incompatible with t.Parallel on parent
func TestLoad(t *testing.T) {
	t.Run("should load valid config file", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		cfgFile := filepath.Join(tmpDir, "autoupdate.yaml")
		content := `
providers:
  - type: github
    token: "ghp_test_token"
    organizations:
      - "test-org"
updaters:
  terraform:
    enabled: true
    auto_complete: false
    target_branch: "main"
  golang:
    enabled: true
`
		err := os.WriteFile(cfgFile, []byte(content), 0o600)
		require.NoError(t, err)

		// when
		cfg, err := config.Load(cfgFile)

		// then
		require.NoError(t, err)
		assert.Len(t, cfg.Providers, 1)
		assert.Equal(t, "github", cfg.Providers[0].Type)
		assert.Equal(t, "ghp_test_token", cfg.Providers[0].Token)
		assert.Equal(t, []string{"test-org"}, cfg.Providers[0].Organizations)
		assert.True(t, cfg.Updaters["terraform"].Enabled)
		assert.False(t, cfg.Updaters["terraform"].AutoComplete)
		assert.Equal(t, "main", cfg.Updaters["terraform"].TargetBranch)
		assert.True(t, cfg.Updaters["golang"].Enabled)
	})

	t.Run("should expand env vars in token during load", func(t *testing.T) {
		// NOTE: cannot use t.Parallel() with t.Setenv()

		// given
		t.Setenv("TEST_LOAD_TOKEN", "expanded-token-value")
		tmpDir := t.TempDir()
		cfgFile := filepath.Join(tmpDir, "autoupdate.yaml")
		content := `
providers:
  - type: github
    token: "${TEST_LOAD_TOKEN}"
    organizations:
      - "org"
`
		err := os.WriteFile(cfgFile, []byte(content), 0o600)
		require.NoError(t, err)

		// when
		cfg, err := config.Load(cfgFile)

		// then
		require.NoError(t, err)
		assert.Equal(t, "expanded-token-value", cfg.Providers[0].Token)
	})

	t.Run("should fail for nonexistent config file", func(t *testing.T) {
		t.Parallel()

		// given
		path := "/tmp/nonexistent_autoupdate_config_xyz.yaml"

		// when
		cfg, err := config.Load(path)

		// then
		require.Error(t, err)
		assert.Nil(t, cfg)
		assert.Contains(t, err.Error(), "failed to read config file")
	})

	t.Run("should fail for invalid YAML", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		cfgFile := filepath.Join(tmpDir, "bad.yaml")
		err := os.WriteFile(cfgFile, []byte("{{{{invalid yaml"), 0o600)
		require.NoError(t, err)

		// when
		cfg, err := config.Load(cfgFile)

		// then
		require.Error(t, err)
		assert.Nil(t, cfg)
		assert.Contains(t, err.Error(), "failed to parse config file")
	})

	t.Run("should fail validation when providers missing", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()
		cfgFile := filepath.Join(tmpDir, "empty.yaml")
		err := os.WriteFile(cfgFile, []byte("updaters: {}"), 0o600)
		require.NoError(t, err)

		// when
		cfg, err := config.Load(cfgFile)

		// then
		require.Error(t, err)
		assert.Nil(t, cfg)
		assert.Contains(t, err.Error(), "at least one provider")
	})
}

func TestFindConfigFile(t *testing.T) {
	t.Run("should return error when no config file exists", func(t *testing.T) {
		// given
		tmpDir := t.TempDir()
		t.Chdir(tmpDir)

		// when
		path, err := config.FindConfigFile()

		// then
		require.Error(t, err)
		assert.Empty(t, path)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("should find autoupdate.yaml in current directory", func(t *testing.T) {
		// given
		tmpDir := t.TempDir()
		t.Chdir(tmpDir)

		cfgFile := filepath.Join(tmpDir, "autoupdate.yaml")
		require.NoError(t, os.WriteFile(cfgFile, []byte("providers: []"), 0o600))

		// when
		path, err := config.FindConfigFile()

		// then
		require.NoError(t, err)
		assert.Equal(t, "autoupdate.yaml", path)
	})

	t.Run("should find .autoupdate.yaml in current directory", func(t *testing.T) {
		// given
		tmpDir := t.TempDir()
		t.Chdir(tmpDir)

		cfgFile := filepath.Join(tmpDir, ".autoupdate.yaml")
		require.NoError(t, os.WriteFile(cfgFile, []byte("providers: []"), 0o600))

		// when
		path, err := config.FindConfigFile()

		// then
		require.NoError(t, err)
		assert.Equal(t, ".autoupdate.yaml", path)
	})
}
