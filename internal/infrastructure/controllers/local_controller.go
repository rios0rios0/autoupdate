package controllers

import (
	"context"

	logger "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// LocalController handles the root command with a path argument (standalone local mode).
type LocalController struct {
	command commands.Local
}

// NewLocalController creates a new LocalController.
func NewLocalController(command commands.Local) *LocalController {
	return &LocalController{command: command}
}

// GetBind returns the Cobra command metadata for the local controller.
func (it *LocalController) GetBind() entities.ControllerBind {
	return entities.ControllerBind{
		Use:   "local",
		Short: "Update dependencies in a local repository",
		Long: `Update dependencies in a local Git repository.
Detects the project type, upgrades dependencies, and creates a pull request.`,
	}
}

// Execute runs the local update mode.
func (it *LocalController) Execute(cmd *cobra.Command, args []string) {
	ctx := context.Background()

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	verbose, _ := cmd.Flags().GetBool("verbose")
	token, _ := cmd.Flags().GetString("token")

	repoDir := "."
	if len(args) > 0 {
		repoDir = args[0]
	}

	if err := it.command.Execute(ctx, commands.LocalOptions{
		RepoDir: repoDir,
		DryRun:  dryRun,
		Verbose: verbose,
		Token:   token,
	}); err != nil {
		logger.Errorf("Local update failed: %v", err)
	}
}
