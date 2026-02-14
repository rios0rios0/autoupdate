//go:build integration || unit || test

package repositorydoubles //nolint:revive,staticcheck // Test package naming follows established project structure

import (
	"context"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// SpyUpdaterRepository implements repositories.UpdaterRepository as a configurable spy.
type SpyUpdaterRepository struct {
	// --- identity ---
	UpdaterName string

	// --- Detect ---
	DetectResult  bool
	DetectedRepos []entities.Repository

	// --- CreateUpdatePRs ---
	PRs            []entities.PullRequest
	CreatePRsErr   error
	CreatePRsCalls []CreatePRsCall
}

// CreatePRsCall records a single invocation of CreateUpdatePRs.
type CreatePRsCall struct {
	Repo entities.Repository
	Opts entities.UpdateOptions
}

var _ repositories.UpdaterRepository = (*SpyUpdaterRepository)(nil)

func (u *SpyUpdaterRepository) Name() string { return u.UpdaterName }

func (u *SpyUpdaterRepository) Detect(
	_ context.Context, _ repositories.ProviderRepository, repo entities.Repository,
) bool {
	u.DetectedRepos = append(u.DetectedRepos, repo)
	return u.DetectResult
}

func (u *SpyUpdaterRepository) CreateUpdatePRs(
	_ context.Context,
	_ repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
) ([]entities.PullRequest, error) {
	u.CreatePRsCalls = append(u.CreatePRsCalls, CreatePRsCall{Repo: repo, Opts: opts})
	return u.PRs, u.CreatePRsErr
}

// DummyUpdaterRepository is a no-op implementation of repositories.UpdaterRepository.
type DummyUpdaterRepository struct{}

var _ repositories.UpdaterRepository = (*DummyUpdaterRepository)(nil)

func (d *DummyUpdaterRepository) Name() string { return "dummy" }

func (d *DummyUpdaterRepository) Detect(
	_ context.Context, _ repositories.ProviderRepository, _ entities.Repository,
) bool {
	return false
}

func (d *DummyUpdaterRepository) CreateUpdatePRs(
	_ context.Context,
	_ repositories.ProviderRepository,
	_ entities.Repository,
	_ entities.UpdateOptions,
) ([]entities.PullRequest, error) {
	return nil, nil
}
