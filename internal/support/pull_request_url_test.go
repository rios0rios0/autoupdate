package support_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
)

func TestRepositoryWebURL(t *testing.T) {
	t.Parallel()

	t.Run("should prefer the clone URL and strip what a person must not open", func(t *testing.T) {
		t.Parallel()

		testCases := map[string]string{
			"https://github.com/example/lib.git":                                     "https://github.com/example/lib",
			"https://github.com/example/lib":                                         "https://github.com/example/lib",
			"https://x-access-token:" + "ghs_secret" + "@github.com/example/lib.git": "https://github.com/example/lib",
			"https://org@dev.azure.com/org/project/_git/repo":                        "https://dev.azure.com/org/project/_git/repo",
			"https://gitlab.com/group/sub/lib.git?foo=bar#frag":                      "https://gitlab.com/group/sub/lib",
			"https://github.com/example/lib.git/":                                    "https://github.com/example/lib",
		}

		for remote, want := range testCases {
			t.Run(remote, func(t *testing.T) {
				t.Parallel()

				// given
				repo := entities.Repository{
					Organization: "example", Name: "lib", RemoteURL: remote,
				}

				// when
				got := support.RepositoryWebURL(repo, support.ProviderGitHub)

				// then
				assert.Equal(t, want, got)
			})
		}
	})

	t.Run("should build the URL from the provider when the remote is not HTTP", func(t *testing.T) {
		t.Parallel()

		// given
		repo := entities.Repository{
			Organization: "example", Name: "lib", Project: "proj",
			RemoteURL: "git@github.com:example/lib.git",
		}

		// when / then
		assert.Equal(t,
			"https://github.com/example/lib",
			support.RepositoryWebURL(repo, support.ProviderGitHub))
		assert.Equal(t,
			"https://gitlab.com/example/lib",
			support.RepositoryWebURL(repo, support.ProviderGitLab))
		assert.Equal(t,
			"https://dev.azure.com/example/proj/_git/lib",
			support.RepositoryWebURL(repo, support.ProviderAzureDevOps))
	})

	t.Run("should omit the Azure DevOps project when it is unknown", func(t *testing.T) {
		t.Parallel()

		// given
		repo := entities.Repository{Organization: "example", Name: "lib"}

		// when
		got := support.RepositoryWebURL(repo, support.ProviderAzureDevOps)

		// then
		assert.Equal(t, "https://dev.azure.com/example/_git/lib", got)
	})

	t.Run("should return nothing rather than guess", func(t *testing.T) {
		t.Parallel()

		// given
		sshRemote := entities.Repository{
			Organization: "example", Name: "lib", RemoteURL: "git@github.com:example/lib.git",
		}

		// when / then
		assert.Empty(t, support.RepositoryWebURL(sshRemote, "bitbucket"))
		assert.Empty(t, support.RepositoryWebURL(entities.Repository{}, support.ProviderGitHub))
	})
}

func TestPullRequestCreationURL(t *testing.T) {
	t.Parallel()

	t.Run("should point GitHub at the compare page", func(t *testing.T) {
		t.Parallel()

		// given / when
		got := support.PullRequestCreationURL(
			support.ProviderGitHub, "https://github.com/example/lib",
			"main", "chore/migrate-lib-v3",
		)

		// then
		assert.Equal(t,
			"https://github.com/example/lib/compare/main...chore/migrate-lib-v3?expand=1", got)
	})

	t.Run("should point GitLab at the new-merge-request form", func(t *testing.T) {
		t.Parallel()

		// given / when
		got := support.PullRequestCreationURL(
			support.ProviderGitLab, "https://gitlab.com/example/lib",
			"main", "chore/migrate-lib-v3",
		)

		// then
		assert.Equal(t,
			"https://gitlab.com/example/lib/-/merge_requests/new?"+
				"merge_request%5Bsource_branch%5D=chore%2Fmigrate-lib-v3&"+
				"merge_request%5Btarget_branch%5D=main", got)
	})

	t.Run("should point Azure DevOps at the create-pull-request page", func(t *testing.T) {
		t.Parallel()

		// given / when
		got := support.PullRequestCreationURL(
			support.ProviderAzureDevOps, "https://dev.azure.com/example/proj/_git/lib",
			"main", "chore/migrate-lib-v3",
		)

		// then
		assert.Equal(t,
			"https://dev.azure.com/example/proj/_git/lib/pullrequestcreate?"+
				"sourceRef=refs%2Fheads%2Fchore%2Fmigrate-lib-v3&"+
				"targetRef=refs%2Fheads%2Fmain", got)
	})

	t.Run("should escape a branch name that needs it and leave its slashes", func(t *testing.T) {
		t.Parallel()

		// given / when
		got := support.PullRequestCreationURL(
			support.ProviderGitHub, "https://github.com/example/lib",
			"main", "chore/migrate my lib-v3",
		)

		// then
		assert.Equal(t,
			"https://github.com/example/lib/compare/main...chore/migrate%20my%20lib-v3?expand=1", got)
	})

	t.Run("should return nothing rather than guess", func(t *testing.T) {
		t.Parallel()

		// given / when / then
		assert.Empty(t, support.PullRequestCreationURL("bitbucket", "https://example.com/lib", "main", "b"))
		assert.Empty(t, support.PullRequestCreationURL(support.ProviderGitHub, "", "main", "b"))
		assert.Empty(t, support.PullRequestCreationURL(support.ProviderGitHub, "https://example.com", "", "b"))
		assert.Empty(t, support.PullRequestCreationURL(support.ProviderGitHub, "https://example.com", "main", ""))
	})
}
