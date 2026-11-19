package controllers_test

import (
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/controllers"
	"github.com/rios0rios0/autoupdate/test/domain/commanddoubles"
)

func TestRunControllerShardFlag(t *testing.T) {
	t.Parallel()

	invalid := []struct {
		name  string
		value string
	}{
		{"an index of zero", "0/4"},
		{"an index past the total", "5/4"},
		{"a value with no total", "2"},
		{"an empty value", ""},
	}
	for _, testCase := range invalid {
		t.Run("should reject "+testCase.name+" before the run starts", func(t *testing.T) {
			t.Parallel()

			// given
			cmd, runs := newRunCommand("--shard", testCase.value)

			// when
			err := cmd.Execute()

			// then
			require.ErrorIs(t, err, entities.ErrInvalidShard)
			assert.Zero(t, *runs, "a value that names no shard must stop the command before it runs")
		})
	}

	t.Run("should read the shard the flag names for the run", func(t *testing.T) {
		t.Parallel()

		// given
		cmd, runs := newRunCommand("--shard", "2/4")

		// when
		err := cmd.Execute()

		// then
		require.NoError(t, err)
		assert.Equal(t, 1, *runs)
		shard := controllers.ShardFromFlags(cmd)
		assert.Equal(t, 2, shard.Index())
		assert.Equal(t, 4, shard.Total())
	})

	t.Run("should keep the run whole when the flag is not given", func(t *testing.T) {
		t.Parallel()

		// given
		cmd, runs := newRunCommand()

		// when
		err := cmd.Execute()

		// then
		require.NoError(t, err)
		assert.Equal(t, 1, *runs)
		assert.False(t, controllers.ShardFromFlags(cmd).IsSet())
	})

	t.Run("should keep the run whole on a command that does not declare the flag", func(t *testing.T) {
		t.Parallel()

		// given
		cmd := &cobra.Command{Use: "run"}

		// when
		shard := controllers.ShardFromFlags(cmd)

		// then
		assert.False(t, shard.IsSet())
	})
}

// newRunCommand registers the run controller's flags on a command whose Run only counts its
// calls, so a test sees what parsing lets through without reading a configuration or running
// anything. The arguments are never nil: given nil, cobra parses the test binary's own.
func newRunCommand(args ...string) (*cobra.Command, *int) {
	runs := 0
	cmd := &cobra.Command{
		Use: "run",
		Run: func(*cobra.Command, []string) { runs++ },
	}
	controllers.NewRunController(&commanddoubles.StubRunCommand{}).AddFlags(cmd)
	cmd.SetArgs(append([]string{}, args...))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd, &runs
}
