package gitlab

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	gl "gitlab.com/gitlab-org/api/client-go"
	"golang.org/x/mod/semver"

	"github.com/rios0rios0/autoupdate/domain"
)

const (
	providerName = "gitlab"
	perPage      = 100
)

var errClientNotInitialized = errors.New("gitlab client not initialized")

// Provider implements domain.Provider for GitLab.
type Provider struct {
	token  string
	client *gl.Client
}

// New creates a new GitLab provider with the given token.
func New(token string) domain.Provider {
	client, err := gl.NewClient(token)
	if err != nil {
		// Return a provider that will fail on use rather than panicking at construction
		return &Provider{token: token, client: nil}
	}
	return &Provider{
		token:  token,
		client: client,
	}
}

func (p *Provider) Name() string      { return providerName }
func (p *Provider) AuthToken() string { return p.token }

func (p *Provider) MatchesURL(rawURL string) bool {
	return strings.Contains(rawURL, "gitlab.com")
}

// DiscoverRepositories lists all projects in a GitLab group.
func (p *Provider) DiscoverRepositories(
	ctx context.Context,
	group string,
) ([]domain.Repository, error) {
	if p.client == nil {
		return nil, errClientNotInitialized
	}

	var allRepos []domain.Repository
	opts := &gl.ListGroupProjectsOptions{
		ListOptions:      gl.ListOptions{PerPage: perPage},
		IncludeSubGroups: gl.Ptr(true),
	}

	for {
		projects, resp, err := p.client.Groups.ListGroupProjects(
			group, opts, gl.WithContext(ctx),
		)
		if err != nil {
			// Fall back to listing user projects
			return p.discoverUserProjects(ctx, group)
		}

		for _, proj := range projects {
			defaultBranch := "main"
			if proj.DefaultBranch != "" {
				defaultBranch = proj.DefaultBranch
			}
			allRepos = append(allRepos, domain.Repository{
				ID:            strconv.FormatInt(proj.ID, 10),
				Name:          proj.Path,
				Organization:  group,
				DefaultBranch: "refs/heads/" + defaultBranch,
				RemoteURL:     proj.HTTPURLToRepo,
				SSHURL:        proj.SSHURLToRepo,
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

func (p *Provider) discoverUserProjects(
	ctx context.Context,
	user string,
) ([]domain.Repository, error) {
	var allRepos []domain.Repository
	opts := &gl.ListProjectsOptions{
		ListOptions: gl.ListOptions{PerPage: perPage},
		Owned:       gl.Ptr(true),
	}

	for {
		projects, resp, err := p.client.Projects.ListProjects(
			opts, gl.WithContext(ctx),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to list projects for %q: %w", user, err)
		}

		for _, proj := range projects {
			defaultBranch := "main"
			if proj.DefaultBranch != "" {
				defaultBranch = proj.DefaultBranch
			}
			allRepos = append(allRepos, domain.Repository{
				ID:            strconv.FormatInt(proj.ID, 10),
				Name:          proj.Path,
				Organization:  user,
				DefaultBranch: "refs/heads/" + defaultBranch,
				RemoteURL:     proj.HTTPURLToRepo,
				SSHURL:        proj.SSHURLToRepo,
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

func (p *Provider) GetFileContent(
	ctx context.Context,
	repo domain.Repository,
	path string,
) (string, error) {
	if p.client == nil {
		return "", errClientNotInitialized
	}

	branch := strings.TrimPrefix(repo.DefaultBranch, "refs/heads/")
	raw, _, err := p.client.RepositoryFiles.GetRawFile(
		repo.Organization+"/"+repo.Name, path,
		&gl.GetRawFileOptions{Ref: gl.Ptr(branch)},
		gl.WithContext(ctx),
	)
	if err != nil {
		return "", fmt.Errorf("failed to get file %q: %w", path, err)
	}

	return string(raw), nil
}

func (p *Provider) ListFiles(
	ctx context.Context,
	repo domain.Repository,
	pattern string,
) ([]domain.File, error) {
	if p.client == nil {
		return nil, errClientNotInitialized
	}

	branch := strings.TrimPrefix(repo.DefaultBranch, "refs/heads/")
	recursive := true
	var allFiles []domain.File
	opts := &gl.ListTreeOptions{
		ListOptions: gl.ListOptions{PerPage: perPage},
		Ref:         gl.Ptr(branch),
		Recursive:   &recursive,
	}

	for {
		nodes, resp, err := p.client.Repositories.ListTree(
			repo.Organization+"/"+repo.Name,
			opts,
			gl.WithContext(ctx),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to list tree: %w", err)
		}

		for _, node := range nodes {
			if pattern != "" && !strings.HasSuffix(node.Path, pattern) {
				continue
			}
			allFiles = append(allFiles, domain.File{
				Path:     node.Path,
				ObjectID: node.ID,
				IsDir:    node.Type == "tree",
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allFiles, nil
}

func (p *Provider) GetTags(
	ctx context.Context,
	repo domain.Repository,
) ([]string, error) {
	if p.client == nil {
		return nil, errClientNotInitialized
	}

	var allTags []string
	opts := &gl.ListTagsOptions{
		ListOptions: gl.ListOptions{PerPage: perPage},
	}

	pid := repo.Organization + "/" + repo.Name
	for {
		tags, resp, err := p.client.Tags.ListTags(
			pid, opts, gl.WithContext(ctx),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to list tags: %w", err)
		}

		for _, tag := range tags {
			allTags = append(allTags, tag.Name)
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	sortVersionsDescending(allTags)
	return allTags, nil
}

func (p *Provider) HasFile(
	ctx context.Context,
	repo domain.Repository,
	path string,
) bool {
	_, err := p.GetFileContent(ctx, repo, path)
	return err == nil
}

func (p *Provider) CreateBranchWithChanges(
	ctx context.Context,
	repo domain.Repository,
	input domain.BranchInput,
) error {
	if p.client == nil {
		return errClientNotInitialized
	}

	pid := repo.Organization + "/" + repo.Name
	baseBranch := strings.TrimPrefix(input.BaseBranch, "refs/heads/")

	// Create the branch first
	_, _, err := p.client.Branches.CreateBranch(pid, &gl.CreateBranchOptions{
		Branch: gl.Ptr(input.BranchName),
		Ref:    gl.Ptr(baseBranch),
	}, gl.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("failed to create branch: %w", err)
	}

	// Commit each file change
	var actions []*gl.CommitActionOptions
	for _, change := range input.Changes {
		action := gl.FileUpdate
		switch change.ChangeType {
		case "add":
			action = gl.FileCreate
		case "delete":
			action = gl.FileDelete
		}
		filePath := strings.TrimPrefix(change.Path, "/")
		content := change.Content
		actions = append(actions, &gl.CommitActionOptions{
			Action:   &action,
			FilePath: &filePath,
			Content:  &content,
		})
	}

	_, _, err = p.client.Commits.CreateCommit(
		pid,
		&gl.CreateCommitOptions{
			Branch:        gl.Ptr(input.BranchName),
			CommitMessage: gl.Ptr(input.CommitMessage),
			Actions:       actions,
		},
		gl.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("failed to create commit: %w", err)
	}

	return nil
}

func (p *Provider) CreatePullRequest(
	ctx context.Context,
	repo domain.Repository,
	input domain.PullRequestInput,
) (*domain.PullRequest, error) {
	if p.client == nil {
		return nil, errClientNotInitialized
	}

	pid := repo.Organization + "/" + repo.Name
	sourceBranch := strings.TrimPrefix(input.SourceBranch, "refs/heads/")
	targetBranch := strings.TrimPrefix(input.TargetBranch, "refs/heads/")

	mr, _, err := p.client.MergeRequests.CreateMergeRequest(
		pid,
		&gl.CreateMergeRequestOptions{
			Title:              gl.Ptr(input.Title),
			Description:        gl.Ptr(input.Description),
			SourceBranch:       gl.Ptr(sourceBranch),
			TargetBranch:       gl.Ptr(targetBranch),
			RemoveSourceBranch: gl.Ptr(true),
		},
		gl.WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create merge request: %w", err)
	}

	return &domain.PullRequest{
		ID:     int(mr.IID),
		Title:  mr.Title,
		URL:    mr.WebURL,
		Status: mr.State,
	}, nil
}

func (p *Provider) PullRequestExists(
	ctx context.Context,
	repo domain.Repository,
	sourceBranch string,
) (bool, error) {
	if p.client == nil {
		return false, errClientNotInitialized
	}

	pid := repo.Organization + "/" + repo.Name
	state := "opened"
	mrs, _, err := p.client.MergeRequests.ListProjectMergeRequests(
		pid,
		&gl.ListProjectMergeRequestsOptions{
			SourceBranch: gl.Ptr(sourceBranch),
			State:        gl.Ptr(state),
		},
		gl.WithContext(ctx),
	)
	if err != nil {
		return false, fmt.Errorf("failed to list merge requests: %w", err)
	}

	return len(mrs) > 0, nil
}

func (p *Provider) CloneURL(repo domain.Repository) string {
	remoteURL := repo.RemoteURL
	if remoteURL == "" {
		remoteURL = fmt.Sprintf(
			"https://gitlab.com/%s/%s.git",
			repo.Organization, repo.Name,
		)
	}
	return strings.Replace(
		remoteURL,
		"https://",
		"https://oauth2:"+p.token+"@",
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
