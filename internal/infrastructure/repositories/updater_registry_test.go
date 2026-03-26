//go:build unit

package repositories_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestUpdaterRegistry_Register(t *testing.T) {
	t.Parallel()

	t.Run("should register and retrieve an updater", func(t *testing.T) {
		t.Parallel()

		// given
		registry := repositories.NewUpdaterRegistry()
		updater := repositorydoubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("terraform").
			BuildSpy()

		// when
		registry.Register(updater)
		result := registry.Get("terraform")

		// then
		require.NotNil(t, result)
		assert.Equal(t, "terraform", result.Name())
	})
}

func TestUpdaterRegistry_Get(t *testing.T) {
	t.Parallel()

	t.Run("should return nil for unregistered updater", func(t *testing.T) {
		t.Parallel()

		// given
		registry := repositories.NewUpdaterRegistry()

		// when
		result := registry.Get("nonexistent")

		// then
		assert.Nil(t, result)
	})
}

func TestUpdaterRegistry_All(t *testing.T) {
	t.Parallel()

	t.Run("should return all registered updaters", func(t *testing.T) {
		t.Parallel()

		// given
		registry := repositories.NewUpdaterRegistry()
		registry.Register(repositorydoubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("terraform").BuildSpy())
		registry.Register(repositorydoubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("golang").BuildSpy())

		// when
		all := registry.All()

		// then
		assert.Len(t, all, 2)
	})
}

func TestUpdaterRegistry_Names(t *testing.T) {
	t.Parallel()

	t.Run("should return all registered updater names", func(t *testing.T) {
		t.Parallel()

		// given
		registry := repositories.NewUpdaterRegistry()
		registry.Register(repositorydoubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("terraform").BuildSpy())
		registry.Register(repositorydoubles.NewSpyUpdaterRepositoryBuilder().
			WithUpdaterName("golang").BuildSpy())

		// when
		names := registry.Names()

		// then
		assert.Len(t, names, 2)
		assert.Contains(t, names, "terraform")
		assert.Contains(t, names, "golang")
	})

	t.Run("should return empty list for empty registry", func(t *testing.T) {
		t.Parallel()

		// given
		registry := repositories.NewUpdaterRegistry()

		// when
		names := registry.Names()

		// then
		assert.Empty(t, names)
	})
}
