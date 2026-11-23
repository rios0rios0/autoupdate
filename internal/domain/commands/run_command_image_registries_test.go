package commands_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	domainRepos "github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

// TestRunCommandHandsTheImageRegistriesToTheUpdaters is not parallel: it points HOME at an
// empty directory with t.Setenv, for the reason TestRunCommandLeavesNothingBehind gives.
func TestRunCommandHandsTheImageRegistriesToTheUpdaters(t *testing.T) {
	t.Run("should give an updater the operator's image_registries rules", func(t *testing.T) {
		// given
		t.Setenv("HOME", t.TempDir())
		rules := []entities.ImageRegistryRule{{
			Registry: "registry.example.com",
			Paths:    []string{"environments/**"},
		}}

		var (
			mu       sync.Mutex
			received [][]entities.ImageRegistryRule
		)
		updater := repositorydoubles.NewSpyLocalUpdaterRepositoryBuilder().
			WithUpdaterName("terraform").
			WithDetectResult(true).
			WithApplyUpdateFn(func(_ string, opts entities.UpdateOptions) (*domainRepos.LocalUpdateResult, error) {
				mu.Lock()
				defer mu.Unlock()
				received = append(received, opts.ImageRegistries)
				return nil, domainRepos.ErrNoUpdatesNeeded
			}).
			BuildSpy()
		cmd := newLocalRunCommandOfMany([]string{newBareRemoteWithBranches(t)}, updater)
		settings := localRunSettings()
		settings.ImageRegistries = rules

		// when
		err := cmd.Execute(context.Background(), settings, commands.RunOptions{})

		// then
		require.NoError(t, err)
		require.Len(t, received, 1, "the updater should have run against the clone")
		assert.Equal(t, rules, received[0])
	})
}
