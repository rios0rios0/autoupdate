package support_test

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
	"github.com/rios0rios0/autoupdate/test/infrastructure/overlap"
)

// heldOpen is how long a test keeps the builds it lets start running while the others
// wait for their turn. Slots that work never let one more start, however long this
// is; it only bounds how quickly a missing slot shows up as a failure.
const heldOpen = 200 * time.Millisecond

// slotTestRepo is the repository every build in these tests is for.
var slotTestRepo = entities.Repository{Organization: "org", Name: "repo"} //nolint:gochecknoglobals // test fixture

func TestBuildSlotsAcquire(t *testing.T) {
	t.Parallel()

	t.Run("should never let more builds run at once than it has slots", func(t *testing.T) {
		t.Parallel()

		// given two slots, and five builds that each take one around their work
		const builds = 5
		slots := support.NewBuildSlots("test", 2)
		recorder := overlap.NewRecorder(builds)
		var group sync.WaitGroup
		for range builds {
			group.Go(func() {
				freeSlot, err := slots.Acquire(t.Context(), "test", slotTestRepo)
				if err != nil {
					t.Error(err)
					return
				}
				defer freeSlot()
				recorder.Hold()
			})
		}

		// when the first two to start are kept running while the others wait
		recorder.AwaitStarts(2)
		assert.False(t, recorder.AnotherStartsWithin(heldOpen),
			"a third build started while both slots were taken")
		recorder.Release()
		group.Wait()

		// then every build ran, and never more than two side by side
		assert.Equal(t, builds, recorder.Runs())
		assert.Equal(t, 2, recorder.Peak())
	})

	t.Run("should free a slot once, however often it is given back", func(t *testing.T) {
		t.Parallel()

		// given the only slot, taken and then given back twice
		slots := support.NewBuildSlots("test", 1)
		freeSlot, err := slots.Acquire(t.Context(), "test", slotTestRepo)
		require.NoError(t, err)
		freeSlot()
		freeSlot()

		// when two more builds ask for it, the second once the run has stopped waiting
		_, firstErr := slots.Acquire(t.Context(), "test", slotTestRepo)
		ctx, cancel := context.WithTimeout(t.Context(), heldOpen)
		defer cancel()
		_, secondErr := slots.Acquire(ctx, "test", slotTestRepo)

		// then the first gets the slot back, and the second finds it taken
		require.NoError(t, firstErr)
		require.ErrorIs(t, secondErr, context.DeadlineExceeded)
	})

	t.Run("should stop waiting for a slot when the run is cancelled", func(t *testing.T) {
		t.Parallel()

		// given the only slot, held by another repository's build
		slots := support.NewBuildSlots("test", 1)
		freeSlot, err := slots.Acquire(t.Context(), "test", slotTestRepo)
		require.NoError(t, err)
		defer freeSlot()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		// when another build asks for one after the run was cancelled
		_, err = slots.Acquire(ctx, "test", slotTestRepo)

		// then it gives up rather than waiting, and says which kind of build it was
		require.ErrorIs(t, err, context.Canceled)
		assert.ErrorContains(t, err, "test build slot")
	})

	t.Run("should limit nothing when there is no pool", func(t *testing.T) {
		t.Parallel()

		// given no pool at all
		var slots *support.BuildSlots

		// when a build asks for a slot
		freeSlot, err := slots.Acquire(t.Context(), "test", slotTestRepo)

		// then it gets one at once, and giving it back does nothing
		require.NoError(t, err)
		freeSlot()
		assert.Zero(t, slots.Size())
	})
}

func TestNewBuildSlots(t *testing.T) {
	t.Parallel()

	t.Run("should never have fewer than one slot", func(t *testing.T) {
		t.Parallel()

		// given / when
		slots := support.NewBuildSlots("test", 0)

		// then
		assert.Equal(t, 1, slots.Size())
	})
}

func TestBuildSlotsFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		memory uint64
		cpus   int
		want   int
	}{
		{name: "should allow one build on a small CI agent", memory: 6 * gibibyte, cpus: 2, want: 1},
		{name: "should allow one build per share of memory", memory: 16 * gibibyte, cpus: 8, want: 4},
		{name: "should allow no more builds than CPUs", memory: 64 * gibibyte, cpus: 4, want: 4},
		{name: "should allow one build when the memory holds less than one", memory: gibibyte, cpus: 8, want: 1},
		{name: "should allow one build when no CPU is reported", memory: 64 * gibibyte, cpus: 0, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// given a machine of the case's memory and CPUs, and 4 GiB a build

			// when
			got := support.BuildSlotsFor(tt.memory, tt.cpus, 4*gibibyte)

			// then
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("should allow one build per CPU when a build needs no memory of its own", func(t *testing.T) {
		t.Parallel()

		// given / when
		got := support.BuildSlotsFor(gibibyte, 3, 0)

		// then
		assert.Equal(t, 3, got)
	})
}

func TestBuildSlotsForMachine(t *testing.T) {
	t.Parallel()

	t.Run("should allow at least one build and no more than the machine has CPUs", func(t *testing.T) {
		t.Parallel()

		// given / when
		got := support.BuildSlotsForMachine(4 * gibibyte)

		// then
		assert.GreaterOrEqual(t, got, 1)
		assert.LessOrEqual(t, got, max(1, runtime.NumCPU()))
	})
}
