package domain

import "context"

// Provider abstracts a Git hosting service (GitHub, GitLab, Azure DevOps, etc.).
// Each implementation handles authentication, repository discovery, file access,
// and pull request management for its platform.
type Provider interface {
	// Name returns the provider identifier (e.g. "github", "gitlab", "azuredevops").
	Name() string

	// MatchesURL returns true if the given remote URL belongs to this provider.
	MatchesURL(url string) bool

	// DiscoverRepositories lists all repositories in an organization or group.
	DiscoverRepositories(ctx context.Context, org string) ([]Repository, error)

	// GetFileContent reads the content of a file from a repository's default branch.
	GetFileContent(ctx context.Context, repo Repository, path string) (string, error)

	// ListFiles returns the list of files in a repository, optionally filtered by a path pattern.
	// When pattern is empty the entire tree is returned.
	ListFiles(ctx context.Context, repo Repository, pattern string) ([]File, error)

	// GetTags returns all tags for a repository, sorted by semantic version descending.
	GetTags(ctx context.Context, repo Repository) ([]string, error)

	// HasFile checks whether a file exists at the given path in a repository.
	HasFile(ctx context.Context, repo Repository, path string) bool

	// CreateBranchWithChanges creates a new branch with one or more file changes
	// committed on top of the base branch.
	CreateBranchWithChanges(ctx context.Context, repo Repository, input BranchInput) error

	// CreatePullRequest creates a pull/merge request on the hosting service.
	CreatePullRequest(ctx context.Context, repo Repository, input PullRequestInput) (*PullRequest, error)

	// PullRequestExists checks if an open pull request already exists for the given source branch.
	PullRequestExists(ctx context.Context, repo Repository, sourceBranch string) (bool, error)

	// CloneURL returns an HTTPS clone URL for the repository, potentially with
	// embedded credentials for authenticated access.
	CloneURL(repo Repository) string

	// AuthToken returns the authentication token configured for this provider.
	AuthToken() string
}
