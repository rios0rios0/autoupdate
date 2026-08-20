//go:build unit

package support_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/support"
)

// versionCase is one question put to both implementations of the rule: the Go
// IsNewerVersion and the bash autoupdate_version_is_newer.
type versionCase struct {
	name      string
	current   string
	candidate string
	newer     bool
}

// versionCases covers the shapes that reach a version pin in the wild. The
// downgrade rows are the ones that matter: each is a pull request autoupdate
// used to open against a repository that had moved ahead of the release feed.
//
// The build metadata rows guard the other half of semver precedence: everything
// after a "+" is excluded from it, so a difference that exists only there is
// neither an upgrade nor a downgrade and must leave the pin alone.
func versionCases() []versionCase {
	return []versionCase{
		{"newer patch", "24.19.0", "24.19.1", true},
		{"newer minor", "3.13.0", "3.14.0", true},
		{"newer major", "20.11.1", "24.19.0", true},
		{"identical versions", "24.19.0", "24.19.0", false},
		{"older patch", "24.19.1", "24.19.0", false},
		{"older minor", "3.14.0", "3.13.2", false},
		{"repository ahead of the LTS line", "26.7.0", "24.19.0", false},
		{"major-only pin behind the release", "24", "26", true},
		{"major-only pin ahead of the release", "26", "24", false},
		{"major-only pin against a full version", "24", "24.19.0", true},
		{"full version against a major-only release", "24.19.0", "24", false},
		{"two-part pin behind the release", "3.13", "3.14", true},
		{"two-part pin ahead of the release", "3.14", "3.13", false},
		{"v-prefixed pin", "v20.11.1", "v24.19.0", true},
		{"v-prefixed pin ahead of the release", "v24.19.0", "v20.11.1", false},
		{"four-segment release", "9.4.0.0", "9.4.1.0", true},
		{"four-segment release backwards", "9.4.1.0", "9.4.0.0", false},
		{"pre-release promoted to final", "9.0.100-rc.2", "9.0.100", true},
		{"final replaced by a pre-release", "9.0.100", "9.0.100-rc.2", false},
		{"newer pre-release", "9.0.100-rc.1", "9.0.100-rc.2", true},
		{"older pre-release", "9.0.100-rc.2", "9.0.100-rc.1", false},
		// Multi-digit identifiers are where a string comparison and semver
		// precedence part ways, so the table has to reach past rc.1/rc.2.
		{"double-digit pre-release", "9.0.100-rc.9", "9.0.100-rc.10", true},
		{"double-digit pre-release backwards", "9.0.100-rc.10", "9.0.100-rc.9", false},
		{"double-digit against single digit", "9.0.100-rc.2", "9.0.100-rc.11", true},
		{"longer pre-release outranks its prefix", "9.0.100-rc.2", "9.0.100-rc.2.24474.11", true},
		{"shorter pre-release loses to its extension", "9.0.100-rc.2.24474.11", "9.0.100-rc.2", false},
		{"numeric identifier ranks below alphanumeric", "1.0.0-1", "1.0.0-alpha", true},
		{"alphanumeric identifier ranks above numeric", "1.0.0-alpha", "1.0.0-1", false},
		{"build metadata added to an unchanged release", "1.0.0", "1.0.0+build.1", false},
		{"build metadata dropped from an unchanged release", "1.0.0+build.1", "1.0.0", false},
		{"build metadata differing on both sides", "1.0.0+build.1", "1.0.0+build.2", false},
		{"build metadata on a newer release", "1.0.0+build.9", "1.0.1+build.1", true},
		{"build metadata on an older release", "1.0.1+build.1", "1.0.0+build.9", false},
		{"pre-release promoted to a final build", "9.0.100-rc.1", "9.0.100+build.5", true},
		{"final build replaced by a pre-release", "9.0.100+build.5", "9.0.100-rc.1", false},
		{"pre-release carrying build metadata", "9.0.100-rc.1+build.5", "9.0.100-rc.2", true},
		{"alias pin", "lts/*", "24.19.0", false},
		{"named pin", "system", "3.4.1", false},
		{"alternative implementation pin", "jruby-9.4.0.0", "3.4.1", false},
		{"free-threaded python pin", "3.13t", "3.14.0", false},
		{"empty current version", "", "24.19.0", false},
		{"empty candidate version", "24.19.0", "", false},
		{"both empty", "", "", false},
	}
}

func TestIsNewerVersion(t *testing.T) {
	t.Parallel()

	for _, testCase := range versionCases() {
		t.Run("should report "+testCase.name+" correctly", func(t *testing.T) {
			t.Parallel()

			// given
			current, candidate := testCase.current, testCase.candidate

			// when
			newer := support.IsNewerVersion(current, candidate)

			// then
			assert.Equal(t, testCase.newer, newer,
				"IsNewerVersion(%q, %q)", current, candidate)
		})
	}
}

func TestVersionGuardScript(t *testing.T) {
	t.Parallel()

	t.Run("should answer exactly as IsNewerVersion does", func(t *testing.T) {
		t.Parallel()

		for _, testCase := range versionCases() {
			// given
			scriptPath := writeGuardHarness(t)

			// when
			newer := runGuard(t, scriptPath, testCase.candidate, testCase.current)

			// then
			assert.Equal(t, testCase.newer, newer,
				"autoupdate_version_is_newer %q %q (%s)",
				testCase.candidate, testCase.current, testCase.name)
		}
	})

	t.Run("should leave the pin alone when an argument is missing", func(t *testing.T) {
		t.Parallel()

		// given
		scriptPath := writeGuardHarness(t)

		// when
		newer := runGuard(t, scriptPath)

		// then
		assert.False(t, newer)
	})
}

// writeGuardHarness materialises the emitted fragment as a runnable script that
// forwards its arguments to the guard, under the same shell options the
// generated upgrade scripts run with.
func writeGuardHarness(t *testing.T) string {
	t.Helper()

	script := "#!/bin/bash\nset -euo pipefail\n\n" +
		support.VersionGuardScript() +
		"autoupdate_version_is_newer \"${1:-}\" \"${2:-}\"\n"

	path := filepath.Join(t.TempDir(), "guard.sh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700))

	return path
}

// runGuard reports whether the shell guard accepted the upgrade, failing the
// test on any exit status other than the documented 0 and 1.
func runGuard(t *testing.T, scriptPath string, args ...string) bool {
	t.Helper()

	output, err := exec.Command("bash", append([]string{scriptPath}, args...)...).CombinedOutput()
	if err == nil {
		return true
	}

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "guard failed to run: %s", output)
	require.Equal(t, 1, exitErr.ExitCode(), "unexpected guard exit status: %s", output)

	return false
}
