package golang

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// VersionFetcher abstracts latest Go version resolution for testability.
type VersionFetcher interface {
	FetchLatestVersion(ctx context.Context) (string, error)
}

// defaultGoVersionURL is the default URL for fetching Go release metadata.
const defaultGoVersionURL = "https://go.dev/dl/?mode=json"

// HTTPGoVersionFetcher fetches the latest stable Go version from the official API.
type HTTPGoVersionFetcher struct {
	client  *http.Client
	baseURL string
}

// NewHTTPGoVersionFetcher creates a version fetcher with the given HTTP client.
func NewHTTPGoVersionFetcher(client *http.Client) VersionFetcher {
	return &HTTPGoVersionFetcher{client: client, baseURL: defaultGoVersionURL}
}

// NewHTTPGoVersionFetcherWithURL creates a version fetcher with a custom base URL (for testing).
func NewHTTPGoVersionFetcherWithURL(client *http.Client, baseURL string) VersionFetcher {
	return &HTTPGoVersionFetcher{client: client, baseURL: baseURL}
}

// FetchLatestVersion returns the latest stable Go version string (e.g. "1.25.7").
func (f *HTTPGoVersionFetcher) FetchLatestVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, f.baseURL, nil,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Go versions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var releases []goRelease
	if decodeErr := json.NewDecoder(resp.Body).Decode(&releases); decodeErr != nil {
		return "", fmt.Errorf("failed to parse Go versions: %w", decodeErr)
	}

	for _, release := range releases {
		if release.Stable {
			return strings.TrimPrefix(release.Version, "go"), nil
		}
	}

	return "", errors.New("no stable Go version found")
}
