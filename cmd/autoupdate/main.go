package main

import (
	"os"

	logger "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/rios0rios0/autoupdate/internal"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/controllers"
)

func buildRootCommand(localController *controllers.LocalController) *cobra.Command {
	bind := localController.GetBind()
	//nolint:exhaustruct // Minimal Command initialization with required fields only
	cmd := &cobra.Command{
		Use:   "autoupdate [path]",
		Short: "Multi-provider dependency update engine",
		Long: `A self-hosted Dependabot alternative that automatically discovers repositories,
detects outdated dependencies across multiple ecosystems (Terraform, Go, etc.),
and creates Pull Requests to upgrade them.

Supports GitHub, GitLab, and Azure DevOps as Git hosting providers.

Usage modes:
  autoupdate .              Update the current local repository (standalone mode)
  autoupdate /path/to/repo  Update a specific local repository
  autoupdate run            Batch mode using a config file (cronjob)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 0 {
				return command.Help()
			}
			localController.Execute(command, args)
			return nil
		},
	}

	// Global persistent flags
	cmd.PersistentFlags().StringP("config", "c", "",
		"Path to config file (default: auto-detect)")
	cmd.PersistentFlags().String("token", "",
		"Auth token for the Git provider (overrides env var detection)")
	cmd.PersistentFlags().Bool("dry-run", false,
		"Show what would be done without making changes")
	cmd.PersistentFlags().BoolP("verbose", "v", false,
		"Enable verbose output")

	_ = bind // suppress unused warning
	return cmd
}

func addSubcommands(rootCmd *cobra.Command, appContext *internal.AppInternal) {
	for _, controller := range appContext.GetControllers() {
		bind := controller.GetBind()
		ctrl := controller // capture for closure
		//nolint:exhaustruct // Minimal Command initialization with required fields only
		subCmd := &cobra.Command{
			Use:   bind.Use,
			Short: bind.Short,
			Long:  bind.Long,
			Run: func(command *cobra.Command, arguments []string) {
				ctrl.Execute(command, arguments)
			},
		}

		// Add controller-specific flags
		if rc, ok := ctrl.(*controllers.RunController); ok {
			rc.AddFlags(subCmd)
		}

		rootCmd.AddCommand(subCmd)
	}
}

func main() {
	//nolint:exhaustruct // Minimal TextFormatter initialization with required fields only
	logger.SetFormatter(&logger.TextFormatter{
		ForceColors:   true,
		FullTimestamp: true,
	})
	if os.Getenv("DEBUG") == "true" {
		logger.SetLevel(logger.DebugLevel)
	}

	// Inject controllers via DIG
	localController := injectLocalController()
	cobraRoot := buildRootCommand(localController)

	// Add all subcommands
	appContext := injectAppContext()
	addSubcommands(cobraRoot, appContext)

	if err := cobraRoot.Execute(); err != nil {
		logger.Fatalf("Error executing 'autoupdate': %s", err)
	}
}
