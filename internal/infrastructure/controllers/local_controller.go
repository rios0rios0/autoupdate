package controllers

import (
	"context"

	logger "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
)

// LocalController backs the root command's positional-path form: `autoupdate .` and
// `autoupdate /path/to/repo`.
//
// It deliberately does not implement entities.Controller. That interface is what
// addSubcommands turns into a subcommand, and there is no `local` subcommand any more --
// registering one would give the same behaviour two spellings.
type LocalController struct {
	command commands.Local
}

// NewLocalController creates a new LocalController.
func NewLocalController(command commands.Local) *LocalController {
	return &LocalController{command: command}
}

// Execute runs the local update mode.
func (it *LocalController) Execute(cmd *cobra.Command, args []string) {
	ctx := context.Background()

	configPath, _ := cmd.Flags().GetString("config")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	verbose, _ := cmd.Flags().GetBool("verbose")
	token, _ := cmd.Flags().GetString("token")

	repoDir := "."
	if len(args) > 0 {
		repoDir = args[0]
	}

	settings, configErr := findReadAndValidateConfig(configPath, false)
	if configErr != nil {
		logger.Debugf("No usable autoupdate config for local mode: %v", configErr)
		settings = nil
	}

	if err := it.command.Execute(ctx, commands.LocalOptions{
		RepoDir:  repoDir,
		DryRun:   dryRun,
		Verbose:  verbose,
		Token:    token,
		Settings: settings,
	}); err != nil {
		logger.Errorf("Local update failed: %v", err)
	}
}
