//go:build unit

package python_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pyUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/python"
)

func TestHTTPPythonVersionFetcher(t *testing.T) {
	t.Parallel()

	t.Run("should return latest active Python version when API responds with valid data", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			releases := []map[string]any{
				{"cycle": "3.13", "latest": "3.13.1", "eol": false},
				{"cycle": "3.12", "latest": "3.12.8", "eol": false},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()
		fetcher := pyUpdater.NewHTTPPythonVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.NoError(t, err)
		assert.Equal(t, "3.13.1", version)
	})

	t.Run("should skip EOL releases and return first active version", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			releases := []map[string]any{
				{"cycle": "3.7", "latest": "3.7.17", "eol": true},
				{"cycle": "3.13", "latest": "3.13.1", "eol": false},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()
		fetcher := pyUpdater.NewHTTPPythonVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.NoError(t, err)
		assert.Equal(t, "3.13.1", version)
	})

	t.Run("should handle EOL as a future date string and treat as active", func(t *testing.T) {
		t.Parallel()

		// given
		futureDate := time.Now().AddDate(2, 0, 0).Format("2006-01-02")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			releases := []map[string]any{
				{"cycle": "3.12", "latest": "3.12.8", "eol": futureDate},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()
		fetcher := pyUpdater.NewHTTPPythonVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.NoError(t, err)
		assert.Equal(t, "3.12.8", version)
	})

	t.Run("should skip release with past EOL date string", func(t *testing.T) {
		t.Parallel()

		// given
		pastDate := time.Now().AddDate(-1, 0, 0).Format("2006-01-02")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			releases := []map[string]any{
				{"cycle": "3.7", "latest": "3.7.17", "eol": pastDate},
				{"cycle": "3.13", "latest": "3.13.1", "eol": false},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()
		fetcher := pyUpdater.NewHTTPPythonVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.NoError(t, err)
		assert.Equal(t, "3.13.1", version)
	})

	t.Run("should return error when no active release is found", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			releases := []map[string]any{
				{"cycle": "3.7", "latest": "3.7.17", "eol": true},
				{"cycle": "3.6", "latest": "3.6.15", "eol": true},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()
		fetcher := pyUpdater.NewHTTPPythonVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active Python release found")
		assert.Empty(t, version)
	})

	t.Run("should return error when server responds with non-200 status", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()
		fetcher := pyUpdater.NewHTTPPythonVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected status code: 503")
		assert.Empty(t, version)
	})

	t.Run("should return error when response body contains malformed JSON", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{broken"))
		}))
		defer server.Close()
		fetcher := pyUpdater.NewHTTPPythonVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse Python versions")
		assert.Empty(t, version)
	})

	t.Run("should return error when server is unreachable", func(t *testing.T) {
		t.Parallel()

		// given
		fetcher := pyUpdater.NewHTTPPythonVersionFetcherWithURL(http.DefaultClient, "http://127.0.0.1:0/nonexistent")

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch Python versions")
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
		fetcher := pyUpdater.NewHTTPPythonVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active Python release found")
		assert.Empty(t, version)
	})
}
