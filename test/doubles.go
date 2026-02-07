// Package testdoubles provides test doubles (spies, stubs, dummies) for domain
// interfaces. These are hand-crafted implementations — no mock frameworks.
package testdoubles

import (
	"context"
	"fmt"

	"github.com/rios0rios0/autoupdate/domain"
)

// ---------------------------------------------------------------------------
// SpyProvider
// ---------------------------------------------------------------------------

// SpyProvider implements domain.Provider as a configurable spy.
// Configure the response fields for the methods your test exercises,
// then inspect the call-tracking fields to verify behavior.
type SpyProvider struct {
	// --- identity ---
	ProviderName string
	Token        string

	// --- DiscoverRepositories ---
	Repositories []domain.Repository
	DiscoverErr  error
	// spy: orgs that were requested
	DiscoveredOrgs []string

	// --- GetFileContent ---
	FileContents   map[string]string // path -> content
	FileContentErr error

	// --- ListFiles ---
	Files       []domain.File
	ListFileErr error

	// --- GetTags ---
	Tags       []string
	GetTagsErr error

	// --- HasFile ---
	ExistingFiles map[string]bool // path -> exists

	// --- CreateBranchWithChanges ---
	CreateBranchErr error
	// spy: inputs received
	BranchInputs []domain.BranchInput

	// --- CreatePullRequest ---
	CreatedPR   *domain.PullRequest
	CreatePRErr error
	// spy: inputs received
	PRInputs []domain.PullRequestInput

	// --- PullRequestExists ---
	PRExistsResult bool
	PRExistsErr    error
	// spy: branch names checked
	PRExistsBranches []string
}

var _ domain.Provider = (*SpyProvider)(nil)

func (p *SpyProvider) Name() string { return p.ProviderName }

func (p *SpyProvider) AuthToken() string { return p.Token }

func (p *SpyProvider) MatchesURL(_ string) bool { return false }

func (p *SpyProvider) DiscoverRepositories(
	_ context.Context,
	org string,
) ([]domain.Repository, error) {
	p.DiscoveredOrgs = append(p.DiscoveredOrgs, org)
	return p.Repositories, p.DiscoverErr
}

func (p *SpyProvider) GetFileContent(
	_ context.Context,
	_ domain.Repository,
	path string,
) (string, error) {
	if p.FileContents != nil {
		if content, ok := p.FileContents[path]; ok {
			return content, nil
		}
	}
	if p.FileContentErr != nil {
		return "", p.FileContentErr
	}
	return "", fmt.Errorf("file not found: %s", path)
}

func (p *SpyProvider) ListFiles(
	_ context.Context,
	_ domain.Repository,
	_ string,
) ([]domain.File, error) {
	return p.Files, p.ListFileErr
}

func (p *SpyProvider) GetTags(
	_ context.Context,
	_ domain.Repository,
) ([]string, error) {
	return p.Tags, p.GetTagsErr
}

func (p *SpyProvider) HasFile(
	_ context.Context,
	_ domain.Repository,
	path string,
) bool {
	if p.ExistingFiles != nil {
		return p.ExistingFiles[path]
	}
	return false
}

func (p *SpyProvider) CreateBranchWithChanges(
	_ context.Context,
	_ domain.Repository,
	input domain.BranchInput,
) error {
	p.BranchInputs = append(p.BranchInputs, input)
	return p.CreateBranchErr
}

func (p *SpyProvider) CreatePullRequest(
	_ context.Context,
	_ domain.Repository,
	input domain.PullRequestInput,
) (*domain.PullRequest, error) {
	p.PRInputs = append(p.PRInputs, input)
	if p.CreatePRErr != nil {
		return nil, p.CreatePRErr
	}
	if p.CreatedPR != nil {
		return p.CreatedPR, nil
	}
	return &domain.PullRequest{
		ID:    1,
		Title: input.Title,
		URL:   "https://example.com/pr/1",
	}, nil
}

func (p *SpyProvider) PullRequestExists(
	_ context.Context,
	_ domain.Repository,
	branch string,
) (bool, error) {
	p.PRExistsBranches = append(p.PRExistsBranches, branch)
	return p.PRExistsResult, p.PRExistsErr
}

func (p *SpyProvider) CloneURL(repo domain.Repository) string {
	if repo.RemoteURL != "" {
		return repo.RemoteURL
	}
	return fmt.Sprintf(
		"https://example.com/%s/%s.git",
		repo.Organization, repo.Name,
	)
}

// ---------------------------------------------------------------------------
// SpyUpdater
// ---------------------------------------------------------------------------

// SpyUpdater implements domain.Updater as a configurable spy.
type SpyUpdater struct {
	// --- identity ---
	UpdaterName string

	// --- Detect ---
	DetectResult bool
	// spy: repos that were checked
	DetectedRepos []domain.Repository

	// --- CreateUpdatePRs ---
	PRs          []domain.PullRequest
	CreatePRsErr error
	// spy: calls received
	CreatePRsCalls []CreatePRsCall
}

// CreatePRsCall records a single invocation of CreateUpdatePRs.
type CreatePRsCall struct {
	Repo domain.Repository
	Opts domain.UpdateOptions
}

var _ domain.Updater = (*SpyUpdater)(nil)

func (u *SpyUpdater) Name() string { return u.UpdaterName }

func (u *SpyUpdater) Detect(
	_ context.Context,
	_ domain.Provider,
	repo domain.Repository,
) bool {
	u.DetectedRepos = append(u.DetectedRepos, repo)
	return u.DetectResult
}

func (u *SpyUpdater) CreateUpdatePRs(
	_ context.Context,
	_ domain.Provider,
	repo domain.Repository,
	opts domain.UpdateOptions,
) ([]domain.PullRequest, error) {
	u.CreatePRsCalls = append(
		u.CreatePRsCalls,
		CreatePRsCall{Repo: repo, Opts: opts},
	)
	return u.PRs, u.CreatePRsErr
}

// ---------------------------------------------------------------------------
// DummyProvider — satisfies the interface but does nothing (for compile checks)
// ---------------------------------------------------------------------------

// DummyProvider is a no-op implementation of domain.Provider.
// Use it only for interface compliance tests or as a placeholder.
type DummyProvider struct{}

var _ domain.Provider = (*DummyProvider)(nil)

func (d *DummyProvider) Name() string                        { return "dummy" }
func (d *DummyProvider) MatchesURL(_ string) bool            { return false }
func (d *DummyProvider) AuthToken() string                   { return "" }
func (d *DummyProvider) CloneURL(_ domain.Repository) string { return "" }

func (d *DummyProvider) DiscoverRepositories(
	_ context.Context,
	_ string,
) ([]domain.Repository, error) {
	return nil, nil
}

func (d *DummyProvider) GetFileContent(
	_ context.Context,
	_ domain.Repository,
	_ string,
) (string, error) {
	return "", nil
}

func (d *DummyProvider) ListFiles(
	_ context.Context,
	_ domain.Repository,
	_ string,
) ([]domain.File, error) {
	return nil, nil
}

func (d *DummyProvider) GetTags(
	_ context.Context,
	_ domain.Repository,
) ([]string, error) {
	return nil, nil
}

func (d *DummyProvider) HasFile(
	_ context.Context,
	_ domain.Repository,
	_ string,
) bool {
	return false
}

func (d *DummyProvider) CreateBranchWithChanges(
	_ context.Context,
	_ domain.Repository,
	_ domain.BranchInput,
) error {
	return nil
}

func (d *DummyProvider) CreatePullRequest(
	_ context.Context,
	_ domain.Repository,
	_ domain.PullRequestInput,
) (*domain.PullRequest, error) {
	return nil, nil //nolint:nilnil // dummy no-op
}

func (d *DummyProvider) PullRequestExists(
	_ context.Context,
	_ domain.Repository,
	_ string,
) (bool, error) {
	return false, nil
}

// ---------------------------------------------------------------------------
// DummyUpdater — satisfies the interface but does nothing
// ---------------------------------------------------------------------------

// DummyUpdater is a no-op implementation of domain.Updater.
type DummyUpdater struct{}

var _ domain.Updater = (*DummyUpdater)(nil)

func (d *DummyUpdater) Name() string { return "dummy" }

func (d *DummyUpdater) Detect(
	_ context.Context,
	_ domain.Provider,
	_ domain.Repository,
) bool {
	return false
}

func (d *DummyUpdater) CreateUpdatePRs(
	_ context.Context,
	_ domain.Provider,
	_ domain.Repository,
	_ domain.UpdateOptions,
) ([]domain.PullRequest, error) {
	return nil, nil
}
