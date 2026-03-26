//go:build unit

package javascript_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	jsUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/javascript"
)

func TestHTTPNodeVersionFetcher(t *testing.T) {
	t.Parallel()

	t.Run("should return latest LTS Node.js version when API responds with valid data", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			releases := []map[string]any{
				{"version": "v22.12.0", "lts": "Jod"},
				{"version": "v21.7.0", "lts": false},
				{"version": "v20.18.0", "lts": "Iron"},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()
		fetcher := jsUpdater.NewHTTPNodeVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.NoError(t, err)
		assert.Equal(t, "22.12.0", version)
	})

	t.Run("should skip non-LTS releases and return first LTS version", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			releases := []map[string]any{
				{"version": "v23.0.0", "lts": false},
				{"version": "v22.12.0", "lts": "Jod"},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()
		fetcher := jsUpdater.NewHTTPNodeVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.NoError(t, err)
		assert.Equal(t, "22.12.0", version)
	})

	t.Run("should return error when no LTS release is found", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			releases := []map[string]any{
				{"version": "v23.0.0", "lts": false},
				{"version": "v23.1.0", "lts": false},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()
		fetcher := jsUpdater.NewHTTPNodeVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no LTS Node.js version found")
		assert.Empty(t, version)
	})

	t.Run("should return error when server responds with non-200 status", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer server.Close()
		fetcher := jsUpdater.NewHTTPNodeVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected status code: 502")
		assert.Empty(t, version)
	})

	t.Run("should return error when response body contains malformed JSON", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("<<<invalid>>>"))
		}))
		defer server.Close()
		fetcher := jsUpdater.NewHTTPNodeVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse Node.js versions")
		assert.Empty(t, version)
	})

	t.Run("should return error when server is unreachable", func(t *testing.T) {
		t.Parallel()

		// given
		fetcher := jsUpdater.NewHTTPNodeVersionFetcherWithURL(http.DefaultClient, "http://127.0.0.1:0/nonexistent")

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch Node.js versions")
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
		fetcher := jsUpdater.NewHTTPNodeVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no LTS Node.js version found")
		assert.Empty(t, version)
	})
}
