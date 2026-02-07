package github //nolint:testpackage // tests unexported functions

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/domain"
)

func TestGitHubProvider(t *testing.T) {
	t.Parallel()

	t.Run("Name", func(t *testing.T) {
		t.Parallel()

		t.Run("should return github", func(t *testing.T) {
			t.Parallel()

			// given
			p := New("token").(*Provider)

			// when
			name := p.Name()

			// then
			assert.Equal(t, "github", name)
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
				name:     "should match HTTPS GitHub URL",
				url:      "https://github.com/org/repo.git",
				expected: true,
			},
			{
				name:     "should match SSH GitHub URL",
				url:      "git@github.com:org/repo.git",
				expected: true,
			},
			{
				name:     "should match GitHub URL without .git suffix",
				url:      "https://github.com/org/repo",
				expected: true,
			},
			{
				name:     "should not match GitLab URL",
				url:      "https://gitlab.com/org/repo.git",
				expected: false,
			},
			{
				name:     "should not match Azure DevOps URL",
				url:      "https://dev.azure.com/org/project/_git/repo",
				expected: false,
			},
			{
				name:     "should not match Bitbucket URL",
				url:      "https://bitbucket.org/org/repo.git",
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
			p := New("my-github-token")

			// when
			token := p.AuthToken()

			// then
			assert.Equal(t, "my-github-token", token)
		})
	})

	t.Run("CloneURL", func(t *testing.T) {
		t.Parallel()

		t.Run("should embed x-access-token in HTTPS URL", func(t *testing.T) {
			t.Parallel()

			// given
			p := New("ghp_secret123")
			repo := domain.Repository{
				Organization: "my-org",
				Name:         "my-repo",
				RemoteURL:    "https://github.com/my-org/my-repo.git",
			}

			// when
			cloneURL := p.CloneURL(repo)

			// then
			assert.Equal(
				t,
				"https://x-access-token:ghp_secret123@github.com/my-org/my-repo.git",
				cloneURL,
			)
		})

		t.Run("should construct URL when RemoteURL is empty", func(t *testing.T) {
			t.Parallel()

			// given
			p := New("ghp_token")
			repo := domain.Repository{
				Organization: "org",
				Name:         "repo",
				RemoteURL:    "",
			}

			// when
			cloneURL := p.CloneURL(repo)

			// then
			assert.Contains(
				t, cloneURL,
				"x-access-token:ghp_token@github.com/org/repo.git",
			)
		})
	})
}

func TestGitHubVersionSorting(t *testing.T) {
	t.Parallel()

	t.Run("should sort semver tags descending", func(t *testing.T) {
		t.Parallel()

		// given
		tags := []string{"v1.0.0", "v2.1.0", "v1.5.0", "v2.0.0"}

		// when
		sortVersionsDescending(tags)

		// then
		assert.Equal(
			t,
			[]string{"v2.1.0", "v2.0.0", "v1.5.0", "v1.0.0"},
			tags,
		)
	})

	t.Run("should sort mixed semver and non-semver tags", func(t *testing.T) {
		t.Parallel()

		// given
		tags := []string{"v1.0.0", "latest", "v2.0.0"}

		// when
		sortVersionsDescending(tags)

		// then
		assert.Equal(t, "v2.0.0", tags[0])
	})

	t.Run("should handle tags without v prefix", func(t *testing.T) {
		t.Parallel()

		// given
		tags := []string{"1.0.0", "2.0.0", "1.5.0"}

		// when
		sortVersionsDescending(tags)

		// then
		assert.Equal(t, []string{"2.0.0", "1.5.0", "1.0.0"}, tags)
	})
}

func TestNormalizeVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "should keep v prefix",
			input:    "v1.2.3",
			expected: "v1.2.3",
		},
		{
			name:     "should add v prefix",
			input:    "1.2.3",
			expected: "v1.2.3",
		},
		{
			name:     "should handle empty string",
			input:    "",
			expected: "v",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// given
			input := tt.input

			// when
			result := normalizeVersion(input)

			// then
			assert.Equal(t, tt.expected, result)
		})
	}
}
