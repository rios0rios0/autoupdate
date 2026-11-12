package golang_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
	"github.com/rios0rios0/autoupdate/internal/support"
)

func TestGoBuildSlots(t *testing.T) {
	t.Parallel()

	t.Run("should let a run start only as many Go builds at once as the machine holds", func(t *testing.T) {
		t.Parallel()

		// given / when
		updater := golang.NewUpdaterRepository()

		// then the run limits its builds, by the memory the machine leaves each of them
		limiter, limited := updater.(repositories.BuildLimiter)
		require.True(t, limited, "the run applies the Go updater without limiting its builds")
		assert.Equal(t, support.BuildSlotsForMachine(golang.GoBuildMemory), golang.BuildSlotCount(updater))
		freeSlot, err := limiter.AcquireBuildSlot(t.Context(), entities.Repository{Organization: "org", Name: "repo"})
		require.NoError(t, err, "a free slot should be granted at once")
		freeSlot()
	})
}
