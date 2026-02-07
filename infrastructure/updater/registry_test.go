package updater_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/infrastructure/updater"
	testdoubles "github.com/rios0rios0/autoupdate/test"
)

func TestUpdaterRegistry(t *testing.T) {
	t.Parallel()

	t.Run("should register and retrieve an updater by name", func(t *testing.T) {
		t.Parallel()

		// given
		reg := updater.NewRegistry()
		stub := &testdoubles.SpyUpdater{UpdaterName: "test-updater"}
		reg.Register(stub)

		// when
		u := reg.Get("test-updater")

		// then
		assert.NotNil(t, u)
		assert.Equal(t, "test-updater", u.Name())
	})

	t.Run("should return nil for unknown updater", func(t *testing.T) {
		t.Parallel()

		// given
		reg := updater.NewRegistry()

		// when
		u := reg.Get("nonexistent")

		// then
		assert.Nil(t, u)
	})

	t.Run("should list all registered updaters", func(t *testing.T) {
		t.Parallel()

		// given
		reg := updater.NewRegistry()
		reg.Register(&testdoubles.SpyUpdater{UpdaterName: "terraform"})
		reg.Register(&testdoubles.SpyUpdater{UpdaterName: "golang"})

		// when
		all := reg.All()

		// then
		assert.Len(t, all, 2)
	})

	t.Run("should list registered updater names", func(t *testing.T) {
		t.Parallel()

		// given
		reg := updater.NewRegistry()
		reg.Register(&testdoubles.SpyUpdater{UpdaterName: "terraform"})
		reg.Register(&testdoubles.SpyUpdater{UpdaterName: "golang"})

		// when
		names := reg.Names()

		// then
		assert.Len(t, names, 2)
		assert.ElementsMatch(t, []string{"terraform", "golang"}, names)
	})

	t.Run("should return empty lists for empty registry", func(t *testing.T) {
		t.Parallel()

		// given
		reg := updater.NewRegistry()

		// when
		all := reg.All()
		names := reg.Names()

		// then
		assert.Empty(t, all)
		assert.Empty(t, names)
	})

	t.Run("should overwrite updater with same name", func(t *testing.T) {
		t.Parallel()

		// given
		reg := updater.NewRegistry()
		reg.Register(&testdoubles.SpyUpdater{UpdaterName: "terraform"})
		reg.Register(&testdoubles.SpyUpdater{UpdaterName: "terraform"})

		// when
		all := reg.All()

		// then
		assert.Len(t, all, 1)
	})
}
