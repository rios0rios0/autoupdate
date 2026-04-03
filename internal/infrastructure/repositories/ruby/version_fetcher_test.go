//go:build unit

package ruby_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rbUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/ruby"
)

func TestHTTPRubyVersionFetcher(t *testing.T) {
	t.Parallel()

	t.Run("should return latest active Ruby version when API responds with valid data", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			releases := []map[string]any{
				{"cycle": "3.3", "latest": "3.3.6", "eol": false},
				{"cycle": "3.2", "latest": "3.2.6", "eol": false},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()
		fetcher := rbUpdater.NewHTTPRubyVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.NoError(t, err)
		assert.Equal(t, "3.3.6", version)
	})

	t.Run("should skip EOL releases and return first active version", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			releases := []map[string]any{
				{"cycle": "2.7", "latest": "2.7.8", "eol": true},
				{"cycle": "3.3", "latest": "3.3.6", "eol": false},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()
		fetcher := rbUpdater.NewHTTPRubyVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.NoError(t, err)
		assert.Equal(t, "3.3.6", version)
	})

	t.Run("should handle EOL as a future date string and treat as active", func(t *testing.T) {
		t.Parallel()

		// given
		futureDate := time.Now().AddDate(2, 0, 0).Format("2006-01-02")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			releases := []map[string]any{
				{"cycle": "3.3", "latest": "3.3.6", "eol": futureDate},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()
		fetcher := rbUpdater.NewHTTPRubyVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.NoError(t, err)
		assert.Equal(t, "3.3.6", version)
	})

	t.Run("should skip release with past EOL date string", func(t *testing.T) {
		t.Parallel()

		// given
		pastDate := time.Now().AddDate(-1, 0, 0).Format("2006-01-02")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			releases := []map[string]any{
				{"cycle": "2.7", "latest": "2.7.8", "eol": pastDate},
				{"cycle": "3.3", "latest": "3.3.6", "eol": false},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()
		fetcher := rbUpdater.NewHTTPRubyVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		require.NoError(t, err)
		assert.Equal(t, "3.3.6", version)
	})

	t.Run("should return error when no active release is found", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			releases := []map[string]any{
				{"cycle": "2.7", "latest": "2.7.8", "eol": true},
				{"cycle": "2.6", "latest": "2.6.10", "eol": true},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()
		fetcher := rbUpdater.NewHTTPRubyVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active Ruby release found")
		assert.Empty(t, version)
	})

	t.Run("should return error when server responds with non-200 status", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()
		fetcher := rbUpdater.NewHTTPRubyVersionFetcherWithURL(server.Client(), server.URL)

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
		fetcher := rbUpdater.NewHTTPRubyVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse Ruby versions")
		assert.Empty(t, version)
	})

	t.Run("should return error when server is unreachable", func(t *testing.T) {
		t.Parallel()

		// given
		fetcher := rbUpdater.NewHTTPRubyVersionFetcherWithURL(http.DefaultClient, "http://127.0.0.1:0/nonexistent")

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch Ruby versions")
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
		fetcher := rbUpdater.NewHTTPRubyVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(t.Context())

		// then
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active Ruby release found")
		assert.Empty(t, version)
	})
}

func TestIsActiveRelease(t *testing.T) {
	t.Parallel()

	t.Run("should return true when EOL is false", func(t *testing.T) {
		t.Parallel()

		// given
		release := rbUpdater.RubyRelease{
			Cycle:  "3.3",
			Latest: "3.3.6",
			EOL:    false,
		}

		// when
		result := rbUpdater.IsActiveRelease(release)

		// then
		assert.True(t, result)
	})

	t.Run("should return false when EOL is true", func(t *testing.T) {
		t.Parallel()

		// given
		release := rbUpdater.RubyRelease{
			Cycle:  "2.7",
			Latest: "2.7.8",
			EOL:    true,
		}

		// when
		result := rbUpdater.IsActiveRelease(release)

		// then
		assert.False(t, result)
	})

	t.Run("should return false when EOL is a past date string", func(t *testing.T) {
		t.Parallel()

		// given
		release := rbUpdater.RubyRelease{
			Cycle:  "2.6",
			Latest: "2.6.10",
			EOL:    "2021-03-31",
		}

		// when
		result := rbUpdater.IsActiveRelease(release)

		// then
		assert.False(t, result)
	})

	t.Run("should return true when EOL is a future date string", func(t *testing.T) {
		t.Parallel()

		// given
		release := rbUpdater.RubyRelease{
			Cycle:  "3.3",
			Latest: "3.3.6",
			EOL:    "2028-03-31",
		}

		// when
		result := rbUpdater.IsActiveRelease(release)

		// then
		assert.True(t, result)
	})

	t.Run("should return false when EOL is an invalid date string", func(t *testing.T) {
		t.Parallel()

		// given
		release := rbUpdater.RubyRelease{
			Cycle:  "3.1",
			Latest: "3.1.6",
			EOL:    "not-a-date",
		}

		// when
		result := rbUpdater.IsActiveRelease(release)

		// then
		assert.False(t, result)
	})

	t.Run("should return false when EOL is an unexpected type", func(t *testing.T) {
		t.Parallel()

		// given
		release := rbUpdater.RubyRelease{
			Cycle:  "3.1",
			Latest: "3.1.6",
			EOL:    42,
		}

		// when
		result := rbUpdater.IsActiveRelease(release)

		// then
		assert.False(t, result)
	})
}
