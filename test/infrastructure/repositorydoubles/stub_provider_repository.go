//go:build integration || unit || test

// Package repositorydoubles provides test doubles (spies, stubs, dummies) for
// repository interfaces. These are hand-crafted implementations — no mock frameworks.
package repositorydoubles //nolint:revive,staticcheck // Test package naming follows established project structure

import (
	"context"
	"fmt"
	"sync"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// SpyProviderRepository implements repositories.ProviderRepository as a configurable spy.
// The recorder slices are guarded by mu because the run command processes
// repositories concurrently, so the provider methods may be called from
// multiple goroutines against the same shared spy.
type SpyProviderRepository struct {
	mu sync.Mutex

	// --- identity ---
	ProviderName string
	Token        string

	// --- DiscoverRepositories ---
	Repositories   []entities.Repository
	DiscoverErr    error
	DiscoveredOrgs []string

	// --- GetFileContent ---
	FileContents   map[string]string
	FileContentErr error

	// --- ListFiles ---
	Files       []entities.File
	ListFileErr error

	// --- GetTags ---
	Tags       []string
	GetTagsErr error

	// --- HasFile ---
	ExistingFiles map[string]bool

	// --- CreateBranchWithChanges ---
	CreateBranchErr error
	BranchInputs    []entities.BranchInput

	// --- CreatePullRequest ---
	CreatedPR   *entities.PullRequest
	CreatePRErr error
	PRInputs    []entities.PullRequestInput

	// --- PullRequestExists ---
	PRExistsResult   bool
	PRExistsErr      error
	PRExistsBranches []string

	// --- ClosePullRequest ---
	PRClosedResult   bool
	PRCloseErr       error
	PRClosedBranches []string
}

var _ repositories.ProviderRepository = (*SpyProviderRepository)(nil)

func (p *SpyProviderRepository) Name() string             { return p.ProviderName }
func (p *SpyProviderRepository) AuthToken() string        { return p.Token }
func (p *SpyProviderRepository) MatchesURL(_ string) bool { return false }

func (p *SpyProviderRepository) DiscoverRepositories(
	_ context.Context, org string,
) ([]entities.Repository, error) {
	p.mu.Lock()
	p.DiscoveredOrgs = append(p.DiscoveredOrgs, org)
	p.mu.Unlock()
	return p.Repositories, p.DiscoverErr
}

func (p *SpyProviderRepository) GetFileContent(
	_ context.Context, _ entities.Repository, path string,
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

func (p *SpyProviderRepository) ListFiles(
	_ context.Context, _ entities.Repository, _ string,
) ([]entities.File, error) {
	return p.Files, p.ListFileErr
}

func (p *SpyProviderRepository) GetTags(
	_ context.Context, _ entities.Repository,
) ([]string, error) {
	return p.Tags, p.GetTagsErr
}

func (p *SpyProviderRepository) HasFile(
	_ context.Context, _ entities.Repository, path string,
) bool {
	if p.ExistingFiles != nil {
		return p.ExistingFiles[path]
	}
	return false
}

func (p *SpyProviderRepository) CreateBranchWithChanges(
	_ context.Context, _ entities.Repository, input entities.BranchInput,
) error {
	p.mu.Lock()
	p.BranchInputs = append(p.BranchInputs, input)
	p.mu.Unlock()
	return p.CreateBranchErr
}

func (p *SpyProviderRepository) CreatePullRequest(
	_ context.Context, _ entities.Repository, input entities.PullRequestInput,
) (*entities.PullRequest, error) {
	p.mu.Lock()
	p.PRInputs = append(p.PRInputs, input)
	p.mu.Unlock()
	if p.CreatePRErr != nil {
		return nil, p.CreatePRErr
	}
	if p.CreatedPR != nil {
		return p.CreatedPR, nil
	}
	return &entities.PullRequest{
		ID:    1,
		Title: input.Title,
		URL:   "https://example.com/pr/1",
	}, nil
}

func (p *SpyProviderRepository) PullRequestExists(
	_ context.Context, _ entities.Repository, branch string,
) (bool, error) {
	p.mu.Lock()
	p.PRExistsBranches = append(p.PRExistsBranches, branch)
	p.mu.Unlock()
	return p.PRExistsResult, p.PRExistsErr
}

func (p *SpyProviderRepository) ClosePullRequest(
	_ context.Context, _ entities.Repository, branch string,
) (bool, error) {
	p.mu.Lock()
	p.PRClosedBranches = append(p.PRClosedBranches, branch)
	p.mu.Unlock()
	return p.PRClosedResult, p.PRCloseErr
}

func (p *SpyProviderRepository) SSHCloneURL(_ entities.Repository, _ string) string { return "" }

func (p *SpyProviderRepository) CloneURL(repo entities.Repository) string {
	if repo.RemoteURL != "" {
		return repo.RemoteURL
	}
	return fmt.Sprintf("https://example.com/%s/%s.git", repo.Organization, repo.Name)
}

// DummyProviderRepository is a no-op implementation of repositories.ProviderRepository.
type DummyProviderRepository struct{}

var _ repositories.ProviderRepository = (*DummyProviderRepository)(nil)

func (d *DummyProviderRepository) Name() string                                       { return "dummy" }
func (d *DummyProviderRepository) MatchesURL(_ string) bool                           { return false }
func (d *DummyProviderRepository) AuthToken() string                                  { return "" }
func (d *DummyProviderRepository) CloneURL(_ entities.Repository) string              { return "" }
func (d *DummyProviderRepository) SSHCloneURL(_ entities.Repository, _ string) string { return "" }

func (d *DummyProviderRepository) DiscoverRepositories(
	_ context.Context, _ string,
) ([]entities.Repository, error) {
	return nil, nil
}

func (d *DummyProviderRepository) GetFileContent(
	_ context.Context, _ entities.Repository, _ string,
) (string, error) {
	return "", nil
}

func (d *DummyProviderRepository) ListFiles(
	_ context.Context, _ entities.Repository, _ string,
) ([]entities.File, error) {
	return nil, nil
}

func (d *DummyProviderRepository) GetTags(
	_ context.Context, _ entities.Repository,
) ([]string, error) {
	return nil, nil
}

func (d *DummyProviderRepository) HasFile(
	_ context.Context, _ entities.Repository, _ string,
) bool {
	return false
}

func (d *DummyProviderRepository) CreateBranchWithChanges(
	_ context.Context, _ entities.Repository, _ entities.BranchInput,
) error {
	return nil
}

func (d *DummyProviderRepository) CreatePullRequest(
	_ context.Context, _ entities.Repository, _ entities.PullRequestInput,
) (*entities.PullRequest, error) {
	return nil, nil //nolint:nilnil // dummy no-op
}

func (d *DummyProviderRepository) PullRequestExists(
	_ context.Context, _ entities.Repository, _ string,
) (bool, error) {
	return false, nil
}

func (d *DummyProviderRepository) ClosePullRequest(
	_ context.Context, _ entities.Repository, _ string,
) (bool, error) {
	return false, nil
}
