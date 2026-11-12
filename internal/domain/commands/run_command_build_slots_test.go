package commands_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	domainRepos "github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/support"
	"github.com/rios0rios0/autoupdate/test/infrastructure/overlap"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

// queuedLongEnough is how long a test lets a repository settle into its wait for a
// build slot before it acts. A run that orders the slot and the cache correctly passes
// however long this is; it only bounds how quickly the wrong order shows up.
const queuedLongEnough = 200 * time.Millisecond

// buildLimitedUpdater is a local updater whose builds a run must limit, the way the Go
// and Java updaters are: every repository it is applied to takes one of its slots.
type buildLimitedUpdater struct {
	*repositorydoubles.SpyLocalUpdaterRepository

	slots *support.BuildSlots
}

func (u *buildLimitedUpdater) AcquireBuildSlot(ctx context.Context, repo entities.Repository) (func(), error) {
	return u.slots.Acquire(ctx, u.Name(), repo)
}

// newBuildLimitedUpdater returns a limited updater with one slot, applying apply.
func newBuildLimitedUpdater(
	apply func(string, entities.UpdateOptions) (*domainRepos.LocalUpdateResult, error),
) *buildLimitedUpdater {
	spy := repositorydoubles.NewSpyLocalUpdaterRepositoryBuilder().
		WithUpdaterName("golang").
		WithDetectResult(true).
		WithApplyUpdateFn(apply).
		BuildSpy()
	return &buildLimitedUpdater{SpyLocalUpdaterRepository: spy, slots: support.NewBuildSlots("Go", 1)}
}

// applyLimited applies updater to one repository the way the run does.
func applyLimited(
	ctx context.Context, t *testing.T, cache *support.SharedCache, updater *buildLimitedUpdater,
) error {
	t.Helper()

	_, err := commands.ApplyLocalUpdater(ctx, cache, updater, updater, t.TempDir(),
		repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy(),
		entities.Repository{Organization: "org", Name: "repo"}, entities.UpdateOptions{})
	return err
}

// newRunCache creates the cache a run shares between its repositories, removed when
// the test ends whatever the test did to it.
func newRunCache(t *testing.T) *support.SharedCache {
	t.Helper()

	cache, err := support.NewSharedCache()
	require.NoError(t, err)
	t.Cleanup(func() { _ = cache.Remove() })
	return cache
}

func TestApplyLocalUpdater(t *testing.T) {
	t.Parallel()

	t.Run("should not hold the shared cache while the repository waits for a build slot", func(t *testing.T) {
		t.Parallel()

		// given a repository queued for the only build slot, which another one holds
		cache := newRunCache(t)
		updater := newBuildLimitedUpdater(func(string, entities.UpdateOptions) (*domainRepos.LocalUpdateResult, error) {
			return nil, domainRepos.ErrNoUpdatesNeeded
		})
		freeHeldSlot, err := updater.AcquireBuildSlot(t.Context(), entities.Repository{})
		require.NoError(t, err)
		applied := make(chan error, 1)
		go func() { applied <- applyLimited(t.Context(), t, cache, updater) }()
		time.Sleep(queuedLongEnough)

		// when the cache is taken for itself while that repository waits for its turn,
		// the way Trim takes it to empty it when the disk runs low
		taken := make(chan struct{})
		go func() {
			_ = cache.Remove()
			close(taken)
		}()

		// then nothing waits for a repository that is only queued, and the repository
		// is applied once the slot is free
		select {
		case <-taken:
		case <-time.After(10 * queuedLongEnough):
			t.Fatal("the shared cache was held by a repository that only waited for a build slot")
		}
		freeHeldSlot()
		require.ErrorIs(t, <-applied, domainRepos.ErrNoUpdatesNeeded)
		assert.Equal(t, 1, updater.ApplyCallCount)
	})

	t.Run("should give the build slot back when the updater fails", func(t *testing.T) {
		t.Parallel()

		// given an updater that fails on the only build slot
		cache := newRunCache(t)
		failure := errors.New("the package manager crashed")
		updater := newBuildLimitedUpdater(func(string, entities.UpdateOptions) (*domainRepos.LocalUpdateResult, error) {
			return nil, failure
		})

		// when it is applied to one repository after another
		firstErr := applyLimited(t.Context(), t, cache, updater)
		ctx, cancel := context.WithTimeout(t.Context(), 10*queuedLongEnough)
		defer cancel()
		secondErr := applyLimited(ctx, t, cache, updater)

		// then the second gets the slot the first gave back
		require.ErrorIs(t, firstErr, failure)
		require.ErrorIs(t, secondErr, failure)
		assert.Equal(t, 2, updater.ApplyCallCount)
	})

	t.Run("should not apply the updater when the run is cancelled while it waits", func(t *testing.T) {
		t.Parallel()

		// given the only build slot, held by another repository, and a cancelled run
		cache := newRunCache(t)
		updater := newBuildLimitedUpdater(nil)
		freeHeldSlot, err := updater.AcquireBuildSlot(t.Context(), entities.Repository{})
		require.NoError(t, err)
		defer freeHeldSlot()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		// when
		err = applyLimited(ctx, t, cache, updater)

		// then
		require.ErrorIs(t, err, context.Canceled)
		assert.Zero(t, updater.ApplyCallCount)
	})
}

// TestRunCommandLimitsBuilds is not parallel: it points TMPDIR and HOME at
// directories of its own, for the reasons TestRunCommandSharesTheGoCaches gives.
func TestRunCommandLimitsBuilds(t *testing.T) {
	t.Run(
		"should never apply a build-limited updater to more repositories at once than it has slots",
		func(t *testing.T) {
			// given three repositories processed at once, and an updater with one build slot
			// whose every build is held open until the test lets it finish
			const repositories = 3
			remoteDirs := make([]string, 0, repositories)
			for range repositories {
				remoteDirs = append(remoteDirs, newBareRemoteWithBranches(t))
			}
			t.Setenv("TMPDIR", t.TempDir())
			t.Setenv("HOME", t.TempDir())
			recorder := overlap.NewRecorder(repositories)
			updater := newBuildLimitedUpdater(
				func(string, entities.UpdateOptions) (*domainRepos.LocalUpdateResult, error) {
					recorder.Hold()
					return nil, domainRepos.ErrNoUpdatesNeeded
				},
			)
			settings := localRunSettings()
			settings.Concurrency = repositories
			cmd := newLocalRunCommandOfMany(remoteDirs, updater)

			// when the first repository's build is kept running while the others reach theirs
			var run sync.WaitGroup
			var runErr error
			run.Go(func() { runErr = cmd.Execute(t.Context(), settings, commands.RunOptions{}) })
			recorder.AwaitStarts(1)
			secondStarted := recorder.AnotherStartsWithin(queuedLongEnough)
			recorder.Release()
			run.Wait()

			// then every repository was applied, and never two side by side
			require.NoError(t, runErr)
			assert.False(t, secondStarted, "a second build started while the first held the only slot")
			assert.Equal(t, repositories, recorder.Runs())
			assert.Equal(t, 1, recorder.Peak())
		},
	)
}
