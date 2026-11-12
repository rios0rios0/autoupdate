package repositories

import (
	"context"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// BuildLimiter is an optional interface for a LocalUpdater whose ApplyUpdates runs
// builds so heavy that a machine holds only a few of them at once -- the Gradle and
// Maven builds of the Java updater, the type-checks of the Go updater.
//
// A batch run processes several repositories at once, so before it applies such an
// updater to a repository it asks for a slot, and it frees the slot once ApplyUpdates
// has returned. It asks before it takes the run's shared cache, so a repository waiting
// for its turn holds nothing the rest of the run needs.
type BuildLimiter interface {
	// AcquireBuildSlot waits until the run may start one more of the updater's builds
	// for repo, and returns the function that frees the slot again. It fails only when
	// ctx ends while it waits.
	AcquireBuildSlot(ctx context.Context, repo entities.Repository) (func(), error)
}
