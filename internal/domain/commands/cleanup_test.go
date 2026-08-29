package commands_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

func TestFilterStaleAggregateBranches(t *testing.T) {
	t.Parallel()

	t.Run("should select every dated branch carrying the aggregate prefix", func(t *testing.T) {
		t.Parallel()

		// given branches from three separate days nobody merged
		branches := []string{
			"main",
			"chore/autoupdate-2026-07-21",
			"chore/autoupdate-2026-07-22",
			"chore/autoupdate-2026-07-23",
			"feat/some-work",
		}

		// when
		stale := commands.FilterStaleAggregateBranches(
			branches, entities.DefaultAggregateBranchPrefix, "main",
		)

		// then
		assert.Equal(t, []string{
			"chore/autoupdate-2026-07-21",
			"chore/autoupdate-2026-07-22",
			"chore/autoupdate-2026-07-23",
		}, stale)
	})

	t.Run("should ignore branches that do not carry the aggregate prefix", func(t *testing.T) {
		t.Parallel()

		// given the other branch families autoupdate and humans create
		branches := []string{
			"main",
			"chore/upgrade-go-deps",
			"chore/upgrade-js-deps",
			"chore/bump-1.0.0",
			"feat/login",
		}

		// when
		stale := commands.FilterStaleAggregateBranches(
			branches, entities.DefaultAggregateBranchPrefix, "main",
		)

		// then nothing outside the aggregate prefix is ever touched
		assert.Empty(t, stale)
	})

	t.Run("should never select the target branch", func(t *testing.T) {
		t.Parallel()

		// given a target branch that would otherwise match the prefix
		branches := []string{"chore/autoupdate-main", "chore/autoupdate-2026-07-21"}

		// when
		stale := commands.FilterStaleAggregateBranches(
			branches, entities.DefaultAggregateBranchPrefix, "chore/autoupdate-main",
		)

		// then
		assert.Equal(t, []string{"chore/autoupdate-2026-07-21"}, stale)
	})

	t.Run("should select today's branch so it is recreated fresh", func(t *testing.T) {
		t.Parallel()

		// given today's leftover branch from a run whose pull request never opened
		today := commands.BuildAggregateBranchName(
			entities.DefaultAggregateBranchPrefix, time.Now(),
		)
		branches := []string{"main", today}

		// when
		stale := commands.FilterStaleAggregateBranches(
			branches, entities.DefaultAggregateBranchPrefix, "main",
		)

		// then it is removed, because the run recreates it immediately afterwards
		assert.Equal(t, []string{today}, stale)
	})

	t.Run("should sort the result so the cleanup order is deterministic", func(t *testing.T) {
		t.Parallel()

		// given
		branches := []string{
			"chore/autoupdate-2026-07-23",
			"chore/autoupdate-2026-07-21",
			"chore/autoupdate-2026-07-22",
		}

		// when
		stale := commands.FilterStaleAggregateBranches(
			branches, entities.DefaultAggregateBranchPrefix, "main",
		)

		// then
		assert.Equal(t, []string{
			"chore/autoupdate-2026-07-21",
			"chore/autoupdate-2026-07-22",
			"chore/autoupdate-2026-07-23",
		}, stale)
	})

	t.Run("should honour a custom aggregate prefix", func(t *testing.T) {
		t.Parallel()

		// given
		branches := []string{"main", "chore/autoupdate-2026-07-21", "bot/refresh-2026-07-21"}

		// when
		stale := commands.FilterStaleAggregateBranches(branches, "bot/refresh-", "main")

		// then
		assert.Equal(t, []string{"bot/refresh-2026-07-21"}, stale)
	})

	t.Run("should return an empty result when there are no branches", func(t *testing.T) {
		t.Parallel()

		// given
		var branches []string

		// when
		stale := commands.FilterStaleAggregateBranches(
			branches, entities.DefaultAggregateBranchPrefix, "main",
		)

		// then
		assert.Empty(t, stale)
	})
}

func TestCleanupEnabled(t *testing.T) {
	t.Parallel()

	t.Run("should be enabled when the setting is absent", func(t *testing.T) {
		t.Parallel()

		// given cleanup is opt-out, so an untouched config leaves it on
		settings := &entities.Settings{}

		// when
		enabled := entities.CleanupEnabled(settings)

		// then
		assert.True(t, enabled)
	})

	t.Run("should be disabled when explicitly turned off", func(t *testing.T) {
		t.Parallel()

		// given
		disabled := false
		settings := &entities.Settings{CleanupStaleBranches: &disabled}

		// when
		enabled := entities.CleanupEnabled(settings)

		// then
		assert.False(t, enabled)
	})

	t.Run("should be enabled when the settings are missing entirely", func(t *testing.T) {
		t.Parallel()

		// given
		var settings *entities.Settings

		// when
		enabled := entities.CleanupEnabled(settings)

		// then
		assert.True(t, enabled)
	})
}

func TestResolveAggregateBranchPrefix(t *testing.T) {
	t.Parallel()

	t.Run("should return the default prefix when none is configured", func(t *testing.T) {
		t.Parallel()

		// when
		prefix := entities.ResolveAggregateBranchPrefix(&entities.Settings{})

		// then
		assert.Equal(t, "chore/autoupdate-", prefix)
	})

	t.Run("should return the configured prefix", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{AggregateBranchPrefix: "bot/refresh-"}

		// when
		prefix := entities.ResolveAggregateBranchPrefix(settings)

		// then
		assert.Equal(t, "bot/refresh-", prefix)
	})

	t.Run("should fall back to the default when the configured prefix is blank", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{AggregateBranchPrefix: "   "}

		// when
		prefix := entities.ResolveAggregateBranchPrefix(settings)

		// then
		assert.Equal(t, entities.DefaultAggregateBranchPrefix, prefix)
	})

	t.Run("should drive the branch a run creates, not just cleanup", func(t *testing.T) {
		t.Parallel()

		// given a custom prefix
		settings := &entities.Settings{AggregateBranchPrefix: "bot/refresh-"}
		timestamp := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)

		// when the branch name is built from the resolved prefix
		branch := commands.BuildAggregateBranchName(
			entities.ResolveAggregateBranchPrefix(settings), timestamp,
		)

		// then creation and cleanup can never look at different branches
		assert.Equal(t, "bot/refresh-2026-07-23", branch)
		assert.Equal(t, []string{branch}, commands.FilterStaleAggregateBranches(
			[]string{"main", branch}, entities.ResolveAggregateBranchPrefix(settings), "main",
		))
	})
}
