//go:build unit

package dart_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dartUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/dart"
)

const fetcherTimeout = 5 * time.Second

// newJSONServer serves a fixed body, so the fetchers are exercised against a
// real HTTP server under test control rather than a transport double.
func newJSONServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestHTTPDartVersionFetcher(t *testing.T) {
	t.Parallel()

	t.Run("should return the version published on the stable channel", func(t *testing.T) {
		t.Parallel()

		// given
		server := newJSONServer(t, http.StatusOK,
			`{"date":"2026-08-05","version":"3.13.0","revision":"da6595cd"}`)
		fetcher := dartUpdater.NewHTTPDartVersionFetcherWithURL(
			&http.Client{Timeout: fetcherTimeout}, server.URL,
		)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.NoError(t, err)
		assert.Equal(t, "3.13.0", version)
	})

	t.Run("should return an error when the channel reports no version", func(t *testing.T) {
		t.Parallel()

		// given
		server := newJSONServer(t, http.StatusOK, `{"date":"2026-08-05"}`)
		fetcher := dartUpdater.NewHTTPDartVersionFetcherWithURL(
			&http.Client{Timeout: fetcherTimeout}, server.URL,
		)

		// when
		_, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.Error(t, err)
	})

	t.Run("should return an error on a non-200 response", func(t *testing.T) {
		t.Parallel()

		// given
		server := newJSONServer(t, http.StatusNotFound, `not found`)
		fetcher := dartUpdater.NewHTTPDartVersionFetcherWithURL(
			&http.Client{Timeout: fetcherTimeout}, server.URL,
		)

		// when
		_, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.Error(t, err)
	})
}

func TestHTTPFlutterVersionFetcher(t *testing.T) {
	t.Parallel()

	// The index lists every release ever published and is not ordered by
	// recency, so the stable entry has to be found by the hash current_release
	// points at. The beta entry below is deliberately listed first.
	const releasesIndex = `{
      "base_url": "https://storage.googleapis.com/flutter_infra_release/releases",
      "current_release": {"beta": "ceb9a865", "stable": "4cf24164"},
      "releases": [
        {"hash": "ceb9a865", "channel": "beta", "version": "3.48.0-1.0.pre", "dart_sdk_version": "3.14.0"},
        {"hash": "0000dead", "channel": "stable", "version": "3.35.0", "dart_sdk_version": "3.10.0"},
        {"hash": "4cf24164", "channel": "stable", "version": "3.47.0", "dart_sdk_version": "3.13.0"}
      ]
    }`

	t.Run("should return the version the current stable hash points at", func(t *testing.T) {
		t.Parallel()

		// given
		server := newJSONServer(t, http.StatusOK, releasesIndex)
		fetcher := dartUpdater.NewHTTPFlutterVersionFetcherWithURL(
			&http.Client{Timeout: fetcherTimeout}, server.URL,
		)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.NoError(t, err)
		assert.Equal(t, "3.47.0", version)
	})

	t.Run("should return an error when there is no current stable release", func(t *testing.T) {
		t.Parallel()

		// given
		server := newJSONServer(t, http.StatusOK,
			`{"current_release":{"beta":"ceb9a865"},"releases":[]}`)
		fetcher := dartUpdater.NewHTTPFlutterVersionFetcherWithURL(
			&http.Client{Timeout: fetcherTimeout}, server.URL,
		)

		// when
		_, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.Error(t, err)
	})

	t.Run("should return an error when the current stable hash is not listed", func(t *testing.T) {
		t.Parallel()

		// given
		server := newJSONServer(t, http.StatusOK,
			`{"current_release":{"stable":"missing"},"releases":[{"hash":"other","channel":"stable","version":"1.0.0"}]}`)
		fetcher := dartUpdater.NewHTTPFlutterVersionFetcherWithURL(
			&http.Client{Timeout: fetcherTimeout}, server.URL,
		)

		// when
		_, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.Error(t, err)
	})

	t.Run("should return an error on malformed JSON", func(t *testing.T) {
		t.Parallel()

		// given
		server := newJSONServer(t, http.StatusOK, `{`)
		fetcher := dartUpdater.NewHTTPFlutterVersionFetcherWithURL(
			&http.Client{Timeout: fetcherTimeout}, server.URL,
		)

		// when
		_, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.Error(t, err)
	})
}
