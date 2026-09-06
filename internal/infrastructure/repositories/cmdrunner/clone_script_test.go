package cmdrunner_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

// failingRunner is a cmdrunner.Runner whose every run fails with the given
// output, the way a script that aborted mid-clone does.
type failingRunner struct {
	output string
}

func (r *failingRunner) Run(
	_ context.Context, _ string, _ []string, _ cmdrunner.RunOptions,
) (*cmdrunner.RunResult, error) {
	return &cmdrunner.RunResult{Output: r.output, ExitCode: 1}, errors.New("command bash exited with code 1")
}

func TestRunCloneScript(t *testing.T) {
	t.Parallel()

	t.Run(
		"should run the script from a throwaway root with the clone directory in its environment",
		func(t *testing.T) {
			t.Parallel()

			// given
			runner := repositorydoubles.NewSpyScriptRunner("CHANGES_PUSHED=true\n")
			var repoDir string
			run := cmdrunner.CloneScriptRun{
				Body:        "#!/bin/bash\necho hello\n",
				TempPattern: "autoupdate-test-*",
				Env: func(dir string) []string {
					repoDir = dir
					return []string{"REPO_DIR=" + dir}
				},
			}

			// when
			output, err := cmdrunner.RunCloneScript(t.Context(), runner, run)

			// then
			require.NoError(t, err)
			assert.Equal(t, "CHANGES_PUSHED=true\n", output)
			require.Len(t, runner.Calls, 1)
			assert.Equal(t, "bash", runner.Calls[0].Name)
			assert.Equal(t, []string{"#!/bin/bash\necho hello\n"}, runner.Scripts)
			assert.Equal(t, filepath.Dir(repoDir), runner.Calls[0].Opts.Dir)
			assert.Equal(t, "repo", filepath.Base(repoDir))
			assert.Contains(t, runner.Calls[0].Opts.Env, "REPO_DIR="+repoDir)
		},
	)

	t.Run("should remove the throwaway root when the run is over", func(t *testing.T) {
		t.Parallel()

		// given
		runner := repositorydoubles.NewSpyScriptRunner("")
		var cloneRoot string
		run := cmdrunner.CloneScriptRun{
			Body:        "#!/bin/bash\n",
			TempPattern: "autoupdate-test-*",
			Env: func(dir string) []string {
				cloneRoot = filepath.Dir(dir)
				return nil
			},
		}

		// when
		_, err := cmdrunner.RunCloneScript(t.Context(), runner, run)

		// then
		require.NoError(t, err)
		_, statErr := os.Stat(cloneRoot)
		assert.True(t, os.IsNotExist(statErr), "expected %s to be removed", cloneRoot)
	})

	t.Run("should redact the secrets from the output when the script fails", func(t *testing.T) {
		t.Parallel()

		// given
		runner := &failingRunner{output: "fatal: could not read from https://x:s3cr3t@example.com/repo.git"}
		run := cmdrunner.CloneScriptRun{
			Body:        "#!/bin/bash\nexit 1\n",
			TempPattern: "autoupdate-test-*",
			Env:         func(_ string) []string { return nil },
			Secrets:     []string{"s3cr3t"},
		}

		// when
		output, err := cmdrunner.RunCloneScript(t.Context(), runner, run)

		// then
		require.Error(t, err)
		assert.Empty(t, output)
		assert.Contains(t, err.Error(), "upgrade script failed")
		assert.Contains(t, err.Error(), "[REDACTED]")
		assert.NotContains(t, err.Error(), "s3cr3t")
	})
}
