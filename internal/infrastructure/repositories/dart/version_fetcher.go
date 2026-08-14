package dart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// VersionFetcher abstracts latest SDK version resolution for testability.
type VersionFetcher interface {
	FetchLatestVersion(ctx context.Context) (string, error)
}

// Neither Dart nor Flutter has an endoflife.date product, so the versions come
// straight from Google's own release channels — which are the canonical source
// the installers themselves read.
const (
	// defaultDartVersionURL serves the stable channel's release metadata.
	defaultDartVersionURL = "https://storage.googleapis.com/dart-archive/channels/stable/release/latest/VERSION"

	// defaultFlutterVersionURL serves every published release plus a pointer to
	// the current one per channel.
	defaultFlutterVersionURL = "https://storage.googleapis.com/flutter_infra_release/releases/releases_linux.json"

	// stableChannel is the only channel autoupdate follows: a scheduled
	// dependency bump must never move a repository onto beta or master.
	stableChannel = "stable"
)

// dartRelease is the stable channel's VERSION document.
type dartRelease struct {
	Version  string `json:"version"`
	Date     string `json:"date"`
	Revision string `json:"revision"`
}

// flutterReleases is the releases index published beside the Flutter archives.
type flutterReleases struct {
	CurrentRelease map[string]string `json:"current_release"`
	Releases       []flutterRelease  `json:"releases"`
}

// flutterRelease is a single published Flutter build.
type flutterRelease struct {
	Hash           string `json:"hash"`
	Channel        string `json:"channel"`
	Version        string `json:"version"`
	DartSDKVersion string `json:"dart_sdk_version"`
}

// HTTPDartVersionFetcher fetches the latest stable Dart SDK version.
type HTTPDartVersionFetcher struct {
	client  *http.Client
	baseURL string
}

// NewHTTPDartVersionFetcher creates a Dart version fetcher with the given client.
func NewHTTPDartVersionFetcher(client *http.Client) VersionFetcher {
	return &HTTPDartVersionFetcher{client: client, baseURL: defaultDartVersionURL}
}

// NewHTTPDartVersionFetcherWithURL creates a Dart version fetcher with a custom base URL (for testing).
func NewHTTPDartVersionFetcherWithURL(client *http.Client, baseURL string) VersionFetcher {
	return &HTTPDartVersionFetcher{client: client, baseURL: baseURL}
}

// FetchLatestVersion returns the latest stable Dart SDK version (e.g. "3.13.0").
func (f *HTTPDartVersionFetcher) FetchLatestVersion(ctx context.Context) (string, error) {
	var release dartRelease
	if err := fetchJSON(ctx, f.client, f.baseURL, &release); err != nil {
		return "", fmt.Errorf("failed to fetch Dart versions: %w", err)
	}
	if release.Version == "" {
		return "", errors.New("no Dart version found in the stable channel")
	}
	return release.Version, nil
}

// HTTPFlutterVersionFetcher fetches the latest stable Flutter SDK version.
type HTTPFlutterVersionFetcher struct {
	client  *http.Client
	baseURL string
}

// NewHTTPFlutterVersionFetcher creates a Flutter version fetcher with the given client.
func NewHTTPFlutterVersionFetcher(client *http.Client) VersionFetcher {
	return &HTTPFlutterVersionFetcher{client: client, baseURL: defaultFlutterVersionURL}
}

// NewHTTPFlutterVersionFetcherWithURL creates a Flutter version fetcher with a custom base URL (for testing).
func NewHTTPFlutterVersionFetcherWithURL(client *http.Client, baseURL string) VersionFetcher {
	return &HTTPFlutterVersionFetcher{client: client, baseURL: baseURL}
}

// FetchLatestVersion returns the latest stable Flutter SDK version (e.g. "3.47.0").
//
// The index lists every release ever published, so the stable entry is found by
// the hash current_release points at rather than by taking the first match —
// the list is not ordered by recency.
func (f *HTTPFlutterVersionFetcher) FetchLatestVersion(ctx context.Context) (string, error) {
	var index flutterReleases
	if err := fetchJSON(ctx, f.client, f.baseURL, &index); err != nil {
		return "", fmt.Errorf("failed to fetch Flutter versions: %w", err)
	}

	hash, ok := index.CurrentRelease[stableChannel]
	if !ok || hash == "" {
		return "", errors.New("no current stable Flutter release found")
	}
	for _, release := range index.Releases {
		if release.Hash == hash && release.Channel == stableChannel {
			return release.Version, nil
		}
	}

	return "", errors.New("current stable Flutter release is not listed")
}

// fetchJSON performs the GET and decodes the body into out.
func fetchJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	if decodeErr := json.NewDecoder(resp.Body).Decode(out); decodeErr != nil {
		return fmt.Errorf("failed to parse response: %w", decodeErr)
	}
	return nil
}
