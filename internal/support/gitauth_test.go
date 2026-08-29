package support_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

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
