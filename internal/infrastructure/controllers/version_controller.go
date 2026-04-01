package controllers

import (
	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/spf13/cobra"
)

type VersionController struct {
	command commands.Version
}

func NewVersionController(command commands.Version) *VersionController {
	return &VersionController{command: command}
}

func (it *VersionController) GetBind() entities.ControllerBind {
	return entities.ControllerBind{
		Use:   "version",
		Short: "Show autoupdate version",
		Long:  "Display the version information for autoupdate.",
	}
}

func (it *VersionController) Execute(_ *cobra.Command, _ []string) {
	it.command.Execute()
}
