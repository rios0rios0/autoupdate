package cmd

import (
	"github.com/spf13/cobra"
)

var (
	// Global flags
	organization string
	pat          string
	dryRun       bool
	verbose      bool
)

var rootCmd = &cobra.Command{
	Use:   "autoupdate",
	Short: "Terraform dependency autoupdate and upgrader for Azure DevOps",
	Long: `A CLI tool that scans Azure DevOps repositories for Terraform module dependencies,
detects outdated versions, and creates Pull Requests to upgrade them automatically.

This tool helps maintain consistency across your infrastructure as code by:
- Scanning all projects you have access to
- Detecting Git-based Terraform module dependencies
- Identifying newer versions (tags) available
- Creating PRs to upgrade dependencies`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&organization, "organization", "o", "", "Azure DevOps organization URL (e.g., https://dev.azure.com/MyOrg)")
	rootCmd.PersistentFlags().StringVarP(&pat, "pat", "p", "", "Personal Access Token for Azure DevOps (or set AZURE_DEVOPS_PAT env var)")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "Show what would be done without making changes")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
}
