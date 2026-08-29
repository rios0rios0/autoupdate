package dart_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	dartUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/dart"
)

func TestFvmPinIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	t.Run("should report no SDK upgrade when the pin is ahead of the stable channel", func(t *testing.T) {
		t.Parallel()

		// given — a project deliberately on a newer Flutter than stable
		const pinned, stable = "3.30.0", "3.27.1"

		// when
		vCtx := dartUpdater.NewVersionContext("flutter", stable, pinned)

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-dart-deps", vCtx.BranchName)
	})

	t.Run("should report an SDK upgrade when the pin is behind the stable channel", func(t *testing.T) {
		t.Parallel()

		// given
		const pinned, stable = "3.24.0", "3.27.1"

		// when
		vCtx := dartUpdater.NewVersionContext("flutter", stable, pinned)

		// then
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-flutter-3.27.1", vCtx.BranchName)
	})

	t.Run("should report no SDK upgrade when the pin already names the stable release", func(t *testing.T) {
		t.Parallel()

		// given
		const pinned, stable = "3.27.1", "3.27.1"

		// when
		vCtx := dartUpdater.NewVersionContext("flutter", stable, pinned)

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
	})
}
