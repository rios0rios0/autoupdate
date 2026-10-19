package java_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
	javaUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/java"
	"github.com/rios0rios0/autoupdate/test/infrastructure/gitrepo"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

// overlapRunner stands in for a Gradle or Maven build: it holds every build open
// until the test lets it finish, and records how many were running at once.
type overlapRunner struct {
	mu      sync.Mutex
	running int
	peak    int
	started chan struct{}
	finish  chan struct{}
}

func (r *overlapRunner) Run(
	_ context.Context, _ string, _ []string, _ cmdrunner.RunOptions,
) (*cmdrunner.RunResult, error) {
	r.mu.Lock()
	r.running++
	r.peak = max(r.peak, r.running)
	r.mu.Unlock()

	r.started <- struct{}{}
	<-r.finish

	r.mu.Lock()
	r.running--
	r.mu.Unlock()
	return &cmdrunner.RunResult{}, nil
}

func TestApplyUpdatesBuildSlots(t *testing.T) {
	t.Parallel()

	t.Run("should run the Gradle or Maven builds of a run one at a time", func(t *testing.T) {
		t.Parallel()

		// given three Gradle repositories a batch run processes at once
		runner := &overlapRunner{started: make(chan struct{}), finish: make(chan struct{})}
		updater := javaUpdater.NewUpdaterRepositoryForTest(
			&repositorydoubles.StubVersionFetcher{Version: "21.0.5"}, runner,
		)
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy()

		var group sync.WaitGroup
		for range 3 {
			repoDir := gitrepo.New(t, map[string]string{"build.gradle": "plugins { id 'java' }\n"})
			group.Go(func() {
				_, _ = updater.ApplyUpdates(t.Context(), repoDir, provider,
					entities.Repository{Organization: "org", Name: "repo"}, entities.UpdateOptions{})
			})
		}

		// when every build is let finish once it has started
		for range 3 {
			<-runner.started
			runner.finish <- struct{}{}
		}
		group.Wait()

		// then no two builds ever ran side by side
		assert.Equal(t, 1, runner.peak)
	})
}
