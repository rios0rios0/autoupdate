//go:build unit

package gitlocal_test

import (
	"context"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlocal"
	globalEntities "github.com/rios0rios0/gitforge/pkg/global/domain/entities"
)

func TestCollectBatchAuthMethods(t *testing.T) {
	t.Parallel()

	t.Run("should collect auth methods from provider token", func(t *testing.T) {
		t.Parallel()

		// given
		resolver := &stubPushAuthResolver{
			authMethods: []transport.AuthMethod{
				&http.BasicAuth{Username: "x-access-token", Password: "provider-token"},
			},
		}
		settings := &entities.Settings{}

		// when
		methods := gitlocal.CollectBatchAuthMethods(
			resolver, globalEntities.GITHUB, "provider-token", settings,
		)

		// then
		require.Len(t, methods, 1)
	})

	t.Run("should collect auth methods from global token when provider token is empty", func(t *testing.T) {
		t.Parallel()

		// given
		resolver := &stubPushAuthResolver{
			authMethods: []transport.AuthMethod{
				&http.BasicAuth{Username: "x-access-token", Password: "global-token"},
			},
		}
		settings := &entities.Settings{
			GitHubAccessToken: "global-token",
		}

		// when
		methods := gitlocal.CollectBatchAuthMethods(
			resolver, globalEntities.GITHUB, "", settings,
		)

		// then
		require.Len(t, methods, 1)
	})

	t.Run("should collect multiple auth methods for GitLab with CI job token", func(t *testing.T) {
		t.Parallel()

		// given
		resolver := &stubPushAuthResolver{
			authMethods: []transport.AuthMethod{
				&http.BasicAuth{Username: "oauth2", Password: "some-token"},
			},
		}
		settings := &entities.Settings{
			GitLabAccessToken: "gitlab-pat",
			GitLabCIJobToken:  "ci-job-token",
		}

		// when
		methods := gitlocal.CollectBatchAuthMethods(
			resolver, globalEntities.GITLAB, "provider-token", settings,
		)

		// then
		assert.Len(t, methods, 3) // provider + gitlab PAT + CI job token
	})

	t.Run("should deduplicate tokens", func(t *testing.T) {
		t.Parallel()

		// given
		resolver := &stubPushAuthResolver{
			authMethods: []transport.AuthMethod{
				&http.BasicAuth{Username: "x-access-token", Password: "same-token"},
			},
		}
		settings := &entities.Settings{
			GitHubAccessToken: "same-token",
		}

		// when
		methods := gitlocal.CollectBatchAuthMethods(
			resolver, globalEntities.GITHUB, "same-token", settings,
		)

		// then
		assert.Len(t, methods, 1) // deduplicated
	})

	t.Run("should return nil when resolver is nil", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{GitHubAccessToken: "token"}

		// when
		methods := gitlocal.CollectBatchAuthMethods(
			nil, globalEntities.GITHUB, "token", settings,
		)

		// then
		assert.Nil(t, methods)
	})

	t.Run("should return nil for unsupported service type", func(t *testing.T) {
		t.Parallel()

		// given
		resolver := &stubPushAuthResolver{}
		settings := &entities.Settings{}

		// when
		methods := gitlocal.CollectBatchAuthMethods(
			resolver, globalEntities.UNKNOWN, "token", settings,
		)

		// then
		assert.Nil(t, methods)
	})
}

func TestResolveServiceTypeFromURL(t *testing.T) {
	t.Parallel()

	t.Run("should return service type from adapter", func(t *testing.T) {
		t.Parallel()

		// given
		resolver := &stubPushAuthResolver{
			serviceType: globalEntities.GITHUB,
		}

		// when
		result := gitlocal.ResolveServiceTypeFromURL(resolver, "https://github.com/org/repo.git")

		// then
		assert.Equal(t, globalEntities.GITHUB, result)
	})

	t.Run("should return UNKNOWN when resolver is nil", func(t *testing.T) {
		t.Parallel()

		// when
		result := gitlocal.ResolveServiceTypeFromURL(nil, "https://github.com/org/repo.git")

		// then
		assert.Equal(t, globalEntities.UNKNOWN, result)
	})

	t.Run("should return UNKNOWN when no adapter matches", func(t *testing.T) {
		t.Parallel()

		// given
		resolver := &stubPushAuthResolver{adapterNil: true}

		// when
		result := gitlocal.ResolveServiceTypeFromURL(resolver, "https://unknown.com/repo.git")

		// then
		assert.Equal(t, globalEntities.UNKNOWN, result)
	})
}

// --- test doubles ---

type stubPushAuthResolver struct {
	serviceType globalEntities.ServiceType
	authMethods []transport.AuthMethod
	adapterNil  bool
}

func (s *stubPushAuthResolver) GetAdapterByURL(_ string) globalEntities.LocalGitAuthProvider {
	if s.adapterNil {
		return nil
	}
	return &stubLocalGitAuthProvider{serviceType: s.serviceType}
}

func (s *stubPushAuthResolver) GetAuthProvider(
	_ globalEntities.ServiceType, _ string,
) (globalEntities.LocalGitAuthProvider, error) {
	return &stubLocalGitAuthProvider{
		serviceType: s.serviceType,
		authMethods: s.authMethods,
	}, nil
}

type stubLocalGitAuthProvider struct {
	serviceType globalEntities.ServiceType
	authMethods []transport.AuthMethod
}

func (s *stubLocalGitAuthProvider) Name() string             { return "stub" }
func (s *stubLocalGitAuthProvider) MatchesURL(_ string) bool { return true }
func (s *stubLocalGitAuthProvider) AuthToken() string        { return "" }

func (s *stubLocalGitAuthProvider) CloneURL(_ globalEntities.Repository) string                { return "" }
func (s *stubLocalGitAuthProvider) SSHCloneURL(_ globalEntities.Repository, _ string) string   { return "" }

func (s *stubLocalGitAuthProvider) DiscoverRepositories(
	_ context.Context, _ string,
) ([]globalEntities.Repository, error) {
	return nil, nil
}

func (s *stubLocalGitAuthProvider) CreatePullRequest(
	_ context.Context, _ globalEntities.Repository, _ globalEntities.PullRequestInput,
) (*globalEntities.PullRequest, error) {
	return nil, nil
}

func (s *stubLocalGitAuthProvider) PullRequestExists(
	_ context.Context, _ globalEntities.Repository, _ string,
) (bool, error) {
	return false, nil
}

func (s *stubLocalGitAuthProvider) GetServiceType() globalEntities.ServiceType {
	return s.serviceType
}

func (s *stubLocalGitAuthProvider) ConfigureTransport() {}

func (s *stubLocalGitAuthProvider) GetAuthMethods(_ string) []transport.AuthMethod {
	return s.authMethods
}

func (s *stubLocalGitAuthProvider) PrepareCloneURL(url string) string {
	return url
}
