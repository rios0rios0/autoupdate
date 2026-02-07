package gitlab //nolint:testpackage // tests unexported functions

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/domain"
)

func TestGitLabProvider(t *testing.T) {
	t.Parallel()

	t.Run("Name", func(t *testing.T) {
		t.Parallel()

		t.Run("should return gitlab", func(t *testing.T) {
			t.Parallel()

			// given
			p := New("token").(*Provider)

			// when
			name := p.Name()

			// then
			assert.Equal(t, "gitlab", name)
		})
	})

	t.Run("MatchesURL", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name     string
			url      string
			expected bool
		}{
			{
				name:     "should match HTTPS GitLab URL",
				url:      "https://gitlab.com/group/repo.git",
				expected: true,
			},
			{
				name:     "should match SSH GitLab URL",
				url:      "git@gitlab.com:group/repo.git",
				expected: true,
			},
			{
				name:     "should not match GitHub URL",
				url:      "https://github.com/org/repo.git",
				expected: false,
			},
			{
				name:     "should not match Azure DevOps URL",
				url:      "https://dev.azure.com/org/project/_git/repo",
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				// given
				p := New("token")

				// when
				result := p.MatchesURL(tt.url)

				// then
				assert.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("AuthToken", func(t *testing.T) {
		t.Parallel()

		t.Run("should return the configured token", func(t *testing.T) {
			t.Parallel()

			// given
			p := New("glpat-my-token")

			// when
			token := p.AuthToken()

			// then
			assert.Equal(t, "glpat-my-token", token)
		})
	})

	t.Run("CloneURL", func(t *testing.T) {
		t.Parallel()

		t.Run("should embed oauth2 credentials in HTTPS URL", func(t *testing.T) {
			t.Parallel()

			// given
			p := New("glpat-secret")
			repo := domain.Repository{
				Organization: "my-group",
				Name:         "my-project",
				RemoteURL:    "https://gitlab.com/my-group/my-project.git",
			}

			// when
			cloneURL := p.CloneURL(repo)

			// then
			assert.Equal(
				t,
				"https://oauth2:glpat-secret@gitlab.com/my-group/my-project.git",
				cloneURL,
			)
		})

		t.Run("should construct URL when RemoteURL is empty", func(t *testing.T) {
			t.Parallel()

			// given
			p := New("glpat-tok")
			repo := domain.Repository{
				Organization: "group",
				Name:         "proj",
				RemoteURL:    "",
			}

			// when
			cloneURL := p.CloneURL(repo)

			// then
			assert.Contains(
				t, cloneURL,
				"oauth2:glpat-tok@gitlab.com/group/proj.git",
			)
		})
	})
}

func TestGitLabVersionSorting(t *testing.T) {
	t.Parallel()

	t.Run("should sort semver tags descending", func(t *testing.T) {
		t.Parallel()

		// given
		tags := []string{"v1.0.0", "v3.0.0", "v2.0.0"}

		// when
		sortVersionsDescending(tags)

		// then
		assert.Equal(t, []string{"v3.0.0", "v2.0.0", "v1.0.0"}, tags)
	})
}
