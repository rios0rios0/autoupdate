package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// TestFinalizeSettingsGitLabCIServerHost is deliberately not parallel: it sets CI_SERVER_HOST
// and CI_SERVER_URL with t.Setenv, which the runtime forbids in a parallel test.
func TestFinalizeSettingsGitLabCIServerHost(t *testing.T) {
	t.Run("should record CI_SERVER_HOST as the server the CI job token belongs to when it is set",
		func(t *testing.T) {
			// given
			t.Setenv("CI_SERVER_HOST", "gitlab.example.com")
			t.Setenv("CI_SERVER_URL", "https://other.example.com")
			settings := &entities.Settings{}

			// when
			entities.FinalizeSettings(settings)

			// then
			assert.Equal(t, "gitlab.example.com", settings.GitLabCIServerHost)
		})

	t.Run("should record CI_SERVER_URL's host when CI_SERVER_HOST is unset", func(t *testing.T) {
		// given
		t.Setenv("CI_SERVER_HOST", "")
		t.Setenv("CI_SERVER_URL", "https://gitlab.example.com:8443")
		settings := &entities.Settings{}

		// when
		entities.FinalizeSettings(settings)

		// then
		assert.Equal(t, "gitlab.example.com", settings.GitLabCIServerHost)
	})

	t.Run("should record no server when the run is not a GitLab CI job", func(t *testing.T) {
		// given
		t.Setenv("CI_SERVER_HOST", "")
		t.Setenv("CI_SERVER_URL", "")
		settings := &entities.Settings{}

		// when
		entities.FinalizeSettings(settings)

		// then
		assert.Empty(t, settings.GitLabCIServerHost)
	})
}
