//go:build unit

package repositories_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainRepos "github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
	globalEntities "github.com/rios0rios0/gitforge/pkg/global/domain/entities"
)

func TestProviderRegistry_NewProviderRegistry(t *testing.T) {
	t.Parallel()

	t.Run("should return a non-nil registry", func(t *testing.T) {
		t.Parallel()

		// when
		registry := repositories.NewProviderRegistry()

		// then
		require.NotNil(t, registry)
	})
}

func TestProviderRegistry_RegisterAndGet(t *testing.T) {
	t.Parallel()

	t.Run("should register and retrieve a provider by name", func(t *testing.T) {
		t.Parallel()

		// given
		registry := repositories.NewProviderRegistry()
		factory := func(token string) domainRepos.ProviderRepository {
			return repositorydoubles.NewSpyProviderRepositoryBuilder().
				WithProviderName("test").
				WithToken(token).
				BuildSpy()
		}
		registry.Register("github", factory)

		// when
		provider, err := registry.Get("github", "my-token")

		// then
		require.NoError(t, err)
		require.NotNil(t, provider)
		assert.Equal(t, "test", provider.Name())
		assert.Equal(t, "my-token", provider.AuthToken())
	})
}

func TestProviderRegistry_GetUnknownProvider(t *testing.T) {
	t.Parallel()

	t.Run("should return error when getting an unregistered provider", func(t *testing.T) {
		t.Parallel()

		// given
		registry := repositories.NewProviderRegistry()

		// when
		provider, err := registry.Get("unknown", "token")

		// then
		require.Error(t, err)
		assert.Nil(t, provider)
	})
}

func TestProviderRegistry_GetAuthProvider_UnsupportedType(t *testing.T) {
	t.Parallel()

	t.Run("should return error when service type is unsupported", func(t *testing.T) {
		t.Parallel()

		// given
		registry := repositories.NewProviderRegistry()

		// when
		provider, err := registry.GetAuthProvider(globalEntities.ServiceType(9999), "token")

		// then
		require.Error(t, err)
		assert.Nil(t, provider)
		assert.Contains(t, err.Error(), "unsupported service type")
	})
}
