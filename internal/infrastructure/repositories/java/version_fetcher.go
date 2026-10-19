package java

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
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

// defaultJavaVersionURL is endoflife.date's Eclipse Temurin feed. Its "java"
// product, which the updater read before, no longer exists and answers 404.
const defaultJavaVersionURL = "https://endoflife.date/api/eclipse-temurin.json"

// temurinReleaseName matches the name of an Eclipse Temurin release and captures
// the JDK version it packages: up to three numbers, the feature, interim and
// update releases. Temurin appends a respin number when it rebuilds a JDK, and a
// build number after a "+": "25.0.4.1+1" is JDK 25.0.4, and "27+35" is JDK 27.
var temurinReleaseName = regexp.MustCompile(`^(\d+(?:\.\d+){0,2})(?:\.\d+)*(?:\+.+)?$`)

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

// FetchLatestVersion returns the JDK version of the newest active LTS line (e.g.
// "25.0.4"), the value the updater writes into .java-version.
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

	release, found := newestActiveLTSRelease(releases)
	if !found {
		return "", errors.New("no active LTS Java release found")
	}

	return jdkVersion(release.Latest)
}

// newestActiveLTSRelease returns the active LTS release with the highest cycle. The
// feed does not list the cycles in order (it puts 11 before 16), so the first
// active LTS release it lists is not necessarily the newest.
func newestActiveLTSRelease(releases []javaRelease) (javaRelease, bool) {
	var newest javaRelease
	newestCycle := -1
	for _, release := range releases {
		cycle, err := strconv.Atoi(release.Cycle)
		if err != nil || cycle <= newestCycle || !isLTSRelease(release) || !isActiveRelease(release) {
			continue
		}
		newest, newestCycle = release, cycle
	}

	return newest, newestCycle >= 0
}

// jdkVersion returns the JDK version an Eclipse Temurin release packages, e.g.
// "25.0.4" for "25.0.4.1+1". The respin and build numbers do not belong in
// .java-version: version managers name JDKs by MAJOR.MINOR.PATCH, and the branch
// name and the commit message are built from the same value.
func jdkVersion(releaseName string) (string, error) {
	match := temurinReleaseName.FindStringSubmatch(releaseName)
	if match == nil {
		return "", fmt.Errorf("unrecognized Java release %q", releaseName)
	}

	return match[1], nil
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
