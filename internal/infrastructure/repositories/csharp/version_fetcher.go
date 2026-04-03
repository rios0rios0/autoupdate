package csharp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// VersionFetcher abstracts latest .NET SDK version resolution for testability.
type VersionFetcher interface {
	FetchLatestVersion(ctx context.Context) (string, error)
}

// dotnetRelease represents a single .NET release cycle from the endoflife.date API.
type dotnetRelease struct {
	Cycle  string `json:"cycle"`
	Latest string `json:"latest"`
	EOL    any    `json:"eol"` // bool (false) or string date
}

// defaultDotnetVersionURL is the default URL for fetching .NET release metadata.
const defaultDotnetVersionURL = "https://endoflife.date/api/dotnet.json"

// HTTPDotnetVersionFetcher fetches the latest stable .NET SDK version from the endoflife.date API.
type HTTPDotnetVersionFetcher struct {
	client  *http.Client
	baseURL string
}

// NewHTTPDotnetVersionFetcher creates a version fetcher with the given HTTP client.
func NewHTTPDotnetVersionFetcher(client *http.Client) VersionFetcher {
	return &HTTPDotnetVersionFetcher{client: client, baseURL: defaultDotnetVersionURL}
}

// NewHTTPDotnetVersionFetcherWithURL creates a version fetcher with a custom base URL (for testing).
func NewHTTPDotnetVersionFetcherWithURL(client *http.Client, baseURL string) VersionFetcher {
	return &HTTPDotnetVersionFetcher{client: client, baseURL: baseURL}
}

// FetchLatestVersion returns the latest active .NET SDK version string (e.g. "8.0.11").
func (f *HTTPDotnetVersionFetcher) FetchLatestVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, f.baseURL, nil,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch .NET versions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var releases []dotnetRelease
	if decodeErr := json.NewDecoder(resp.Body).Decode(&releases); decodeErr != nil {
		return "", fmt.Errorf("failed to parse .NET versions: %w", decodeErr)
	}

	for _, release := range releases {
		if isActiveRelease(release) {
			return release.Latest, nil
		}
	}

	return "", errors.New("no active .NET release found")
}

// isActiveRelease returns true if the .NET release cycle has not reached
// end-of-life. The EOL field is false when still active, or a date string
// when it has an EOL date -- we check if that date is in the future.
func isActiveRelease(release dotnetRelease) bool {
	switch v := release.EOL.(type) {
	case bool:
		return !v
	case string:
		eolDate, err := time.Parse("2006-01-02", v)
		if err != nil {
			return false
		}
		return eolDate.After(time.Now())
	default:
		return false
	}
}
