package repositories

import (
	"context"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// UpdaterRepository abstracts a dependency ecosystem (Terraform modules, Go modules, etc.).
// Each implementation owns the full cycle: detection, scanning, upgrading, and PR creation.
// This design accommodates fundamentally different workflows — for example Terraform
// updates happen entirely through the provider API, while Go updates require a local clone.
type UpdaterRepository interface {
	// Name returns the updater identifier (e.g. "terraform", "golang").
	Name() string

	// Detect returns true if the given repository uses this dependency ecosystem.
	Detect(ctx context.Context, provider ProviderRepository, repo entities.Repository) bool

	// CreateUpdatePRs scans the repository for outdated dependencies, applies upgrades,
	// and creates pull requests with the changes. It returns the list of PRs created.
	CreateUpdatePRs(
		ctx context.Context,
		provider ProviderRepository,
		repo entities.Repository,
		opts entities.UpdateOptions,
	) ([]entities.PullRequest, error)
}
