//go:build integration || unit || test

package repositorydoubles //nolint:revive,staticcheck // Test package naming follows established project structure

import (
	"context"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// SpyLocalUpdaterRepository implements both repositories.UpdaterRepository
// and repositories.LocalUpdater. ApplyUpdateFn is invoked on each call,
// allowing tests to write files to repoDir, simulate failures, and return
// custom LocalUpdateResult values.
type SpyLocalUpdaterRepository struct {
	// --- identity ---
	UpdaterName string

	// --- Detect ---
	DetectResult  bool
	DetectedRepos []entities.Repository

	// --- ApplyUpdates ---
	// ApplyUpdateFn receives repoDir and returns what ApplyUpdates should
	// yield for this invocation. Tests use this to mutate the worktree and
	// control the result/error.
	ApplyUpdateFn  func(repoDir string) (*repositories.LocalUpdateResult, error)
	ApplyCallCount int
}

var (
	_ repositories.UpdaterRepository = (*SpyLocalUpdaterRepository)(nil)
	_ repositories.LocalUpdater      = (*SpyLocalUpdaterRepository)(nil)
)

// Name returns the configured updater name.
func (u *SpyLocalUpdaterRepository) Name() string { return u.UpdaterName }

// Detect records the repository and returns the configured result.
func (u *SpyLocalUpdaterRepository) Detect(
	_ context.Context, _ repositories.ProviderRepository, repo entities.Repository,
) bool {
	u.DetectedRepos = append(u.DetectedRepos, repo)
	return u.DetectResult
}

// CreateUpdatePRs is unused — local updaters go through ApplyUpdates in
// the aggregate pipeline. It is implemented only to satisfy the
// UpdaterRepository interface.
func (u *SpyLocalUpdaterRepository) CreateUpdatePRs(
	_ context.Context,
	_ repositories.ProviderRepository,
	_ entities.Repository,
	_ entities.UpdateOptions,
) ([]entities.PullRequest, error) {
	return nil, nil
}

// ApplyUpdates dispatches to ApplyUpdateFn when set, otherwise reports
// no updates needed. Tests typically inject ApplyUpdateFn to mutate the
// worktree and return a fixed LocalUpdateResult.
func (u *SpyLocalUpdaterRepository) ApplyUpdates(
	_ context.Context,
	repoDir string,
	_ repositories.ProviderRepository,
	_ entities.Repository,
	_ entities.UpdateOptions,
) (*repositories.LocalUpdateResult, error) {
	u.ApplyCallCount++
	if u.ApplyUpdateFn != nil {
		return u.ApplyUpdateFn(repoDir)
	}
	return nil, repositories.ErrNoUpdatesNeeded
}
