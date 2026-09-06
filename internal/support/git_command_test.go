package support_test

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/support"
)

func TestGitCommand(t *testing.T) {
	t.Parallel()

	t.Run("should run git by its absolute path inside the given directory when git is installed", func(t *testing.T) {
		t.Parallel()

		// given
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git is not available on this platform")
		}
		dir := t.TempDir()

		// when
		cmd := support.GitCommand(t.Context(), dir, "--version")
		output, err := cmd.Output()

		// then
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(cmd.Path), "expected the executable to be resolved, got %q", cmd.Path)
		assert.Equal(t, dir, cmd.Dir)
		assert.Contains(t, string(output), "git version")
	})

	t.Run("should keep the arguments in order when several are given", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()

		// when
		cmd := support.GitCommand(t.Context(), dir, "rev-parse", "--abbrev-ref", "HEAD")

		// then
		require.Len(t, cmd.Args, 4)
		assert.Equal(t, []string{"rev-parse", "--abbrev-ref", "HEAD"}, cmd.Args[1:])
	})
}
