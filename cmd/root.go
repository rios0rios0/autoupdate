package cmd

import (
	"github.com/spf13/cobra"
)

//nolint:gochecknoglobals // required by cobra CLI pattern
var (
	configPath string
	tokenFlag  string
	dryRun     bool
	verbose    bool
)

//nolint:gochecknoglobals // required by cobra CLI pattern
var rootCmd = &cobra.Command{
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
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		return runLocal(cmd, args)
	},
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

//nolint:gochecknoinits // required by cobra CLI pattern
func init() {
	rootCmd.PersistentFlags().StringVarP(
		&configPath, "config", "c", "",
		"Path to config file (default: auto-detect)",
	)
	rootCmd.PersistentFlags().StringVar(
		&tokenFlag, "token", "",
		"Auth token for the Git provider (overrides env var detection)",
	)
	rootCmd.PersistentFlags().BoolVar(
		&dryRun, "dry-run", false,
		"Show what would be done without making changes",
	)
	rootCmd.PersistentFlags().BoolVarP(
		&verbose, "verbose", "v", false,
		"Enable verbose output",
	)
}
