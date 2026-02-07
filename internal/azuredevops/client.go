package azuredevops

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client represents an Azure DevOps API client
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	org        string
}

// NewClient creates a new Azure DevOps client
func NewClient(organization, pat string) *Client {
	// Normalize organization URL
	org := strings.TrimSuffix(organization, "/")
	if !strings.HasPrefix(org, "https://") {
		org = "https://dev.azure.com/" + org
	}

	// Extract just the org name for building source URLs
	orgName := extractOrgName(org)

	return &Client{
		baseURL: org,
		token:   pat,
		org:     orgName,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func extractOrgName(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 {
		return u.Host + "/" + parts[0]
	}
	return u.Host
}

// Organization returns the organization identifier
func (c *Client) Organization() string {
	return c.org
}

// Token returns the PAT token
func (c *Client) Token() string {
	return c.token
}

// BaseURL returns the base URL of the Azure DevOps organization
func (c *Client) BaseURL() string {
	return c.baseURL
}

// HasFile checks if a specific file exists at the given path in a repository
func (c *Client) HasFile(ctx context.Context, projectID, repoID, path string) bool {
	endpoint := fmt.Sprintf("/%s/_apis/git/repositories/%s/items?path=%s&api-version=7.0",
		projectID, repoID, url.QueryEscape(path))

	_, err := c.doRequest(ctx, "GET", endpoint, nil)
	return err == nil
}

// AuthCloneURL builds a clone URL with embedded PAT authentication
func (c *Client) AuthCloneURL(remoteURL string) string {
	// remoteURL looks like: https://dev.azure.com/???/Project/_git/Repo
	// We need: https://pat:<TOKEN>@dev.azure.com/???/Project/_git/Repo
	return strings.Replace(remoteURL, "https://", "https://pat:"+c.token+"@", 1)
}

// Project represents an Azure DevOps project
type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	State       string `json:"state"`
}

// Repository represents an Azure DevOps Git repository
type Repository struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	URL           string  `json:"url"`
	RemoteURL     string  `json:"remoteUrl"`
	SSHURL        string  `json:"sshUrl"`
	DefaultBranch string  `json:"defaultBranch"`
	Project       Project `json:"project"`
}

// RepositoryItem represents a file or folder in a repository
type RepositoryItem struct {
	ObjectID      string `json:"objectId"`
	GitObjectType string `json:"gitObjectType"`
	CommitID      string `json:"commitId"`
	Path          string `json:"path"`
	URL           string `json:"url"`
}

// FileChange represents a file modification for a commit
type FileChange struct {
	Path       string
	Content    string
	ChangeType string // "add", "edit", "delete"
}

// PullRequest represents an Azure DevOps pull request
type PullRequest struct {
	ID           int    `json:"pullRequestId"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Status       string `json:"status"`
	URL          string `json:"url"`
	SourceBranch string `json:"sourceRefName"`
	TargetBranch string `json:"targetRefName"`
}

// CreatePRRequest represents a request to create a pull request
type CreatePRRequest struct {
	SourceBranch string
	TargetBranch string
	Title        string
	Description  string
	AutoComplete bool
}

// GetProjects returns all projects the user has access to
func (c *Client) GetProjects(ctx context.Context) ([]Project, error) {
	var allProjects []Project
	continuationToken := ""

	for {
		endpoint := "/_apis/projects?api-version=7.0"
		if continuationToken != "" {
			endpoint += "&continuationToken=" + continuationToken
		}

		resp, headers, err := c.doRequestWithHeaders(ctx, "GET", endpoint, nil)
		if err != nil {
			return nil, err
		}

		var result struct {
			Value []Project `json:"value"`
			Count int       `json:"count"`
		}

		if err := json.Unmarshal(resp, &result); err != nil {
			return nil, fmt.Errorf("failed to parse projects response: %w", err)
		}

		allProjects = append(allProjects, result.Value...)

		// Check for continuation token
		continuationToken = headers.Get("x-ms-continuationtoken")
		if continuationToken == "" {
			break
		}
	}

	return allProjects, nil
}

// GetRepositories returns all repositories in a project
func (c *Client) GetRepositories(ctx context.Context, projectID string) ([]Repository, error) {
	endpoint := fmt.Sprintf("/%s/_apis/git/repositories?api-version=7.0", projectID)

	resp, err := c.doRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Value []Repository `json:"value"`
		Count int          `json:"count"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse repositories response: %w", err)
	}

	return result.Value, nil
}

// GetRepositoryItems returns items (files/folders) at a given path
func (c *Client) GetRepositoryItems(ctx context.Context, projectID, repoID, path string) ([]RepositoryItem, error) {
	endpoint := fmt.Sprintf("/%s/_apis/git/repositories/%s/items?scopePath=%s&recursionLevel=OneLevel&api-version=7.0",
		projectID, repoID, url.QueryEscape(path))

	resp, err := c.doRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Value []RepositoryItem `json:"value"`
		Count int              `json:"count"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse items response: %w", err)
	}

	return result.Value, nil
}

// TerraformFile represents a Terraform file in a repository
type TerraformFile struct {
	Path     string
	ObjectID string
}

// GetTerraformFiles returns all Terraform files in a repository
func (c *Client) GetTerraformFiles(ctx context.Context, projectID, repoID string) ([]TerraformFile, error) {
	// Get all items recursively
	endpoint := fmt.Sprintf("/%s/_apis/git/repositories/%s/items?recursionLevel=Full&api-version=7.0", projectID, repoID)

	resp, err := c.doRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Value []RepositoryItem `json:"value"`
		Count int              `json:"count"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse items response: %w", err)
	}

	var tfFiles []TerraformFile
	for _, item := range result.Value {
		if item.GitObjectType == "blob" && strings.HasSuffix(item.Path, ".tf") {
			tfFiles = append(tfFiles, TerraformFile{
				Path:     item.Path,
				ObjectID: item.ObjectID,
			})
		}
	}

	return tfFiles, nil
}

// GetFileContent returns the content of a file in a repository
func (c *Client) GetFileContent(ctx context.Context, projectID, repoID, path string) (string, error) {
	endpoint := fmt.Sprintf("/%s/_apis/git/repositories/%s/items?path=%s&api-version=7.0",
		projectID, repoID, url.QueryEscape(path))

	resp, err := c.doRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", err
	}

	return string(resp), nil
}

// GetTags returns all tags in a repository, sorted by version (newest first)
func (c *Client) GetTags(ctx context.Context, projectID, repoID string) ([]string, error) {
	endpoint := fmt.Sprintf("/%s/_apis/git/repositories/%s/refs?filter=tags&api-version=7.0", projectID, repoID)

	resp, err := c.doRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Value []struct {
			Name     string `json:"name"`
			ObjectID string `json:"objectId"`
		} `json:"value"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tags response: %w", err)
	}

	var tags []string
	for _, ref := range result.Value {
		// Remove refs/tags/ prefix
		tag := strings.TrimPrefix(ref.Name, "refs/tags/")
		tags = append(tags, tag)
	}

	// Sort by semantic version (newest first)
	sortVersionsDescending(tags)

	return tags, nil
}

// GetDefaultBranchRef returns the default branch reference
func (c *Client) GetDefaultBranchRef(ctx context.Context, projectID, repoID string) (string, string, error) {
	endpoint := fmt.Sprintf("/%s/_apis/git/repositories/%s?api-version=7.0", projectID, repoID)

	resp, err := c.doRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", "", err
	}

	var repo Repository
	if err := json.Unmarshal(resp, &repo); err != nil {
		return "", "", fmt.Errorf("failed to parse repository response: %w", err)
	}

	// Get the commit ID for the default branch
	branchEndpoint := fmt.Sprintf("/%s/_apis/git/repositories/%s/refs?filter=heads/%s&api-version=7.0",
		projectID, repoID, strings.TrimPrefix(repo.DefaultBranch, "refs/heads/"))

	branchResp, err := c.doRequest(ctx, "GET", branchEndpoint, nil)
	if err != nil {
		return "", "", err
	}

	var branchResult struct {
		Value []struct {
			ObjectID string `json:"objectId"`
		} `json:"value"`
	}

	if err := json.Unmarshal(branchResp, &branchResult); err != nil {
		return "", "", fmt.Errorf("failed to parse branch response: %w", err)
	}

	if len(branchResult.Value) == 0 {
		return "", "", fmt.Errorf("default branch not found")
	}

	return repo.DefaultBranch, branchResult.Value[0].ObjectID, nil
}

// CreateBranchWithChanges creates a new branch with file changes
func (c *Client) CreateBranchWithChanges(ctx context.Context, projectID, repoID, branchName, baseBranch string, changes []FileChange, commitMessage string) error {
	// Get the base branch commit ID
	_, baseCommitID, err := c.GetDefaultBranchRef(ctx, projectID, repoID)
	if err != nil {
		return fmt.Errorf("failed to get base branch: %w", err)
	}

	// Prepare the push request
	var fileChanges []map[string]interface{}
	for _, change := range changes {
		changeEntry := map[string]interface{}{
			"changeType": change.ChangeType,
			"item": map[string]string{
				"path": change.Path,
			},
			"newContent": map[string]string{
				"content":     base64.StdEncoding.EncodeToString([]byte(change.Content)),
				"contentType": "base64encoded",
			},
		}
		fileChanges = append(fileChanges, changeEntry)
	}

	pushBody := map[string]interface{}{
		"refUpdates": []map[string]string{
			{
				"name":        "refs/heads/" + branchName,
				"oldObjectId": "0000000000000000000000000000000000000000",
			},
		},
		"commits": []map[string]interface{}{
			{
				"comment": commitMessage,
				"changes": fileChanges,
				"parents": []string{baseCommitID},
			},
		},
	}

	endpoint := fmt.Sprintf("/%s/_apis/git/repositories/%s/pushes?api-version=7.0", projectID, repoID)

	_, err = c.doRequest(ctx, "POST", endpoint, pushBody)
	if err != nil {
		return fmt.Errorf("failed to push changes: %w", err)
	}

	return nil
}

// CreatePullRequest creates a new pull request
func (c *Client) CreatePullRequest(ctx context.Context, projectID, repoID string, req CreatePRRequest) (*PullRequest, error) {
	body := map[string]interface{}{
		"sourceRefName": req.SourceBranch,
		"targetRefName": req.TargetBranch,
		"title":         req.Title,
		"description":   req.Description,
	}

	endpoint := fmt.Sprintf("/%s/_apis/git/repositories/%s/pullrequests?api-version=7.0", projectID, repoID)

	resp, err := c.doRequest(ctx, "POST", endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}

	var pr PullRequest
	if err := json.Unmarshal(resp, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse PR response: %w", err)
	}

	// Set auto-complete if requested
	if req.AutoComplete {
		updateBody := map[string]interface{}{
			"autoCompleteSetBy": map[string]string{
				"id": "me",
			},
		}

		updateEndpoint := fmt.Sprintf("/%s/_apis/git/repositories/%s/pullrequests/%d?api-version=7.0",
			projectID, repoID, pr.ID)

		_, err = c.doRequest(ctx, "PATCH", updateEndpoint, updateBody)
		if err != nil {
			// Log warning but don't fail
			fmt.Printf("Warning: failed to set auto-complete: %v\n", err)
		}
	}

	return &pr, nil
}

func (c *Client) doRequest(ctx context.Context, method, endpoint string, body interface{}) ([]byte, error) {
	resp, _, err := c.doRequestWithHeaders(ctx, method, endpoint, body)
	return resp, err
}

func (c *Client) doRequestWithHeaders(ctx context.Context, method, endpoint string, body interface{}) ([]byte, http.Header, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	url := c.baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set Basic Auth with PAT
	auth := base64.StdEncoding.EncodeToString([]byte(":" + c.token))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, resp.Header, nil
}
