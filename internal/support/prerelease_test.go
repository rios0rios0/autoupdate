package support_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/support"
)

// prereleaseCases covers the shapes that reach a dependency version in the
// wild. The true rows beginning `3.0.0-beta3`, `7.1.0-M1`, `5.5-beta2` and
// `5.7-alpha1` are the four versions a single Maven run wrote into one
// `pom.xml`; the first of them dropped `log4j-api` back to a transitive 2.24.1
// and reintroduced three CVEs the pinned version existed to remediate.
//
// The false rows are the ones that matter just as much: a filter that also
// caught `2.26.1` or `33.7.1-jre` would stop the updater doing its job.
func prereleaseCases() []struct {
	name    string
	version string
	pre     bool
} {
	return []struct {
		name    string
		version string
		pre     bool
	}{
		{"maven beta with a trailing digit", "3.0.0-beta3", true},
		{"spring milestone", "7.1.0-M1", true},
		{"two-segment beta", "5.5-beta2", true},
		{"alpha", "5.7-alpha1", true},
		{"release candidate", "1.2.3-rc1", true},
		{"dotted release candidate", "1.0.0-rc.1", true},
		{"maven snapshot", "2.0.0-SNAPSHOT", true},
		{"uppercase alpha", "1.0.0-ALPHA", true},
		{"maven cr qualifier", "1.0.0-cr1", true},
		{"spelled-out milestone", "1.0.0-milestone2", true},
		{"no separator", "1.0.0beta", true},
		{"preview build", "21.0.1-preview", true},

		{"plain release", "2.26.1", false},
		{"plain release with two segments", "5.5", false},
		{"guava classifier", "33.7.1-jre", false},
		{"leading v", "v1.0.0", false},
		{"four segments", "1.2.3.4", false},
		{"build metadata only", "1.0.0+build.1", false},
		{"classifier that merely contains no qualifier", "1.0.0-android", false},
		{"empty", "", false},
		{"not a version", "latest", false},
	}
}

func TestIsPrereleaseVersion(t *testing.T) {
	t.Parallel()

	for _, testCase := range prereleaseCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// given
			version := testCase.version

			// when
			pre := support.IsPrereleaseVersion(version)

			// then
			assert.Equal(t, testCase.pre, pre, "IsPrereleaseVersion(%q)", version)
		})
	}
}

func TestMavenVersionIgnore(t *testing.T) {
	t.Parallel()

	// The plugin splits the value on commas and matches each expression against
	// the whole version, so the test compiles them the same way rather than
	// asserting on the string. A pattern that is correct as text but does not
	// compile would be dropped silently by Maven and filter nothing.
	compile := func(t *testing.T) []*regexp.Regexp {
		t.Helper()

		value := support.MavenVersionIgnore()
		require.NotEmpty(t, value)

		patterns := make([]*regexp.Regexp, 0)
		for _, raw := range strings.Split(value, ",") {
			compiled, err := regexp.Compile(raw)
			require.NoError(t, err, "pattern %q does not compile", raw)
			patterns = append(patterns, compiled)
		}

		return patterns
	}

	matchesAny := func(patterns []*regexp.Regexp, version string) bool {
		for _, pattern := range patterns {
			if pattern.MatchString(version) {
				return true
			}
		}

		return false
	}

	t.Run("should filter exactly what IsPrereleaseVersion calls a pre-release", func(t *testing.T) {
		t.Parallel()

		// given
		patterns := compile(t)

		for _, testCase := range prereleaseCases() {
			if testCase.version == "" || testCase.version == "latest" {
				// Not versions Maven would offer as candidates at all.
				continue
			}

			// when
			ignored := matchesAny(patterns, testCase.version)

			// then -- the two sides must agree, or a qualifier filtered from a
			// changelog entry would still be written into the pom
			assert.Equal(t, testCase.pre, ignored,
				"maven.version.ignore vs IsPrereleaseVersion disagree on %q (%s)",
				testCase.version, testCase.name)
		}
	})

	t.Run("should not filter the releases the pom actually pins", func(t *testing.T) {
		t.Parallel()

		// given -- every stable version from the rest-arch pom that the broken
		// run either kept or should have kept
		patterns := compile(t)
		kept := []string{
			"2.26.1", "6.2.19", "5.4.3", "5.6.4",
			"1.6.3", "2.22.2", "5.5.0", "2.14.3", "33.7.1-jre", "11.0.25",
		}

		for _, version := range kept {
			// when
			ignored := matchesAny(patterns, version)

			// then
			assert.False(t, ignored, "%q must remain a candidate", version)
		}
	})
}
