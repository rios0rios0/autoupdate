package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/rios0rios0/autoupdate/internal/azuredevops"
	"github.com/rios0rios0/autoupdate/internal/scanner"
	"github.com/spf13/cobra"
)

var (
	projectFilter string
	repoFilter    string
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan repositories for Terraform module dependencies",
	Long: `Scan all accessible Azure DevOps repositories for Terraform files and
identify Git-based module dependencies with their current versions.`,
	RunE: runScan,
}

func init() {
	scanCmd.Flags().StringVar(&projectFilter, "project", "", "Filter by project name (optional)")
	scanCmd.Flags().StringVar(&repoFilter, "repo", "", "Filter by repository name (optional)")
	rootCmd.AddCommand(scanCmd)
}

func runScan(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	client, err := createClient()
	if err != nil {
		return err
	}

	fmt.Println("🔍 Scanning Azure DevOps for Terraform dependencies...")
	fmt.Println()

	// Get all projects
	projects, err := client.GetProjects(ctx)
	if err != nil {
		return fmt.Errorf("failed to get projects: %w", err)
	}

	if verbose {
		fmt.Printf("Found %d projects\n", len(projects))
	}

	// Filter projects if specified
	if projectFilter != "" {
		var filtered []azuredevops.Project
		for _, p := range projects {
			if p.Name == projectFilter {
				filtered = append(filtered, p)
			}
		}
		projects = filtered
	}

	totalDeps := 0
	for _, project := range projects {
		repos, err := client.GetRepositories(ctx, project.ID)
		if err != nil {
			fmt.Printf("⚠️  Warning: failed to get repos for project %s: %v\n", project.Name, err)
			continue
		}

		// Filter repos if specified
		if repoFilter != "" {
			var filtered []azuredevops.Repository
			for _, r := range repos {
				if r.Name == repoFilter {
					filtered = append(filtered, r)
				}
			}
			repos = filtered
		}

		for _, repo := range repos {
			// Get Terraform files from the repository
			tfFiles, err := client.GetTerraformFiles(ctx, project.ID, repo.ID)
			if err != nil {
				if verbose {
					fmt.Printf("  ⚠️  Warning: failed to scan repo %s/%s: %v\n", project.Name, repo.Name, err)
				}
				continue
			}

			if len(tfFiles) == 0 {
				continue
			}

			// Scan each file for module dependencies
			var repoDeps []scanner.ModuleDependency
			for _, file := range tfFiles {
				content, err := client.GetFileContent(ctx, project.ID, repo.ID, file.Path)
				if err != nil {
					continue
				}

				deps, err := scanner.ScanTerraformFile(content, file.Path)
				if err != nil {
					if verbose {
						fmt.Printf("  ⚠️  Warning: failed to parse %s: %v\n", file.Path, err)
					}
					continue
				}

				repoDeps = append(repoDeps, deps...)
			}

			if len(repoDeps) > 0 {
				fmt.Printf("📁 %s/%s\n", project.Name, repo.Name)
				for _, dep := range repoDeps {
					fmt.Printf("   └─ %s @ %s (from %s)\n", dep.Source, dep.Version, dep.FilePath)
					totalDeps++
				}
				fmt.Println()
			}
		}
	}

	fmt.Printf("✅ Found %d Terraform module dependencies across all repositories\n", totalDeps)
	return nil
}

func createClient() (*azuredevops.Client, error) {
	// Default values for testing
	const defaultOrg = "https://dev.azure.com/???"
	const defaultPAT = "???"

	org := organization
	if org == "" {
		org = os.Getenv("AZURE_DEVOPS_ORG")
	}
	if org == "" {
		org = defaultOrg
	}

	token := pat
	if token == "" {
		token = os.Getenv("AZURE_DEVOPS_PAT")
	}
	if token == "" {
		token = defaultPAT
	}

	return azuredevops.NewClient(org, token), nil
}
