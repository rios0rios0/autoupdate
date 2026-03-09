package versions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const fetchTimeout = 15 * time.Second

// VersionFetcher is a function that fetches the latest version of a language or tool.
type VersionFetcher func(ctx context.Context) (string, error)

// --- Go ---

type goRelease struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

// FetchLatestGoVersion fetches the latest stable Go version from the official API.
func FetchLatestGoVersion(ctx context.Context) (string, error) {
	var releases []goRelease
	if err := fetchJSON(ctx, "https://go.dev/dl/?mode=json", &releases); err != nil {
		return "", fmt.Errorf("failed to fetch Go versions: %w", err)
	}

	for _, release := range releases {
		if release.Stable {
			return strings.TrimPrefix(release.Version, "go"), nil
		}
	}

	return "", errors.New("no stable Go version found")
}

// --- Node.js ---

type nodeRelease struct {
	Version string `json:"version"`
	LTS     any    `json:"lts"` // false or string like "Jod"
}

// FetchLatestNodeVersion fetches the latest LTS Node.js version.
func FetchLatestNodeVersion(ctx context.Context) (string, error) {
	var releases []nodeRelease
	if err := fetchJSON(ctx, "https://nodejs.org/dist/index.json", &releases); err != nil {
		return "", fmt.Errorf("failed to fetch Node.js versions: %w", err)
	}

	for _, release := range releases {
		if isLTSRelease(release) {
			return strings.TrimPrefix(release.Version, "v"), nil
		}
	}

	return "", errors.New("no LTS Node.js version found")
}

func isLTSRelease(release nodeRelease) bool {
	switch v := release.LTS.(type) {
	case string:
		return v != ""
	case bool:
		return v
	default:
		return false
	}
}

// --- endoflife.date shared ---

// eolRelease represents a release from the endoflife.date API.
// Used by Python, Java, and Terraform fetchers.
type eolRelease struct {
	Cycle  string `json:"cycle"`
	Latest string `json:"latest"`
	EOL    any    `json:"eol"` // bool (false) or string date
	LTS    bool   `json:"lts"`
}

// fetchEndOfLifeLatest queries the endoflife.date API and returns the latest
// version of the first release that passes the filter function.
func fetchEndOfLifeLatest(
	ctx context.Context, apiURL, name string, filter func(r eolRelease) bool,
) (string, error) {
	var releases []eolRelease
	if err := fetchJSON(ctx, apiURL, &releases); err != nil {
		return "", fmt.Errorf("failed to fetch %s versions: %w", name, err)
	}

	for _, release := range releases {
		if filter(release) {
			return release.Latest, nil
		}
	}

	return "", fmt.Errorf("no active %s release found", name)
}

// --- Python ---

// FetchLatestPythonVersion fetches the latest active Python version.
func FetchLatestPythonVersion(ctx context.Context) (string, error) {
	return fetchEndOfLifeLatest(ctx, "https://endoflife.date/api/python.json", "Python",
		func(r eolRelease) bool { return isActiveEOL(r.EOL) },
	)
}

// --- Java ---

// FetchLatestJavaVersion fetches the latest LTS Java version.
func FetchLatestJavaVersion(ctx context.Context) (string, error) {
	return fetchEndOfLifeLatest(ctx, "https://endoflife.date/api/java.json", "Java",
		func(r eolRelease) bool { return r.LTS && isActiveEOL(r.EOL) },
	)
}

// --- Terraform ---

// FetchLatestTerraformVersion fetches the latest active Terraform version.
func FetchLatestTerraformVersion(ctx context.Context) (string, error) {
	return fetchEndOfLifeLatest(ctx, "https://endoflife.date/api/terraform.json", "Terraform",
		func(r eolRelease) bool { return isActiveEOL(r.EOL) },
	)
}

// --- Helpers ---

// fetchJSON performs an HTTP GET and decodes the JSON response into target.
func fetchJSON(ctx context.Context, url string, target any) error {
	client := &http.Client{Timeout: fetchTimeout}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

// isActiveEOL returns true if the endoflife.date EOL field indicates
// the release is still active. The field is false when active, or a date
// string when it has an EOL date.
func isActiveEOL(eol any) bool {
	switch v := eol.(type) {
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
