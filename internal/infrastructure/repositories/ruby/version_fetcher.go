package ruby

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// VersionFetcher abstracts latest Ruby version resolution for testability.
type VersionFetcher interface {
	FetchLatestVersion(ctx context.Context) (string, error)
}

// rubyRelease represents a single Ruby release cycle from the endoflife.date API.
type rubyRelease struct {
	Cycle  string `json:"cycle"`
	Latest string `json:"latest"`
	EOL    any    `json:"eol"` // bool (false) or string date
}

// defaultRubyVersionURL is the default URL for fetching Ruby release metadata.
const defaultRubyVersionURL = "https://endoflife.date/api/ruby.json"

// HTTPRubyVersionFetcher fetches the latest stable Ruby version from the endoflife.date API.
type HTTPRubyVersionFetcher struct {
	client  *http.Client
	baseURL string
}

// NewHTTPRubyVersionFetcher creates a version fetcher with the given HTTP client.
func NewHTTPRubyVersionFetcher(client *http.Client) VersionFetcher {
	return &HTTPRubyVersionFetcher{client: client, baseURL: defaultRubyVersionURL}
}

// NewHTTPRubyVersionFetcherWithURL creates a version fetcher with a custom base URL (for testing).
func NewHTTPRubyVersionFetcherWithURL(client *http.Client, baseURL string) VersionFetcher {
	return &HTTPRubyVersionFetcher{client: client, baseURL: baseURL}
}

// FetchLatestVersion returns the latest stable Ruby version string (e.g. "3.3.6").
func (f *HTTPRubyVersionFetcher) FetchLatestVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, f.baseURL, nil,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Ruby versions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var releases []rubyRelease
	if decodeErr := json.NewDecoder(resp.Body).Decode(&releases); decodeErr != nil {
		return "", fmt.Errorf("failed to parse Ruby versions: %w", decodeErr)
	}

	for _, release := range releases {
		if isActiveRelease(release) {
			return release.Latest, nil
		}
	}

	return "", errors.New("no active Ruby release found")
}

// isActiveRelease returns true if the Ruby release cycle has not reached
// end-of-life. The EOL field is false when still active, or a date string
// when it has an EOL date -- we check if that date is in the future.
func isActiveRelease(release rubyRelease) bool {
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
