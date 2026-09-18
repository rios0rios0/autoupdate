package golang_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
)

// TestBuildUpgradeScriptIsValidBash parses the whole generated script rather
// than asserting on fragments of it.
//
// Every other test here reads the script as text, which cannot fail on an
// unbalanced quote, an unterminated `if`, or a heredoc whose delimiter moved --
// and this script is assembled from a dozen writers plus three guards, so the
// seams between them are exactly where that happens. A break there surfaces as
// a repository whose upgrade run dies on the first line, which is a long way
// from the change that caused it.
//
// Both major modes are parsed because they emit different guards, so one of the
// two paths is otherwise never parsed at all.
func TestBuildUpgradeScriptIsValidBash(t *testing.T) {
	t.Parallel()

	bash, err := exec.LookPath("bash")
	require.NoError(t, err, "bash is required to parse the generated script")

	for _, allowMajorUpdates := range []bool{true, false} {
		t.Run(majorModeName(allowMajorUpdates), func(t *testing.T) {
			t.Parallel()

			// given
			body := golang.BuildRemoteScriptForMajorMode(allowMajorUpdates)
			path := filepath.Join(t.TempDir(), "upgrade.sh")
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

			// when
			output, parseErr := exec.Command(bash, "-n", path).CombinedOutput()

			// then
			require.NoError(t, parseErr, "generated script does not parse: %s", output)
		})
	}
}

func majorModeName(allowMajorUpdates bool) string {
	if allowMajorUpdates {
		return "majors allowed"
	}

	return "majors held back"
}
