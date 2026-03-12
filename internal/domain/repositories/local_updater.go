package repositories

import (
	"context"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// LocalUpdater is an optional interface that UpdaterRepository implementations
// can satisfy to participate in the centralized clone-based pipeline.
//
// When an updater implements LocalUpdater, the RunCommand clones the repo once
// and calls ApplyUpdates on the local filesystem, then handles branch creation,
// signed commits, push, and PR creation centrally.
//
// Updaters that do NOT implement LocalUpdater fall back to the legacy
// CreateUpdatePRs flow.
type LocalUpdater interface {
	// ApplyUpdates modifies files in the cloned repository on disk.
	// The provider is still available for operations that require the remote
	// API (e.g. tag resolution, version checking).
	// Returns nil result if no changes were needed.
	ApplyUpdates(
		ctx context.Context,
		repoDir string,
		provider ProviderRepository,
		repo entities.Repository,
		opts entities.UpdateOptions,
	) (*LocalUpdateResult, error)
}

// LocalUpdateResult describes the outcome of a local update operation.
type LocalUpdateResult struct {
	BranchName    string
	CommitMessage string
	PRTitle       string
	PRDescription string
}
