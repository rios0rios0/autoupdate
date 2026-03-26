package python

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// VersionFetcher abstracts latest Python version resolution for testability.
type VersionFetcher interface {
	FetchLatestVersion(ctx context.Context) (string, error)
}

// pythonRelease represents a single Python release cycle from the endoflife.date API.
type pythonRelease struct {
	Cycle  string `json:"cycle"`
	Latest string `json:"latest"`
	EOL    any    `json:"eol"` // bool (false) or string date
}

// HTTPPythonVersionFetcher fetches the latest stable Python version from the endoflife.date API.
type HTTPPythonVersionFetcher struct {
	client *http.Client
}

// NewHTTPPythonVersionFetcher creates a version fetcher with the given HTTP client.
func NewHTTPPythonVersionFetcher(client *http.Client) VersionFetcher {
	return &HTTPPythonVersionFetcher{client: client}
}

// FetchLatestVersion returns the latest stable Python version string (e.g. "3.13.1").
func (f *HTTPPythonVersionFetcher) FetchLatestVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, "https://endoflife.date/api/python.json", nil,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Python versions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var releases []pythonRelease
	if decodeErr := json.NewDecoder(resp.Body).Decode(&releases); decodeErr != nil {
		return "", fmt.Errorf("failed to parse Python versions: %w", decodeErr)
	}

	for _, release := range releases {
		if isActiveRelease(release) {
			return release.Latest, nil
		}
	}

	return "", errors.New("no active Python release found")
}

// isActiveRelease returns true if the Python release cycle has not reached
// end-of-life. The EOL field is false when still active, or a date string
// when it has an EOL date -- we check if that date is in the future.
func isActiveRelease(release pythonRelease) bool {
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
