package support_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/support"
)

func TestGitAuthScript(t *testing.T) {
	t.Parallel()

	t.Run("should rewrite the provider's URLs when the provider is known", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			provider   string
			host       string
			credential string
		}{
			{support.ProviderAzureDevOps, "dev.azure.com", "https://pat:"},
			{support.ProviderGitHub, "github.com", "https://x-access-token:"},
			{support.ProviderGitLab, "gitlab.com", "https://oauth2:"},
		}

		for _, testCase := range testCases {
			t.Run(testCase.provider, func(t *testing.T) {
				t.Parallel()

				// given
				provider := testCase.provider

				// when
				script := support.GitAuthScript(provider)

				// then
				assert.Contains(t, script, testCase.credential)
				assert.Contains(t, script, "insteadOf = https://"+testCase.host+"/")
				assert.Contains(t, script, "GIT_CONFIG_GLOBAL")
			})
		}
	})

	t.Run("should read the token from the environment rather than inline it", func(t *testing.T) {
		t.Parallel()

		// given
		provider := support.ProviderGitHub

		// when
		script := support.GitAuthScript(provider)

		// then
		assert.Contains(t, script, "${AUTH_TOKEN}")
	})

	t.Run("should rewrite Azure DevOps SSH remotes as well as HTTPS ones", func(t *testing.T) {
		t.Parallel()

		// given
		provider := support.ProviderAzureDevOps

		// when
		script := support.GitAuthScript(provider)

		// then
		assert.Contains(t, script, "insteadOf = git@ssh.dev.azure.com:v3/")
		assert.Equal(t, 2, strings.Count(script, "@dev.azure.com/"))
	})

	t.Run("should isolate the config and remove it on exit for every provider", func(t *testing.T) {
		t.Parallel()

		// given
		provider := support.ProviderGitLab

		// when
		script := support.GitAuthScript(provider)

		// then
		assert.Contains(t, script, "TEMP_GITCONFIG=$(mktemp)")
		assert.Contains(t, script, `export GIT_CONFIG_GLOBAL="$TEMP_GITCONFIG"`)
		assert.Contains(t, script, `trap 'rm -f "$TEMP_GITCONFIG"' EXIT`)
	})

	t.Run("should still isolate the config when the provider is unknown", func(t *testing.T) {
		t.Parallel()

		// given
		provider := "unknown-forge"

		// when
		script := support.GitAuthScript(provider)

		// then
		assert.Contains(t, script, "TEMP_GITCONFIG=$(mktemp)")
		assert.NotContains(t, script, "insteadOf")
	})
}

func TestFileGitAuthConfig(t *testing.T) {
	t.Parallel()

	t.Run("should write the same rewrites the script exports", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder
		operatorConfig := []byte("[user]\n\tname = Operator\n")

		// when
		token := "ghs_" + "secret"
		err := support.WriteGitAuthConfig(&sb, operatorConfig, support.ProviderGitHub, token)

		// then
		require.NoError(t, err)
		written := sb.String()
		assert.Contains(t, written, "[user]\n\tname = Operator\n",
			"the operator's own settings must survive the replacement")
		assert.Contains(t, written, `[url "https://x-access-token:`+token+`@github.com/"]`)
		assert.Contains(t, written, "insteadOf = https://github.com/")
	})

	t.Run("should write every rule a provider has", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder

		// when
		err := support.WriteGitAuthConfig(&sb, nil, support.ProviderAzureDevOps, "pat123")

		// then
		require.NoError(t, err)
		written := sb.String()
		assert.Contains(t, written, "insteadOf = https://dev.azure.com/")
		assert.Contains(t, written, "insteadOf = git@ssh.dev.azure.com:v3/")
		assert.Equal(t, 2, strings.Count(written, "pat123"))
	})

	t.Run("should not end the value early when the token quotes", func(t *testing.T) {
		t.Parallel()

		// given
		var sb strings.Builder

		// when
		err := support.WriteGitAuthConfig(&sb, nil, support.ProviderGitLab, `tok"en\`)

		// then
		require.NoError(t, err)
		assert.Contains(t, sb.String(), `tok\"en\\@gitlab.com/`)
	})

	t.Run("should write no rule for an unknown provider or an empty token", func(t *testing.T) {
		t.Parallel()

		for name, testCase := range map[string]struct {
			provider string
			token    string
		}{
			"unknown provider": {provider: "unknown-forge", token: "secret"},
			"empty token":      {provider: support.ProviderGitHub},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				// given
				var sb strings.Builder

				// when
				err := support.WriteGitAuthConfig(&sb, nil, testCase.provider, testCase.token)

				// then
				require.NoError(t, err)
				assert.NotContains(t, sb.String(), "insteadOf")
			})
		}
	})

	t.Run("should create the file in the directory it was given and remove it on cleanup", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()

		// when
		path, cleanup, err := support.FileGitAuthConfig(dir, support.ProviderGitHub, "ghs_secret")

		// then
		require.NoError(t, err)
		assert.Equal(t, dir, filepath.Dir(path), "the file must land where the caller can sweep it")

		written, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.Contains(t, string(written), "x-access-token:ghs_secret@github.com")

		cleanup()
		assert.NoFileExists(t, path)
	})
}

func TestGitAuthEnv(t *testing.T) {
	t.Parallel()

	t.Run("should hand over the token and the config path when both are set", func(t *testing.T) {
		t.Parallel()

		// given / when
		env := support.GitAuthEnv("/tmp/gitconfig", "ghs_secret")

		// then
		assert.Contains(t, env, "AUTH_TOKEN=ghs_secret")
		assert.Contains(t, env, "GIT_HTTPS_TOKEN=ghs_secret")
		assert.Contains(t, env, "GIT_CONFIG_GLOBAL=/tmp/gitconfig")
	})

	t.Run("should never leave a git prompt waiting for input", func(t *testing.T) {
		t.Parallel()

		// given / when
		env := support.GitAuthEnv("", "")

		// then
		assert.Contains(t, env, "GIT_TERMINAL_PROMPT=0")
		assert.NotContains(t, env, "GIT_CONFIG_GLOBAL=",
			"an empty path would replace the operator's gitconfig with nothing")
	})
}
