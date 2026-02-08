package azuredevops //nolint:testpackage // tests unexported functions

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/domain"
)

func TestAzureDevOpsProvider(t *testing.T) {
	t.Parallel()

	t.Run("Name", func(t *testing.T) {
		t.Parallel()

		t.Run("should return azuredevops", func(t *testing.T) {
			t.Parallel()

			// given
			p := New("token").(*Provider)

			// when
			name := p.Name()

			// then
			assert.Equal(t, "azuredevops", name)
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
				name:     "should match HTTPS Azure DevOps URL",
				url:      "https://dev.azure.com/org/project/_git/repo",
				expected: true,
			},
			{
				name:     "should match URL with username prefix",
				url:      "https://user@dev.azure.com/org/project/_git/repo",
				expected: true,
			},
			{
				name:     "should not match GitHub URL",
				url:      "https://github.com/org/repo.git",
				expected: false,
			},
			{
				name:     "should not match GitLab URL",
				url:      "https://gitlab.com/group/repo.git",
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

		t.Run("should return the configured PAT", func(t *testing.T) {
			t.Parallel()

			// given
			p := New("my-ado-pat")

			// when
			token := p.AuthToken()

			// then
			assert.Equal(t, "my-ado-pat", token)
		})
	})

	t.Run("CloneURL", func(t *testing.T) {
		t.Parallel()

		t.Run("should embed pat credentials in HTTPS URL", func(t *testing.T) {
			t.Parallel()

			// given
			p := New("ado-secret-pat")
			repo := domain.Repository{
				Organization: "MyOrg",
				Project:      "MyProject",
				Name:         "MyRepo",
				RemoteURL:    "https://dev.azure.com/MyOrg/MyProject/_git/MyRepo",
			}

			// when
			cloneURL := p.CloneURL(repo)

			// then
			assert.Equal(
				t,
				"https://pat:ado-secret-pat@dev.azure.com/MyOrg/MyProject/_git/MyRepo",
				cloneURL,
			)
		})

		t.Run("should strip existing username from RemoteURL before embedding PAT", func(t *testing.T) {
			t.Parallel()

			// given
			p := New("ado-secret-pat")
			repo := domain.Repository{
				Organization: "MyOrg",
				Project:      "MyProject",
				Name:         "MyRepo",
				RemoteURL:    "https://MyOrg@dev.azure.com/MyOrg/MyProject/_git/MyRepo",
			}

			// when
			cloneURL := p.CloneURL(repo)

			// then
			assert.Equal(
				t,
				"https://pat:ado-secret-pat@dev.azure.com/MyOrg/MyProject/_git/MyRepo",
				cloneURL,
			)
		})

		t.Run("should construct URL when RemoteURL is empty", func(t *testing.T) {
			t.Parallel()

			// given
			p := New("pat123")
			repo := domain.Repository{
				Organization: "Org",
				Project:      "Proj",
				Name:         "Repo",
				RemoteURL:    "",
			}

			// when
			cloneURL := p.CloneURL(repo)

			// then
			assert.Contains(t, cloneURL, "pat:pat123@dev.azure.com")
			assert.Contains(t, cloneURL, "Org/Proj/_git/Repo")
		})
	})
}

func TestNormalizeOrgURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "should prefix bare org name with base URL",
			input:    "MyOrg",
			expected: "https://dev.azure.com/MyOrg",
		},
		{
			name:     "should keep full URL unchanged",
			input:    "https://dev.azure.com/MyOrg",
			expected: "https://dev.azure.com/MyOrg",
		},
		{
			name:     "should strip trailing slash",
			input:    "https://dev.azure.com/MyOrg/",
			expected: "https://dev.azure.com/MyOrg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// given
			input := tt.input

			// when
			result := normalizeOrgURL(input)

			// then
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractOrgName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "should extract org from standard URL",
			input:    "https://dev.azure.com/MyOrg",
			expected: "MyOrg",
		},
		{
			name:     "should extract first path segment",
			input:    "https://dev.azure.com/MyOrg/extra/path",
			expected: "MyOrg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// given
			input := tt.input

			// when
			result := extractOrgName(input)

			// then
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAzureDevOpsVersionSorting(t *testing.T) {
	t.Parallel()

	t.Run("should sort semver tags descending", func(t *testing.T) {
		t.Parallel()

		// given
		tags := []string{"v0.1.0", "v1.0.0", "v0.5.0"}

		// when
		sortVersionsDescending(tags)

		// then
		assert.Equal(t, []string{"v1.0.0", "v0.5.0", "v0.1.0"}, tags)
	})

	t.Run("should handle empty slice", func(t *testing.T) {
		t.Parallel()

		// given
		var tags []string

		// when
		sortVersionsDescending(tags)

		// then
		assert.Empty(t, tags)
	})
}
