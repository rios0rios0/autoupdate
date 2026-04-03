package java

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// VersionFetcher abstracts latest Java version resolution for testability.
type VersionFetcher interface {
	FetchLatestVersion(ctx context.Context) (string, error)
}

// javaRelease represents a single Java release cycle from the endoflife.date API.
type javaRelease struct {
	Cycle  string `json:"cycle"`
	Latest string `json:"latest"`
	LTS    any    `json:"lts"` // bool (false) or string date
	EOL    any    `json:"eol"` // bool (false) or string date
}

// defaultJavaVersionURL is the default URL for fetching Java release metadata.
const defaultJavaVersionURL = "https://endoflife.date/api/java.json"

// HTTPJavaVersionFetcher fetches the latest LTS Java version from the endoflife.date API.
type HTTPJavaVersionFetcher struct {
	client  *http.Client
	baseURL string
}

// NewHTTPJavaVersionFetcher creates a version fetcher with the given HTTP client.
func NewHTTPJavaVersionFetcher(client *http.Client) VersionFetcher {
	return &HTTPJavaVersionFetcher{client: client, baseURL: defaultJavaVersionURL}
}

// NewHTTPJavaVersionFetcherWithURL creates a version fetcher with a custom base URL (for testing).
func NewHTTPJavaVersionFetcherWithURL(client *http.Client, baseURL string) VersionFetcher {
	return &HTTPJavaVersionFetcher{client: client, baseURL: baseURL}
}

// FetchLatestVersion returns the latest LTS Java version string (e.g. "21.0.5").
func (f *HTTPJavaVersionFetcher) FetchLatestVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, f.baseURL, nil,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Java versions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var releases []javaRelease
	if decodeErr := json.NewDecoder(resp.Body).Decode(&releases); decodeErr != nil {
		return "", fmt.Errorf("failed to parse Java versions: %w", decodeErr)
	}

	for _, release := range releases {
		if isLTSRelease(release) && isActiveRelease(release) {
			return release.Latest, nil
		}
	}

	return "", errors.New("no active LTS Java release found")
}

// isLTSRelease returns true if the Java release is an LTS version.
// The LTS field is false for non-LTS releases and a date string for LTS releases.
func isLTSRelease(release javaRelease) bool {
	switch v := release.LTS.(type) {
	case bool:
		return v
	case string:
		// A non-empty date string means it is an LTS release
		return v != ""
	default:
		return false
	}
}

// isActiveRelease returns true if the Java release cycle has not reached
// end-of-life. The EOL field is false when still active, or a date string
// when it has an EOL date -- we check if that date is in the future.
func isActiveRelease(release javaRelease) bool {
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
