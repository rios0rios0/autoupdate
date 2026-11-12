package java_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
	javaUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/java"
	"github.com/rios0rios0/autoupdate/test/infrastructure/gitrepo"
	"github.com/rios0rios0/autoupdate/test/infrastructure/overlap"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

// firstBuildHeldOpen is how long the test keeps the first build running while the
// others reach their turn. A slot that works never lets a second build start, however
// long this is; it only bounds how quickly a missing slot shows up as a failure.
const firstBuildHeldOpen = 200 * time.Millisecond

// overlapRunner stands in for a Gradle or Maven build: it holds every build open
// until the test releases them, and records how many ran and how many at once.
type overlapRunner struct {
	*overlap.Recorder
}

func (r overlapRunner) Run(
	_ context.Context, _ string, _ []string, _ cmdrunner.RunOptions,
) (*cmdrunner.RunResult, error) {
	r.Hold()
	return &cmdrunner.RunResult{}, nil
}

func TestApplyUpdatesBuildSlots(t *testing.T) {
	t.Parallel()

	t.Run("should hold every other Gradle or Maven build back while one is running", func(t *testing.T) {
		t.Parallel()

		// given three Gradle repositories a batch run processes at once, each taking its
		// build slot the way the run does before applying the updater
		const repositories = 3
		runner := overlapRunner{Recorder: overlap.NewRecorder(repositories)}
		updater := javaUpdater.NewUpdaterRepositoryForTest(
			&repositorydoubles.StubVersionFetcher{Version: "21.0.5"}, runner,
		)
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		var group sync.WaitGroup
		for range repositories {
			repoDir := gitrepo.New(t, map[string]string{"build.gradle": "plugins { id 'java' }\n"})
			group.Go(func() {
				freeSlot, err := updater.AcquireBuildSlot(t.Context(), repo)
				if err != nil {
					t.Error(err)
					return
				}
				defer freeSlot()
				_, _ = updater.ApplyUpdates(t.Context(), repoDir, provider, repo, entities.UpdateOptions{})
			})
		}

		// when the first build to start is kept running while the others reach their turn
		runner.AwaitStarts(1)
		assert.False(t, runner.AnotherStartsWithin(firstBuildHeldOpen),
			"a second Gradle or Maven build started while the first was still running")
		runner.Release()
		group.Wait()

		// then every build ran, and never two of them side by side
		assert.Equal(t, repositories, runner.Runs())
		assert.Equal(t, 1, runner.Peak())
	})
}
