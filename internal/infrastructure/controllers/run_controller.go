package controllers

import (
	"context"
	"fmt"

	logger "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// RunController handles the "run" subcommand (batch mode).
type RunController struct {
	command commands.Run
}

// NewRunController creates a new RunController.
func NewRunController(command commands.Run) *RunController {
	return &RunController{command: command}
}

// GetBind returns the Cobra command metadata for the run controller.
func (it *RunController) GetBind() entities.ControllerBind {
	return entities.ControllerBind{
		Use:   "run",
		Short: "Run the dependency update engine",
		Long: `Discover repositories, scan for outdated dependencies,
and create Pull Requests.

This is the main command intended to be used in a cronjob.
It reads the configuration file, discovers repositories from
each configured provider and organization, then runs all
enabled updaters against each repository.`,
	}
}

// Execute runs the batch update mode.
func (it *RunController) Execute(cmd *cobra.Command, _ []string) {
	ctx := context.Background()

	configPath, _ := cmd.Flags().GetString("config")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	verbose, _ := cmd.Flags().GetBool("verbose")
	providerFilter, _ := cmd.Flags().GetString("provider")
	orgOverride, _ := cmd.Flags().GetString("org")
	updaterFilter, _ := cmd.Flags().GetString("updater")

	// Load configuration
	cfgPath := configPath
	if cfgPath == "" {
		var err error
		cfgPath, err = entities.FindConfigFile()
		if err != nil {
			logger.Errorf(
				"no config file found: %v\nSpecify one with --config or create autoupdate.yaml",
				err,
			)
			return
		}
	}

	logger.Infof("Using config file: %s", cfgPath)

	settings, err := entities.NewSettings(cfgPath)
	if err != nil {
		logger.Errorf("failed to load config: %v", err)
		return
	}

	logger.Info("Starting autoupdate run...")

	if runErr := it.command.Execute(ctx, settings, commands.RunOptions{
		DryRun:       dryRun,
		Verbose:      verbose,
		ProviderName: providerFilter,
		OrgOverride:  orgOverride,
		UpdaterName:  updaterFilter,
	}); runErr != nil {
		logger.Errorf("Run failed: %v", runErr)
	}
}

// AddFlags adds the run-specific flags to the given Cobra command.
func (it *RunController) AddFlags(cmd *cobra.Command) {
	cmd.Flags().String("provider", "", "Only process this provider (github, gitlab, azuredevops)")
	cmd.Flags().String("org", "", "Only process this organization/group")
	cmd.Flags().String("updater", "",
		fmt.Sprintf("Only run this updater (terraform, golang, python, javascript)"),
	)
}
