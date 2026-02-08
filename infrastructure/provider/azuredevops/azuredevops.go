package azuredevops

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/rios0rios0/autoupdate/domain"
)

const (
	providerName     = "azuredevops"
	apiVersion       = "7.0"
	httpTimeout      = 30 * time.Second
	httpStatusOKMin  = 200
	httpStatusOKMax  = 300
	paginationHeader = "X-Ms-Continuationtoken"
	allZeroObjectID  = "0000000000000000000000000000000000000000"
)

// Provider implements domain.Provider for Azure DevOps.
type Provider struct {
	token      string
	httpClient *http.Client
}

// New creates a new Azure DevOps provider with the given PAT.
func New(token string) domain.Provider {
	return &Provider{
		token: token,
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
	}
}

func (p *Provider) Name() string      { return providerName }
func (p *Provider) AuthToken() string { return p.token }

func (p *Provider) MatchesURL(rawURL string) bool {
	return strings.Contains(rawURL, "dev.azure.com")
}

// DiscoverRepositories lists all repositories in all projects
// of an Azure DevOps organization. The org parameter should be
// the organization URL or just the org name.
func (p *Provider) DiscoverRepositories(
	ctx context.Context,
	org string,
) ([]domain.Repository, error) {
	baseURL := normalizeOrgURL(org)

	projects, err := p.getProjects(ctx, baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get projects: %w", err)
	}

	var repos []domain.Repository
	for _, proj := range projects {
		projRepos, repoErr := p.getRepositories(ctx, baseURL, proj.ID)
		if repoErr != nil {
			continue
		}
		for _, r := range projRepos {
			repos = append(repos, domain.Repository{
				ID:            r.ID,
				Name:          r.Name,
				Organization:  extractOrgName(baseURL),
				Project:       proj.Name,
				DefaultBranch: r.DefaultBranch,
				RemoteURL:     r.RemoteURL,
				SSHURL:        r.SSHURL,
				ProviderName:  providerName,
			})
		}
	}

	return repos, nil
}

func (p *Provider) GetFileContent(
	ctx context.Context,
	repo domain.Repository,
	path string,
) (string, error) {
	baseURL := buildBaseURL(repo.Organization)
	endpoint := fmt.Sprintf(
		"/%s/_apis/git/repositories/%s/items?path=%s&api-version=%s",
		repo.Project, repo.ID, url.QueryEscape(path), apiVersion,
	)

	resp, err := p.doRequest(ctx, baseURL, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	return string(resp), nil
}

func (p *Provider) ListFiles(
	ctx context.Context,
	repo domain.Repository,
	pattern string,
) ([]domain.File, error) {
	baseURL := buildBaseURL(repo.Organization)
	endpoint := fmt.Sprintf(
		"/%s/_apis/git/repositories/%s/items?recursionLevel=Full&api-version=%s",
		repo.Project, repo.ID, apiVersion,
	)

	resp, err := p.doRequest(ctx, baseURL, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Value []struct {
			ObjectID      string `json:"objectId"`
			GitObjectType string `json:"gitObjectType"`
			Path          string `json:"path"`
		} `json:"value"`
	}
	if unmarshalErr := json.Unmarshal(resp, &result); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to parse items response: %w", unmarshalErr)
	}

	var files []domain.File
	for _, item := range result.Value {
		isDir := item.GitObjectType != "blob"
		if pattern != "" && !strings.HasSuffix(item.Path, pattern) {
			continue
		}
		files = append(files, domain.File{
			Path:     item.Path,
			ObjectID: item.ObjectID,
			IsDir:    isDir,
		})
	}

	return files, nil
}

func (p *Provider) GetTags(
	ctx context.Context,
	repo domain.Repository,
) ([]string, error) {
	baseURL := buildBaseURL(repo.Organization)
	endpoint := fmt.Sprintf(
		"/%s/_apis/git/repositories/%s/refs?filter=tags&api-version=%s",
		repo.Project, repo.ID, apiVersion,
	)

	resp, err := p.doRequest(ctx, baseURL, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Value []struct {
			Name string `json:"name"`
		} `json:"value"`
	}
	if unmarshalErr := json.Unmarshal(resp, &result); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to parse tags response: %w", unmarshalErr)
	}

	var tags []string
	for _, ref := range result.Value {
		tags = append(tags, strings.TrimPrefix(ref.Name, "refs/tags/"))
	}

	sortVersionsDescending(tags)
	return tags, nil
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
	baseURL := buildBaseURL(repo.Organization)

	// Get the base branch commit ID
	baseCommitID, err := p.getCommitID(ctx, baseURL, repo)
	if err != nil {
		return fmt.Errorf("failed to get base branch commit: %w", err)
	}

	var fileChanges []map[string]interface{}
	for _, change := range input.Changes {
		entry := map[string]interface{}{
			"changeType": change.ChangeType,
			"item": map[string]string{
				"path": change.Path,
			},
			"newContent": map[string]string{
				"content":     base64.StdEncoding.EncodeToString([]byte(change.Content)),
				"contentType": "base64encoded",
			},
		}
		fileChanges = append(fileChanges, entry)
	}

	pushBody := map[string]interface{}{
		"refUpdates": []map[string]string{
			{
				"name":        "refs/heads/" + input.BranchName,
				"oldObjectId": allZeroObjectID,
			},
		},
		"commits": []map[string]interface{}{
			{
				"comment": input.CommitMessage,
				"changes": fileChanges,
				"parents": []string{baseCommitID},
			},
		},
	}

	endpoint := fmt.Sprintf(
		"/%s/_apis/git/repositories/%s/pushes?api-version=%s",
		repo.Project, repo.ID, apiVersion,
	)

	_, err = p.doRequest(ctx, baseURL, http.MethodPost, endpoint, pushBody)
	if err != nil {
		return fmt.Errorf("failed to push changes: %w", err)
	}

	return nil
}

func (p *Provider) CreatePullRequest(
	ctx context.Context,
	repo domain.Repository,
	input domain.PullRequestInput,
) (*domain.PullRequest, error) {
	baseURL := buildBaseURL(repo.Organization)

	body := map[string]interface{}{
		"sourceRefName": input.SourceBranch,
		"targetRefName": input.TargetBranch,
		"title":         input.Title,
		"description":   input.Description,
	}

	endpoint := fmt.Sprintf(
		"/%s/_apis/git/repositories/%s/pullrequests?api-version=%s",
		repo.Project, repo.ID, apiVersion,
	)

	resp, err := p.doRequest(ctx, baseURL, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}

	var prResp struct {
		PullRequestID int    `json:"pullRequestId"`
		Title         string `json:"title"`
		URL           string `json:"url"`
		Status        string `json:"status"`
	}
	if unmarshalErr := json.Unmarshal(resp, &prResp); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to parse PR response: %w", unmarshalErr)
	}

	pr := &domain.PullRequest{
		ID:     prResp.PullRequestID,
		Title:  prResp.Title,
		URL:    prResp.URL,
		Status: prResp.Status,
	}

	if input.AutoComplete {
		updateBody := map[string]interface{}{
			"autoCompleteSetBy": map[string]string{"id": "me"},
		}
		updateEndpoint := fmt.Sprintf(
			"/%s/_apis/git/repositories/%s/pullrequests/%d?api-version=%s",
			repo.Project, repo.ID, pr.ID, apiVersion,
		)
		_, _ = p.doRequest(ctx, baseURL, http.MethodPatch, updateEndpoint, updateBody)
	}

	return pr, nil
}

func (p *Provider) PullRequestExists(
	ctx context.Context,
	repo domain.Repository,
	sourceBranch string,
) (bool, error) {
	baseURL := buildBaseURL(repo.Organization)
	endpoint := fmt.Sprintf(
		"/%s/_apis/git/repositories/%s/pullrequests?searchCriteria.sourceRefName=refs/heads/%s&searchCriteria.status=active&api-version=%s",
		repo.Project,
		repo.ID,
		sourceBranch,
		apiVersion,
	)

	resp, err := p.doRequest(ctx, baseURL, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}

	var result struct {
		Count int `json:"count"`
	}
	if unmarshalErr := json.Unmarshal(resp, &result); unmarshalErr != nil {
		return false, fmt.Errorf("failed to parse PR list response: %w", unmarshalErr)
	}

	return result.Count > 0, nil
}

func (p *Provider) CloneURL(repo domain.Repository) string {
	remoteURL := repo.RemoteURL
	if remoteURL == "" {
		remoteURL = fmt.Sprintf(
			"https://dev.azure.com/%s/%s/_git/%s",
			repo.Organization, repo.Project, repo.Name,
		)
	}

	parsed, err := url.Parse(remoteURL)
	if err != nil {
		return strings.Replace(
			remoteURL, "https://", "https://pat:"+p.token+"@", 1,
		)
	}

	parsed.User = url.UserPassword("pat", p.token)

	return parsed.String()
}

// --- internal helpers ---

type project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type repository struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	RemoteURL     string `json:"remoteUrl"`
	SSHURL        string `json:"sshUrl"`
	DefaultBranch string `json:"defaultBranch"`
}

func (p *Provider) getProjects(
	ctx context.Context,
	baseURL string,
) ([]project, error) {
	var all []project
	continuationToken := ""

	for {
		endpoint := "/_apis/projects?api-version=" + apiVersion
		if continuationToken != "" {
			endpoint += "&continuationToken=" + continuationToken
		}

		resp, headers, err := p.doRequestWithHeaders(
			ctx, baseURL, http.MethodGet, endpoint, nil,
		)
		if err != nil {
			return nil, err
		}

		var result struct {
			Value []project `json:"value"`
		}
		if unmarshalErr := json.Unmarshal(resp, &result); unmarshalErr != nil {
			return nil, fmt.Errorf("failed to parse projects response: %w", unmarshalErr)
		}

		all = append(all, result.Value...)
		continuationToken = headers.Get(paginationHeader)
		if continuationToken == "" {
			break
		}
	}

	return all, nil
}

func (p *Provider) getRepositories(
	ctx context.Context,
	baseURL, projectID string,
) ([]repository, error) {
	endpoint := fmt.Sprintf(
		"/%s/_apis/git/repositories?api-version=%s",
		projectID, apiVersion,
	)

	resp, err := p.doRequest(ctx, baseURL, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Value []repository `json:"value"`
	}
	if unmarshalErr := json.Unmarshal(resp, &result); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to parse repositories response: %w", unmarshalErr)
	}

	return result.Value, nil
}

func (p *Provider) getCommitID(
	ctx context.Context,
	baseURL string,
	repo domain.Repository,
) (string, error) {
	endpoint := fmt.Sprintf(
		"/%s/_apis/git/repositories/%s?api-version=%s",
		repo.Project, repo.ID, apiVersion,
	)

	resp, err := p.doRequest(ctx, baseURL, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	var repoInfo struct {
		DefaultBranch string `json:"defaultBranch"`
	}
	if unmarshalErr := json.Unmarshal(resp, &repoInfo); unmarshalErr != nil {
		return "", fmt.Errorf("failed to parse repository info: %w", unmarshalErr)
	}

	branchName := strings.TrimPrefix(repoInfo.DefaultBranch, "refs/heads/")
	branchEndpoint := fmt.Sprintf(
		"/%s/_apis/git/repositories/%s/refs?filter=heads/%s&api-version=%s",
		repo.Project, repo.ID, branchName, apiVersion,
	)

	branchResp, branchErr := p.doRequest(
		ctx, baseURL, http.MethodGet, branchEndpoint, nil,
	)
	if branchErr != nil {
		return "", branchErr
	}

	var branchResult struct {
		Value []struct {
			ObjectID string `json:"objectId"`
		} `json:"value"`
	}
	if unmarshalErr := json.Unmarshal(branchResp, &branchResult); unmarshalErr != nil {
		return "", fmt.Errorf("failed to parse branch response: %w", unmarshalErr)
	}
	if len(branchResult.Value) == 0 {
		return "", errors.New("default branch not found")
	}

	return branchResult.Value[0].ObjectID, nil
}

func (p *Provider) doRequest(
	ctx context.Context,
	baseURL, method, endpoint string,
	body interface{},
) ([]byte, error) {
	resp, _, err := p.doRequestWithHeaders(ctx, baseURL, method, endpoint, body)
	return resp, err
}

func (p *Provider) doRequestWithHeaders(
	ctx context.Context,
	baseURL, method, endpoint string,
	body interface{},
) ([]byte, http.Header, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBody, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return nil, nil, fmt.Errorf("failed to marshal request body: %w", marshalErr)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	fullURL := baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, method, fullURL, reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	auth := base64.StdEncoding.EncodeToString([]byte(":" + p.token))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < httpStatusOKMin || resp.StatusCode >= httpStatusOKMax {
		return nil, nil, fmt.Errorf(
			"API error (status %d): %s",
			resp.StatusCode, string(respBody),
		)
	}

	return respBody, resp.Header, nil
}

// --- URL helpers ---

func normalizeOrgURL(org string) string {
	org = strings.TrimSuffix(org, "/")
	if !strings.HasPrefix(org, "https://") {
		org = "https://dev.azure.com/" + org
	}
	return org
}

func buildBaseURL(orgName string) string {
	return "https://dev.azure.com/" + strings.Split(orgName, "/")[0]
}

func extractOrgName(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return u.Host
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
