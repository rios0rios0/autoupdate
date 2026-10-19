package java_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	javaUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/java"
)

func TestHTTPJavaVersionFetcher(t *testing.T) {
	t.Parallel()

	t.Run("should return latest active LTS Java version when API responds with valid data", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(
				[]byte(
					`[{"cycle":"21","latest":"21.0.5","lts":"2023-09-19","eol":false},{"cycle":"17","latest":"17.0.13","lts":"2021-09-14","eol":false}]`,
				),
			)
		}))
		defer server.Close()

		fetcher := javaUpdater.NewHTTPJavaVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(context.Background())

		// then
		require.NoError(t, err)
		assert.Equal(t, "21.0.5", version)
	})

	t.Run("should skip non-LTS releases even when they are newer", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(
				[]byte(
					`[{"cycle":"22","latest":"22.0.2","lts":false,"eol":false},{"cycle":"21","latest":"21.0.5","lts":"2023-09-19","eol":false}]`,
				),
			)
		}))
		defer server.Close()

		fetcher := javaUpdater.NewHTTPJavaVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(context.Background())

		// then
		require.NoError(t, err)
		assert.Equal(t, "21.0.5", version)
	})

	t.Run("should return the JDK version of the newest LTS line from an Eclipse Temurin feed", func(t *testing.T) {
		t.Parallel()

		// given the feed's own shape: release names with respin and build numbers,
		// a boolean LTS flag, and the lines out of cycle order
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[` +
				`{"cycle":"27","latest":"27+35","lts":false,"eol":false},` +
				`{"cycle":"17","latest":"17.0.20.1+1","lts":true,"eol":false},` +
				`{"cycle":"25","latest":"25.0.4.1+1","lts":true,"eol":false},` +
				`{"cycle":"11","latest":"11.0.32.1+1","lts":true,"eol":false},` +
				`{"cycle":"21","latest":"21.0.12.1+1","lts":true,"eol":false},` +
				`{"cycle":"8","latest":"8u504-b01","lts":true,"eol":false}` +
				`]`))
		}))
		defer server.Close()

		fetcher := javaUpdater.NewHTTPJavaVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(context.Background())

		// then
		require.NoError(t, err)
		assert.Equal(t, "25.0.4", version)
	})

	t.Run("should return error when the newest LTS release has a name it cannot read", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"cycle":"25","latest":"jdk-25.0.4","lts":true,"eol":false}]`))
		}))
		defer server.Close()

		fetcher := javaUpdater.NewHTTPJavaVersionFetcherWithURL(server.Client(), server.URL)

		// when
		_, err := fetcher.FetchLatestVersion(context.Background())

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unrecognized Java release "jdk-25.0.4"`)
	})

	t.Run("should skip EOL releases even if LTS", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(
				[]byte(
					`[{"cycle":"11","latest":"11.0.24","lts":"2018-09-25","eol":true},{"cycle":"21","latest":"21.0.5","lts":"2023-09-19","eol":false}]`,
				),
			)
		}))
		defer server.Close()

		fetcher := javaUpdater.NewHTTPJavaVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(context.Background())

		// then
		require.NoError(t, err)
		assert.Equal(t, "21.0.5", version)
	})

	t.Run("should handle EOL as future date string and treat as active", func(t *testing.T) {
		t.Parallel()

		// given
		futureDate := time.Now().AddDate(2, 0, 0).Format("2006-01-02")
		body := `[{"cycle":"21","latest":"21.0.5","lts":"2023-09-19","eol":"` + futureDate + `"}]`
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		defer server.Close()

		fetcher := javaUpdater.NewHTTPJavaVersionFetcherWithURL(server.Client(), server.URL)

		// when
		version, err := fetcher.FetchLatestVersion(context.Background())

		// then
		require.NoError(t, err)
		assert.Equal(t, "21.0.5", version)
	})

	t.Run("should return error when no active LTS release is found", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(
				[]byte(
					`[{"cycle":"22","latest":"22.0.2","lts":false,"eol":false},{"cycle":"11","latest":"11.0.24","lts":"2018-09-25","eol":true}]`,
				),
			)
		}))
		defer server.Close()

		fetcher := javaUpdater.NewHTTPJavaVersionFetcherWithURL(server.Client(), server.URL)

		// when
		_, err := fetcher.FetchLatestVersion(context.Background())

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no active LTS Java release found")
	})

	t.Run("should return error when server responds with non-200 status", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		fetcher := javaUpdater.NewHTTPJavaVersionFetcherWithURL(server.Client(), server.URL)

		// when
		_, err := fetcher.FetchLatestVersion(context.Background())

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected status code: 500")
	})

	t.Run("should return error when response body contains malformed JSON", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{broken"))
		}))
		defer server.Close()

		fetcher := javaUpdater.NewHTTPJavaVersionFetcherWithURL(server.Client(), server.URL)

		// when
		_, err := fetcher.FetchLatestVersion(context.Background())

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse Java versions")
	})

	t.Run("should return error when server is unreachable", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		client := server.Client()
		url := server.URL
		server.Close()

		fetcher := javaUpdater.NewHTTPJavaVersionFetcherWithURL(client, url)

		// when
		_, err := fetcher.FetchLatestVersion(context.Background())

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fetch Java versions")
	})

	t.Run("should return error when release list is empty", func(t *testing.T) {
		t.Parallel()

		// given
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
		}))
		defer server.Close()

		fetcher := javaUpdater.NewHTTPJavaVersionFetcherWithURL(server.Client(), server.URL)

		// when
		_, err := fetcher.FetchLatestVersion(context.Background())

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no active LTS Java release found")
	})
}

func TestJDKVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		release string
		jdk     string
	}{
		{"a respin and a build number", "25.0.4.1+1", "25.0.4"},
		{"a build number", "21.0.12+7", "21.0.12"},
		{"the first release of a line", "27+35", "27"},
		{"a plain JDK version", "21.0.5", "21.0.5"},
	}

	for _, testCase := range cases {
		t.Run("should drop what follows the JDK version from "+testCase.name, func(t *testing.T) {
			t.Parallel()

			// given / when
			jdk, err := javaUpdater.JDKVersion(testCase.release)

			// then
			require.NoError(t, err)
			assert.Equal(t, testCase.jdk, jdk)
		})
	}

	unreadable := []struct {
		name    string
		release string
	}{
		// Java 8 kept its own update numbering; its line is never the newest LTS.
		{"Java 8 update numbering", "8u504-b01"},
		{"a prefix", "jdk-25.0.4"},
		{"a pre-release", "26-ea"},
		{"an empty name", ""},
	}

	for _, testCase := range unreadable {
		t.Run("should return error for "+testCase.name, func(t *testing.T) {
			t.Parallel()

			// given / when
			_, err := javaUpdater.JDKVersion(testCase.release)

			// then
			require.Error(t, err)
		})
	}
}

func TestIsLTSRelease(t *testing.T) {
	t.Parallel()

	t.Run("should return true when LTS is bool true", func(t *testing.T) {
		t.Parallel()

		// given
		release := javaUpdater.JavaRelease{LTS: true}

		// when
		result := javaUpdater.IsLTSRelease(release)

		// then
		assert.True(t, result)
	})

	t.Run("should return false when LTS is bool false", func(t *testing.T) {
		t.Parallel()

		// given
		release := javaUpdater.JavaRelease{LTS: false}

		// when
		result := javaUpdater.IsLTSRelease(release)

		// then
		assert.False(t, result)
	})

	t.Run("should return true when LTS is a non-empty date string", func(t *testing.T) {
		t.Parallel()

		// given
		release := javaUpdater.JavaRelease{LTS: "2023-09-19"}

		// when
		result := javaUpdater.IsLTSRelease(release)

		// then
		assert.True(t, result)
	})

	t.Run("should return false when LTS is an empty string", func(t *testing.T) {
		t.Parallel()

		// given
		release := javaUpdater.JavaRelease{LTS: ""}

		// when
		result := javaUpdater.IsLTSRelease(release)

		// then
		assert.False(t, result)
	})

	t.Run("should return false when LTS is an unexpected type", func(t *testing.T) {
		t.Parallel()

		// given
		release := javaUpdater.JavaRelease{LTS: 42}

		// when
		result := javaUpdater.IsLTSRelease(release)

		// then
		assert.False(t, result)
	})
}

func TestIsActiveRelease(t *testing.T) {
	t.Parallel()

	t.Run("should return true when EOL is bool false", func(t *testing.T) {
		t.Parallel()

		// given
		release := javaUpdater.JavaRelease{EOL: false}

		// when
		result := javaUpdater.IsActiveRelease(release)

		// then
		assert.True(t, result)
	})

	t.Run("should return false when EOL is bool true", func(t *testing.T) {
		t.Parallel()

		// given
		release := javaUpdater.JavaRelease{EOL: true}

		// when
		result := javaUpdater.IsActiveRelease(release)

		// then
		assert.False(t, result)
	})

	t.Run("should return false when EOL is a past date string", func(t *testing.T) {
		t.Parallel()

		// given
		release := javaUpdater.JavaRelease{EOL: "2020-01-01"}

		// when
		result := javaUpdater.IsActiveRelease(release)

		// then
		assert.False(t, result)
	})

	t.Run("should return true when EOL is a future date string", func(t *testing.T) {
		t.Parallel()

		// given
		futureDate := time.Now().AddDate(2, 0, 0).Format("2006-01-02")
		release := javaUpdater.JavaRelease{EOL: futureDate}

		// when
		result := javaUpdater.IsActiveRelease(release)

		// then
		assert.True(t, result)
	})

	t.Run("should return false when EOL is an invalid date string", func(t *testing.T) {
		t.Parallel()

		// given
		release := javaUpdater.JavaRelease{EOL: "not-a-date"}

		// when
		result := javaUpdater.IsActiveRelease(release)

		// then
		assert.False(t, result)
	})

	t.Run("should return false when EOL is an unexpected type", func(t *testing.T) {
		t.Parallel()

		// given
		release := javaUpdater.JavaRelease{EOL: 42}

		// when
		result := javaUpdater.IsActiveRelease(release)

		// then
		assert.False(t, result)
	})
}
