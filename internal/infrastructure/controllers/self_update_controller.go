package controllers

import (
	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	logger "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type SelfUpdateController struct {
	command commands.SelfUpdate
}

func NewSelfUpdateController(command commands.SelfUpdate) *SelfUpdateController {
	return &SelfUpdateController{command: command}
}

func (it *SelfUpdateController) GetBind() entities.ControllerBind {
	return entities.ControllerBind{
		Use:   "self-update",
		Short: "Update autoupdate to the latest version",
		Long:  "Download and install the latest version of autoupdate from GitHub releases.",
	}
}

// AddFlags adds the self-update-specific flags to the given Cobra command.
func (it *SelfUpdateController) AddFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("force", false, "Skip confirmation prompts")
}

func (it *SelfUpdateController) Execute(cmd *cobra.Command, _ []string) {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")

	err := it.command.Execute(dryRun, force)
	if err != nil {
		logger.Errorf("Self-update failed: %s", err)
		return
	}
}
