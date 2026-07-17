package golang

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	logger "github.com/sirupsen/logrus"
	"golang.org/x/mod/semver"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
)

const (
	dockerHubTimeout      = 15 * time.Second
	dockerHubMaxPages     = 5   // fetch up to 5 pages of tags (500 tags)
	dockerHubPageSize     = 100 // Docker Hub max page size
	golangImage           = "golang"
	golangVersionMinParts = 3 // MAJOR.MINOR.PATCH after padding
)

// golangFromPattern matches the tag of an official `golang` base image in a
// Dockerfile FROM clause, e.g. `FROM golang:1.24-alpine`. Group 1 is the
// literal prefix up to and including `golang:`; group 2 is the tag. Only the
// official Docker Hub `golang` image is matched — a registry- or namespace-
// qualified `.../golang:` is intentionally excluded because its tags cannot be
// verified against Docker Hub's `library` namespace.
var golangFromPattern = regexp.MustCompile(
	`(?im)^(FROM\s+(?:--platform=\S+\s+)?golang:)([A-Za-z0-9][A-Za-z0-9._-]*)`,
)

// golangVersionPattern splits a golang tag into its leading version and the
// remaining suffix, e.g. `1.24.5-alpine3.20` -> (`1.24.5`, `-alpine3.20`).
var golangVersionPattern = regexp.MustCompile(`^(\d+(?:\.\d+)*)(.*)$`)

// golangTagLister returns the published `golang` tags from a registry. It is a
// function type so callers inject the Docker Hub adapter and tests inject a
// hand-rolled lister without touching global state.
type golangTagLister func(ctx context.Context) ([]string, error)

// updateDockerfileGolangTags scans every Dockerfile under repoDir and rewrites
// each official `golang:` base image to the tag matching goVersion — but only
// after verifying that tag actually exists on Docker Hub. When the exact
// `goVersion`+suffix image is not published (registry lag, or a dropped Alpine
// variant such as `golang:1.25.7-alpine3.20`), it falls back to the closest
// existing patch within the same minor+suffix; when nothing suitable exists it
// leaves the clause untouched so the build is never pointed at a non-existent
// image. Returns whether any file was changed.
func updateDockerfileGolangTags(
	ctx context.Context,
	repoDir, goVersion string,
	listTags golangTagLister,
) (bool, error) {
	files, err := support.WalkFilesByPredicate(repoDir, isDockerfileName)
	if err != nil {
		return false, fmt.Errorf("failed to walk Dockerfiles: %w", err)
	}

	var available []string
	fetched := false
	var changes []entities.FileChange

	for _, rel := range files {
		data, readErr := os.ReadFile(filepath.Join(repoDir, rel))
		if readErr != nil {
			logger.Warnf("[golang] Failed to read %s: %v", rel, readErr)
			continue
		}
		content := string(data)
		if !golangFromPattern.MatchString(content) {
			continue
		}

		// Fetch the tag list lazily — only when a Dockerfile actually
		// references the official golang image.
		if !fetched {
			available, err = listTags(ctx)
			if err != nil {
				return false, fmt.Errorf("failed to fetch golang tags: %w", err)
			}
			fetched = true
		}

		if updated := rewriteGolangTags(content, goVersion, available, rel); updated != content {
			changes = append(changes, entities.FileChange{
				Path:       rel,
				Content:    updated,
				ChangeType: "edit",
			})
		}
	}

	if len(changes) == 0 {
		return false, nil
	}

	if writeErr := support.WriteFileChanges(repoDir, changes); writeErr != nil {
		return false, writeErr
	}
	return true, nil
}

// rewriteGolangTags replaces every official `golang:` tag in content with the
// closest published tag for goVersion, preserving the original suffix and never
// downgrading. Clauses whose target image does not exist are left untouched.
func rewriteGolangTags(content, goVersion string, available []string, relPath string) string {
	return golangFromPattern.ReplaceAllStringFunc(content, func(match string) string {
		groups := golangFromPattern.FindStringSubmatch(match)
		prefix, currentTag := groups[1], groups[2]

		currentVersion, suffix, ok := parseGolangTag(currentTag)
		if !ok {
			return match // not a pinned numeric tag (e.g. "latest", "alpine")
		}

		newTag, resolved := resolveClosestGolangTag(available, goVersion, suffix)
		if !resolved {
			logger.Warnf(
				"[golang] %s: no published golang image for Go %s with suffix %q; leaving golang:%s unchanged",
				relPath, goVersion, suffix, currentTag,
			)
			return match
		}

		// Never downgrade: only rewrite when the resolved version is newer than
		// the version currently pinned in the Dockerfile.
		newVersion, _, _ := parseGolangTag(newTag)
		if semver.Compare(normalizeGoVersion(newVersion), normalizeGoVersion(currentVersion)) <= 0 {
			return match
		}

		logger.Infof("[golang] %s: upgrading golang:%s -> golang:%s", relPath, currentTag, newTag)
		return prefix + newTag
	})
}

// resolveClosestGolangTag returns the best published golang tag for goVersion
// while preserving suffix. It prefers the exact `goVersion+suffix` tag; when
// that image is not published it falls back to the highest published patch in
// the same minor+suffix that does not exceed goVersion. It returns ("", false)
// when no suitable published tag exists, signalling the caller to leave the
// Dockerfile clause unchanged rather than point it at a non-existent image.
func resolveClosestGolangTag(available []string, goVersion, suffix string) (string, bool) {
	desired := goVersion + suffix
	if slices.Contains(available, desired) {
		return desired, true
	}

	target := normalizeGoVersion(goVersion)
	targetMinor := semver.MajorMinor(target)

	bestTag := ""
	bestNorm := ""
	for _, tag := range available {
		version, tagSuffix, ok := parseGolangTag(tag)
		if !ok || tagSuffix != suffix {
			continue
		}

		norm := normalizeGoVersion(version)
		if semver.MajorMinor(norm) != targetMinor {
			continue // stay within the target minor to avoid cross-version drift
		}
		if semver.Compare(norm, target) > 0 {
			continue // never exceed the target Go version
		}
		if bestTag == "" || semver.Compare(norm, bestNorm) > 0 {
			bestTag, bestNorm = tag, norm
		}
	}

	if bestTag != "" {
		return bestTag, true
	}
	return "", false
}

// parseGolangTag splits a golang tag into its leading version and suffix,
// returning ok=false when the tag is not version-pinned (e.g. "latest").
func parseGolangTag(tag string) (string, string, bool) {
	m := golangVersionPattern.FindStringSubmatch(tag)
	if len(m) < golangVersionMinParts { // full match + 2 capture groups
		return "", "", false
	}

	version, suffix := m[1], m[2]
	if !semver.IsValid(normalizeGoVersion(version)) {
		return "", "", false
	}
	return version, suffix, true
}

// normalizeGoVersion turns a bare version like "1.25" into valid semver
// ("v1.25.0") so golang.org/x/mod/semver can compare it.
func normalizeGoVersion(version string) string {
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	for len(parts) < golangVersionMinParts {
		parts = append(parts, "0")
	}
	return "v" + strings.Join(parts, ".")
}

// isDockerfileName returns true if a file's base name looks like a Dockerfile.
func isDockerfileName(name string) bool {
	return name == "Dockerfile" ||
		strings.HasPrefix(name, "Dockerfile.") ||
		strings.HasSuffix(name, ".Dockerfile")
}

// --- Docker Hub tag fetching ---

type dockerHubTagResult struct {
	Name string `json:"name"`
}

type dockerHubTagsResponse struct {
	Results []dockerHubTagResult `json:"results"`
	Next    string               `json:"next"`
}

// fetchGolangTags queries Docker Hub for the published tags of the official
// `golang` image, paginating through up to dockerHubMaxPages pages.
func fetchGolangTags(ctx context.Context) ([]string, error) {
	apiURL := fmt.Sprintf(
		"https://hub.docker.com/v2/repositories/library/%s/tags/?page_size=%d&ordering=last_updated",
		golangImage, dockerHubPageSize,
	)

	client := &http.Client{Timeout: dockerHubTimeout}
	var tags []string

	for page := 0; page < dockerHubMaxPages && apiURL != ""; page++ {
		pageTags, next, err := fetchGolangTagPage(ctx, client, apiURL)
		if err != nil {
			return nil, err
		}
		tags = append(tags, pageTags...)
		apiURL = next
	}

	return tags, nil
}

// fetchGolangTagPage fetches a single page of golang tags from Docker Hub.
func fetchGolangTagPage(
	ctx context.Context,
	client *http.Client,
	apiURL string,
) ([]string, string, error) {
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

	var body dockerHubTagsResponse
	if decErr := json.NewDecoder(resp.Body).Decode(&body); decErr != nil {
		return nil, "", fmt.Errorf("failed to parse tags response: %w", decErr)
	}

	tags := make([]string, 0, len(body.Results))
	for _, result := range body.Results {
		tags = append(tags, result.Name)
	}

	return tags, body.Next, nil
}
