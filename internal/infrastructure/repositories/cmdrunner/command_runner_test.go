//go:build unit

package cmdrunner_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
)

func TestNewDefaultRunner(t *testing.T) {
	t.Parallel()

	t.Run("should return a non-nil runner", func(t *testing.T) {
		t.Parallel()

		// when
		runner := cmdrunner.NewDefaultRunner()

		// then
		require.NotNil(t, runner)
	})
}

func TestRun_SuccessfulCommand(t *testing.T) {
	t.Parallel()

	t.Run("should return output and exit code 0 when command succeeds", func(t *testing.T) {
		t.Parallel()

		// given
		runner := cmdrunner.NewDefaultRunner()
		ctx := context.Background()

		// when
		result, err := runner.Run(ctx, "echo", []string{"hello"}, cmdrunner.RunOptions{})

		// then
		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, result.Output, "hello")
	})
}

func TestRun_FailingCommand(t *testing.T) {
	t.Parallel()

	t.Run("should return non-zero exit code and no error when command fails with ExitError", func(t *testing.T) {
		t.Parallel()

		// given
		runner := cmdrunner.NewDefaultRunner()
		ctx := context.Background()

		// when
		result, err := runner.Run(ctx, "false", nil, cmdrunner.RunOptions{})

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exited with code")
		assert.NotEqual(t, 0, result.ExitCode)
	})
}

func TestRun_NonExistentCommand(t *testing.T) {
	t.Parallel()

	t.Run("should return error when command binary does not exist", func(t *testing.T) {
		t.Parallel()

		// given
		runner := cmdrunner.NewDefaultRunner()
		ctx := context.Background()

		// when
		result, err := runner.Run(ctx, "nonexistent_binary_12345", nil, cmdrunner.RunOptions{})

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to execute")
		assert.Equal(t, -1, result.ExitCode)
	})
}

func TestRun_WithDirOption(t *testing.T) {
	t.Parallel()

	t.Run("should execute command in the specified directory", func(t *testing.T) {
		t.Parallel()

		// given
		runner := cmdrunner.NewDefaultRunner()
		ctx := context.Background()
		opts := cmdrunner.RunOptions{Dir: "/tmp"}

		// when
		result, err := runner.Run(ctx, "pwd", nil, opts)

		// then
		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, result.Output, "/tmp")
	})
}

func TestRun_WithEnvOption(t *testing.T) {
	t.Parallel()

	t.Run("should pass environment variables to the subprocess", func(t *testing.T) {
		t.Parallel()

		// given
		runner := cmdrunner.NewDefaultRunner()
		ctx := context.Background()
		env := append(os.Environ(), "TEST_VAR=hello")
		opts := cmdrunner.RunOptions{Env: env}

		// when
		result, err := runner.Run(ctx, "env", nil, opts)

		// then
		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)

		found := false
		for _, line := range strings.Split(result.Output, "\n") {
			if line == "TEST_VAR=hello" {
				found = true
				break
			}
		}
		assert.True(t, found, "expected TEST_VAR=hello in env output")
	})
}
