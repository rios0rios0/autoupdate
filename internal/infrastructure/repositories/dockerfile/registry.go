package dockerfile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	registryTimeout = 15 * time.Second
	maxTagPages     = 5 // fetch up to 5 pages of tags (500 tags)
)

// parsedImageRef represents a parsed Docker FROM clause image reference.
type parsedImageRef struct {
	Namespace string // "" for official images, "bitnami" for bitnami/redis, etc.
	Image     string // "golang", "python", "alpine", etc.
	Tag       string // full tag: "1.25.7", "3.13-slim-bullseye"
	Version   string // parsed version part: "1.25.7", "3.13"
	Suffix    string // parsed suffix part: "", "-slim-bullseye"
	Precision int    // number of version parts: 2 for MAJOR.MINOR, 3 for MAJOR.MINOR.PATCH
}

// FullName returns the fully qualified image name (e.g., "golang" or "bitnami/redis").
func (p *parsedImageRef) FullName() string {
	if p.Namespace == "" {
		return p.Image
	}
	return p.Namespace + "/" + p.Image
}

// --- Docker Hub API ---

type dockerTagResult struct {
	Name string `json:"name"`
}

type dockerTagsResponse struct {
	Results []dockerTagResult `json:"results"`
	Next    string            `json:"next"`
}

// fetchTags queries Docker Hub for available tags of an image.
// It paginates through up to maxTagPages pages of results.
func fetchTags(ctx context.Context, ref *parsedImageRef) ([]string, error) {
	var apiURL string
	if ref.Namespace == "" {
		apiURL = fmt.Sprintf(
			"https://hub.docker.com/v2/repositories/library/%s/tags/?page_size=100&ordering=last_updated",
			ref.Image,
		)
	} else {
		apiURL = fmt.Sprintf(
			"https://hub.docker.com/v2/repositories/%s/%s/tags/?page_size=100&ordering=last_updated",
			ref.Namespace, ref.Image,
		)
	}

	client := &http.Client{Timeout: registryTimeout}
	var tags []string

	for page := 0; page < maxTagPages && apiURL != ""; page++ {
		pageTags, nextURL, err := fetchTagPage(ctx, client, apiURL)
		if err != nil {
			return nil, err
		}
		tags = append(tags, pageTags...)
		apiURL = nextURL
	}

	return tags, nil
}

// fetchTagPage fetches a single page of tags from Docker Hub.
func fetchTagPage(ctx context.Context, client *http.Client, apiURL string) ([]string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch tags: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var tagsResp dockerTagsResponse
	if decodeErr := json.NewDecoder(resp.Body).Decode(&tagsResp); decodeErr != nil {
		return nil, "", fmt.Errorf("failed to parse tags response: %w", decodeErr)
	}

	tags := make([]string, 0, len(tagsResp.Results))
	for _, result := range tagsResp.Results {
		tags = append(tags, result.Name)
	}

	return tags, tagsResp.Next, nil
}

// --- tag parsing ---

// versionPartPattern matches the leading version digits in a tag.
var versionPartPattern = regexp.MustCompile(`^(\d+(?:\.\d+)*)(.*)$`)

// parseTag splits a Docker tag into version and suffix components.
// For example: "3.13-slim-bullseye" -> ("3.13", "-slim-bullseye", 2, true).
func parseTag(tag string) (version, suffix string, precision int, ok bool) {
	m := versionPartPattern.FindStringSubmatch(tag)
	if len(m) < 3 { //nolint:mnd // need full match + 2 capture groups
		return "", "", 0, false
	}

	version = m[1]
	suffix = m[2]
	precision = len(strings.Split(version, "."))

	// Validate that the version part is semver-like
	normalized := "v" + version
	if precision < 3 { //nolint:mnd // pad to 3-part for semver validation
		for i := precision; i < 3; i++ {
			normalized += ".0"
		}
	}

	if !semver.IsValid(normalized) {
		return "", "", 0, false
	}

	return version, suffix, precision, true
}

// findBestUpgrade finds the highest compatible tag for a given current reference.
// It matches tags with the same suffix and upgrades within the same major version.
func findBestUpgrade(current *parsedImageRef, availableTags []string) string {
	var bestVersion string
	var bestTag string

	for _, tag := range availableTags {
		version, tagSuffix, tagPrecision, ok := parseTag(tag)
		if !ok {
			continue
		}

		// Must match the same suffix
		if tagSuffix != current.Suffix {
			continue
		}

		// Must match the same precision level
		if tagPrecision != current.Precision {
			continue
		}

		// Compare versions using semver
		curNorm := normalizeToSemver(current.Version)
		newNorm := normalizeToSemver(version)

		if !semver.IsValid(curNorm) || !semver.IsValid(newNorm) {
			continue
		}

		// Must be newer
		if semver.Compare(newNorm, curNorm) <= 0 {
			continue
		}

		// Must be within the same major version
		if semver.Major(newNorm) != semver.Major(curNorm) {
			continue
		}

		// For patch-pinned versions (3 parts), stay within same minor
		if current.Precision >= 3 { //nolint:mnd // 3-part = patch-pinned
			curMinor := semver.Major(curNorm) + "." + extractMinor(curNorm)
			newMinor := semver.Major(newNorm) + "." + extractMinor(newNorm)
			if curMinor != newMinor {
				continue
			}
		}

		// Track the highest version found
		if bestVersion == "" || semver.Compare(newNorm, normalizeToSemver(bestVersion)) > 0 {
			bestVersion = version
			bestTag = tag
		}
	}

	return bestTag
}

// normalizeToSemver ensures a version string is valid semver by prepending "v"
// and padding to 3 parts if needed.
func normalizeToSemver(version string) string {
	v := version
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}

	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	for len(parts) < 3 { //nolint:mnd // semver requires 3 parts
		parts = append(parts, "0")
	}

	return "v" + strings.Join(parts, ".")
}

// extractMinor extracts the minor version number from a semver string.
func extractMinor(v string) string {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) >= 2 { //nolint:mnd // need at least major.minor
		return parts[1]
	}
	return "0"
}
