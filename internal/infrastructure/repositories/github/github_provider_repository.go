package github

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	gh "github.com/google/go-github/v66/github"
	"golang.org/x/mod/semver"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

const (
	providerName = "github"
	perPage      = 100
	blobMode     = "100644"
	blobType     = "blob"
)

// GitHubProviderRepository implements repositories.ProviderRepository for GitHub.
type GitHubProviderRepository struct {
	token  string
	client *gh.Client
}

// NewGitHubProviderRepository creates a new GitHub provider with the given token.
func NewGitHubProviderRepository(token string) repositories.ProviderRepository {
	client := gh.NewClient(nil).WithAuthToken(token)
	return &GitHubProviderRepository{
		token:  token,
		client: client,
	}
}

func (p *GitHubProviderRepository) Name() string      { return providerName }
func (p *GitHubProviderRepository) AuthToken() string { return p.token }

func (p *GitHubProviderRepository) MatchesURL(rawURL string) bool {
	return strings.Contains(rawURL, "github.com")
}

// DiscoverRepositories lists all repositories in a GitHub
// organization or user account.
func (p *GitHubProviderRepository) DiscoverRepositories(
	ctx context.Context,
	org string,
) ([]entities.Repository, error) {
	var allRepos []entities.Repository
	opts := &gh.RepositoryListByOrgOptions{
		ListOptions: gh.ListOptions{PerPage: perPage},
	}

	for {
		repos, resp, err := p.client.Repositories.ListByOrg(ctx, org, opts)
		if err != nil {
			// Fall back to listing user repos if org listing fails
			return p.discoverUserRepos(ctx, org)
		}

		for _, r := range repos {
			defaultBranch := "main"
			if r.DefaultBranch != nil {
				defaultBranch = *r.DefaultBranch
			}
			allRepos = append(allRepos, entities.Repository{
				ID:            strconv.FormatInt(r.GetID(), 10),
				Name:          r.GetName(),
				Organization:  org,
				DefaultBranch: "refs/heads/" + defaultBranch,
				RemoteURL:     r.GetCloneURL(),
				SSHURL:        r.GetSSHURL(),
				ProviderName:  providerName,
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allRepos, nil
}

func (p *GitHubProviderRepository) discoverUserRepos(
	ctx context.Context,
	user string,
) ([]entities.Repository, error) {
	var allRepos []entities.Repository
	opts := &gh.RepositoryListByUserOptions{
		ListOptions: gh.ListOptions{PerPage: perPage},
		Type:        "owner",
	}

	for {
		repos, resp, err := p.client.Repositories.ListByUser(ctx, user, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list repos for %q: %w", user, err)
		}

		for _, r := range repos {
			defaultBranch := "main"
			if r.DefaultBranch != nil {
				defaultBranch = *r.DefaultBranch
			}
			allRepos = append(allRepos, entities.Repository{
				ID:            strconv.FormatInt(r.GetID(), 10),
				Name:          r.GetName(),
				Organization:  user,
				DefaultBranch: "refs/heads/" + defaultBranch,
				RemoteURL:     r.GetCloneURL(),
				SSHURL:        r.GetSSHURL(),
				ProviderName:  providerName,
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allRepos, nil
}

func (p *GitHubProviderRepository) GetFileContent(
	ctx context.Context,
	repo entities.Repository,
	path string,
) (string, error) {
	fileContent, _, _, err := p.client.Repositories.GetContents(
		ctx, repo.Organization, repo.Name, path,
		&gh.RepositoryContentGetOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("failed to get file %q: %w", path, err)
	}
	if fileContent == nil {
		return "", fmt.Errorf("path %q is a directory, not a file", path)
	}

	content, err := fileContent.GetContent()
	if err != nil {
		return "", fmt.Errorf("failed to decode file content: %w", err)
	}

	return content, nil
}

func (p *GitHubProviderRepository) ListFiles(
	ctx context.Context,
	repo entities.Repository,
	pattern string,
) ([]entities.File, error) {
	tree, _, err := p.client.Git.GetTree(
		ctx, repo.Organization, repo.Name,
		strings.TrimPrefix(repo.DefaultBranch, "refs/heads/"),
		true, // recursive
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get repo tree: %w", err)
	}

	var files []entities.File
	for _, entry := range tree.Entries {
		if pattern != "" && !strings.HasSuffix(entry.GetPath(), pattern) {
			continue
		}
		files = append(files, entities.File{
			Path:     entry.GetPath(),
			ObjectID: entry.GetSHA(),
			IsDir:    entry.GetType() == "tree",
		})
	}

	return files, nil
}

func (p *GitHubProviderRepository) GetTags(
	ctx context.Context,
	repo entities.Repository,
) ([]string, error) {
	var allTags []string
	opts := &gh.ListOptions{PerPage: perPage}

	for {
		tags, resp, err := p.client.Repositories.ListTags(
			ctx, repo.Organization, repo.Name, opts,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to list tags: %w", err)
		}

		for _, tag := range tags {
			allTags = append(allTags, tag.GetName())
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	sortVersionsDescending(allTags)
	return allTags, nil
}

func (p *GitHubProviderRepository) HasFile(
	ctx context.Context,
	repo entities.Repository,
	path string,
) bool {
	_, err := p.GetFileContent(ctx, repo, path)
	return err == nil
}

func (p *GitHubProviderRepository) CreateBranchWithChanges(
	ctx context.Context,
	repo entities.Repository,
	input entities.BranchInput,
) error {
	owner := repo.Organization
	repoName := repo.Name

	// Get the base branch reference
	baseBranch := strings.TrimPrefix(input.BaseBranch, "refs/heads/")
	baseRef, _, err := p.client.Git.GetRef(
		ctx, owner, repoName, "refs/heads/"+baseBranch,
	)
	if err != nil {
		return fmt.Errorf("failed to get base branch ref: %w", err)
	}
	baseSHA := baseRef.Object.GetSHA()

	// Get the base tree
	baseCommit, _, err := p.client.Git.GetCommit(
		ctx, owner, repoName, baseSHA,
	)
	if err != nil {
		return fmt.Errorf("failed to get base commit: %w", err)
	}

	// Create tree entries for the changes
	var entries []*gh.TreeEntry
	for _, change := range input.Changes {
		content := change.Content
		path := strings.TrimPrefix(change.Path, "/")
		mode := blobMode
		entryType := blobType
		entries = append(entries, &gh.TreeEntry{
			Path:    &path,
			Mode:    &mode,
			Type:    &entryType,
			Content: &content,
		})
	}

	// Create new tree
	newTree, _, err := p.client.Git.CreateTree(
		ctx, owner, repoName, baseCommit.Tree.GetSHA(), entries,
	)
	if err != nil {
		return fmt.Errorf("failed to create tree: %w", err)
	}

	// Create commit
	newCommit, _, err := p.client.Git.CreateCommit(
		ctx, owner, repoName,
		&gh.Commit{
			Message: &input.CommitMessage,
			Tree:    newTree,
			Parents: []*gh.Commit{{SHA: &baseSHA}},
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to create commit: %w", err)
	}

	// Create the new branch ref
	branchRef := "refs/heads/" + input.BranchName
	_, _, err = p.client.Git.CreateRef(
		ctx, owner, repoName,
		&gh.Reference{
			Ref:    &branchRef,
			Object: &gh.GitObject{SHA: newCommit.SHA},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create branch: %w", err)
	}

	return nil
}

func (p *GitHubProviderRepository) CreatePullRequest(
	ctx context.Context,
	repo entities.Repository,
	input entities.PullRequestInput,
) (*entities.PullRequest, error) {
	owner := repo.Organization
	repoName := repo.Name

	sourceBranch := strings.TrimPrefix(input.SourceBranch, "refs/heads/")
	targetBranch := strings.TrimPrefix(input.TargetBranch, "refs/heads/")

	maintainerCanModify := true
	pr, _, err := p.client.PullRequests.Create(
		ctx, owner, repoName,
		&gh.NewPullRequest{
			Title:               &input.Title,
			Head:                &sourceBranch,
			Base:                &targetBranch,
			Body:                &input.Description,
			MaintainerCanModify: &maintainerCanModify,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create pull request: %w", err)
	}

	return &entities.PullRequest{
		ID:     pr.GetNumber(),
		Title:  pr.GetTitle(),
		URL:    pr.GetHTMLURL(),
		Status: pr.GetState(),
	}, nil
}

func (p *GitHubProviderRepository) PullRequestExists(
	ctx context.Context,
	repo entities.Repository,
	sourceBranch string,
) (bool, error) {
	owner := repo.Organization
	repoName := repo.Name

	prs, _, err := p.client.PullRequests.List(
		ctx, owner, repoName,
		&gh.PullRequestListOptions{
			Head:  owner + ":" + sourceBranch,
			State: "open",
		},
	)
	if err != nil {
		return false, fmt.Errorf("failed to list pull requests: %w", err)
	}

	return len(prs) > 0, nil
}

func (p *GitHubProviderRepository) CloneURL(repo entities.Repository) string {
	remoteURL := repo.RemoteURL
	if remoteURL == "" {
		remoteURL = fmt.Sprintf(
			"https://github.com/%s/%s.git",
			repo.Organization, repo.Name,
		)
	}
	return strings.Replace(
		remoteURL,
		"https://",
		"https://x-access-token:"+p.token+"@",
		1,
	)
}

// --- version sorting ---

func sortVersionsDescending(versions []string) {
	sort.Slice(versions, func(i, j int) bool {
		v1 := normalizeVersion(versions[i])
		v2 := normalizeVersion(versions[j])
		if semver.IsValid(v1) && semver.IsValid(v2) {
			return semver.Compare(v1, v2) > 0
		}
		return versions[i] > versions[j]
	})
}

func normalizeVersion(version string) string {
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}
