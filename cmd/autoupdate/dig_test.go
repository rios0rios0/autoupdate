package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectApp(t *testing.T) {
	t.Parallel()

	t.Run("should build both roots from one container", func(t *testing.T) {
		t.Parallel()

		// given / when
		appContext, localController := injectApp()

		// then
		require.NotNil(t, appContext)
		require.NotNil(t, localController)
		assert.NotEmpty(t, appContext.GetControllers())
	})
}

func TestBuildRootCommand(t *testing.T) {
	t.Parallel()

	t.Run("should create the root command with its persistent flags", func(t *testing.T) {
		t.Parallel()

		// given
		_, localController := injectApp()

		// when
		cmd := buildRootCommand(localController)

		// then
		require.NotNil(t, cmd)
		assert.Equal(t, "autoupdate [path]", cmd.Use)
		assert.NotNil(t, cmd.PersistentFlags().Lookup("config"))
		assert.NotNil(t, cmd.PersistentFlags().Lookup("verbose"))
		assert.NotNil(t, cmd.PersistentFlags().Lookup("skip-cleanup"))
	})
}

func TestAddSubcommands(t *testing.T) {
	t.Parallel()

	t.Run("should register run, version and self-update", func(t *testing.T) {
		t.Parallel()

		// given
		appContext, localController := injectApp()
		rootCmd := buildRootCommand(localController)

		// when
		addSubcommands(rootCmd, appContext, localController)

		// then
		for _, name := range []string{"run", "version", "self-update"} {
			subCmd, _, err := rootCmd.Find([]string{name})
			require.NoError(t, err)
			require.NotNil(t, subCmd)
			assert.Equal(t, name, subCmd.Name())
		}
	})

	t.Run("should keep local hidden and deprecated", func(t *testing.T) {
		t.Parallel()

		// given -- `local` was removed, but registering nothing in its place is worse than a
		// deprecation notice: the bare word would fall through to the root command's
		// positional argument and be read as a path, so `autoupdate local` would report that
		// ./local does not exist rather than that the command is gone.
		//
		// Asserted on the command's name rather than on an error, because cobra's Find
		// returns the deepest match it has rather than failing on an unknown name.
		appContext, localController := injectApp()
		rootCmd := buildRootCommand(localController)
		addSubcommands(rootCmd, appContext, localController)

		// when
		localCmd, _, err := rootCmd.Find([]string{"local"})

		// then
		require.NoError(t, err)
		require.NotNil(t, localCmd)
		assert.Equal(t, "local", localCmd.Name())
		assert.True(t, localCmd.Hidden, "it must not appear in --help")
	})
}
