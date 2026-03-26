//go:build unit

package golang_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	goUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
)

func TestHTTPGoVersionFetcher(t *testing.T) {
	t.Parallel()

	t.Run("should return latest stable Go version when API responds with valid data", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			releases := []map[string]any{
				{"version": "go1.25.7", "stable": true},
				{"version": "go1.25.6", "stable": true},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()
		fetcher := goUpdater.NewHTTPGoVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.NoError(t, err)
		assert.Equal(t, "1.25.7", version)
	})

	t.Run("should skip unstable releases and return first stable version", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			releases := []map[string]any{
				{"version": "go1.26rc1", "stable": false},
				{"version": "go1.26beta2", "stable": false},
				{"version": "go1.25.7", "stable": true},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()
		fetcher := goUpdater.NewHTTPGoVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.NoError(t, err)
		assert.Equal(t, "1.25.7", version)
	})

	t.Run("should return error when no stable release is found", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			releases := []map[string]any{
				{"version": "go1.26rc1", "stable": false},
				{"version": "go1.26beta1", "stable": false},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()
		fetcher := goUpdater.NewHTTPGoVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no stable Go version found")
		assert.Empty(t, version)
	})

	t.Run("should return error when server responds with non-200 status", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()
		fetcher := goUpdater.NewHTTPGoVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected status code: 500")
		assert.Empty(t, version)
	})

	t.Run("should return error when response body contains malformed JSON", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("not valid json"))
		}))
		defer server.Close()
		fetcher := goUpdater.NewHTTPGoVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse Go versions")
		assert.Empty(t, version)
	})

	t.Run("should return error when server is unreachable", func(t *testing.T) {
		t.Parallel()

		// given
		fetcher := goUpdater.NewHTTPGoVersionFetcherWithURL(http.DefaultClient, "http://127.0.0.1:0/nonexistent")

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch Go versions")
		assert.Empty(t, version)
	})

	t.Run("should return error when release list is empty", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		}))
		defer server.Close()
		fetcher := goUpdater.NewHTTPGoVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no stable Go version found")
		assert.Empty(t, version)
	})
}
