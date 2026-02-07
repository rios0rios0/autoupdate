package provider_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/domain"
	"github.com/rios0rios0/autoupdate/infrastructure/provider"
	testdoubles "github.com/rios0rios0/autoupdate/test"
)

func TestProviderRegistry(t *testing.T) {
	t.Parallel()

	t.Run("should register and retrieve a provider by name", func(t *testing.T) {
		t.Parallel()

		// given
		reg := provider.NewRegistry()
		factory := func(_ string) domain.Provider {
			return &testdoubles.SpyProvider{ProviderName: "test-provider"}
		}
		reg.Register("test-provider", factory)

		// when
		prov, err := reg.Get("test-provider", "fake-token")

		// then
		require.NoError(t, err)
		assert.NotNil(t, prov)
		assert.Equal(t, "test-provider", prov.Name())
	})

	t.Run("should return error for unknown provider", func(t *testing.T) {
		t.Parallel()

		// given
		reg := provider.NewRegistry()

		// when
		prov, err := reg.Get("nonexistent", "token")

		// then
		require.Error(t, err)
		assert.Nil(t, prov)
		assert.Contains(t, err.Error(), "unknown provider type")
	})

	t.Run("should list registered provider names", func(t *testing.T) {
		t.Parallel()

		// given
		reg := provider.NewRegistry()
		reg.Register("github", func(_ string) domain.Provider {
			return &testdoubles.SpyProvider{ProviderName: "github"}
		})
		reg.Register("gitlab", func(_ string) domain.Provider {
			return &testdoubles.SpyProvider{ProviderName: "gitlab"}
		})

		// when
		names := reg.Names()

		// then
		assert.Len(t, names, 2)
		assert.ElementsMatch(t, []string{"github", "gitlab"}, names)
	})

	t.Run("should pass token to factory function", func(t *testing.T) {
		t.Parallel()

		// given
		var receivedToken string
		reg := provider.NewRegistry()
		reg.Register("custom", func(token string) domain.Provider {
			receivedToken = token
			return &testdoubles.SpyProvider{ProviderName: "custom", Token: token}
		})

		// when
		_, err := reg.Get("custom", "my-secret-token")

		// then
		require.NoError(t, err)
		assert.Equal(t, "my-secret-token", receivedToken)
	})

	t.Run("should return empty names for empty registry", func(t *testing.T) {
		t.Parallel()

		// given
		reg := provider.NewRegistry()

		// when
		names := reg.Names()

		// then
		assert.Empty(t, names)
	})
}
