package javascript

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// VersionFetcher abstracts latest Node.js version resolution for testability.
type VersionFetcher interface {
	FetchLatestVersion(ctx context.Context) (string, error)
}

// defaultNodeVersionURL is the default URL for fetching Node.js release metadata.
const defaultNodeVersionURL = "https://nodejs.org/dist/index.json"

// HTTPNodeVersionFetcher fetches the latest LTS Node.js version from the official API.
type HTTPNodeVersionFetcher struct {
	client  *http.Client
	baseURL string
}

// NewHTTPNodeVersionFetcher creates a version fetcher with the given HTTP client.
func NewHTTPNodeVersionFetcher(client *http.Client) VersionFetcher {
	return &HTTPNodeVersionFetcher{client: client, baseURL: defaultNodeVersionURL}
}

// NewHTTPNodeVersionFetcherWithURL creates a version fetcher with a custom base URL (for testing).
func NewHTTPNodeVersionFetcherWithURL(client *http.Client, baseURL string) VersionFetcher {
	return &HTTPNodeVersionFetcher{client: client, baseURL: baseURL}
}

// FetchLatestVersion returns the latest LTS Node.js version string (e.g. "20.18.0").
func (f *HTTPNodeVersionFetcher) FetchLatestVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, f.baseURL, nil,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Node.js versions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var releases []nodeRelease
	if decodeErr := json.NewDecoder(resp.Body).Decode(&releases); decodeErr != nil {
		return "", fmt.Errorf("failed to parse Node.js versions: %w", decodeErr)
	}

	for _, release := range releases {
		if isLTSRelease(release) {
			return strings.TrimPrefix(release.Version, "v"), nil
		}
	}

	return "", errors.New("no LTS Node.js version found")
}
