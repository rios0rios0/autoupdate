package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/azuredevops"
	"github.com/rios0rios0/autoupdate/internal/scanner"
	"github.com/rios0rios0/autoupdate/internal/upgrader"
	"github.com/spf13/cobra"
)

var (
	showOutdatedOnly bool
	outputFormat     string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all Terraform module dependencies with their versions",
	Long: `List all Terraform module dependencies across Azure DevOps repositories,
showing current versions and available updates.`,
	RunE: runList,
}

func init() {
	listCmd.Flags().BoolVar(&showOutdatedOnly, "outdated", false, "Show only outdated dependencies")
	listCmd.Flags().StringVar(&outputFormat, "output", "table", "Output format: table, json, or markdown")
	listCmd.Flags().StringVar(&projectFilter, "project", "", "Filter by project name (optional)")
	listCmd.Flags().StringVar(&repoFilter, "repo", "", "Filter by repository name (optional)")
	rootCmd.AddCommand(listCmd)
}

type dependencyInfo struct {
	Project      string
	Repository   string
	ModuleName   string
	Source       string
	CurrentVer   string
	LatestVer    string
	FilePath     string
	NeedsUpgrade bool
	UpgradeType  string
}

func runList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	client, err := createClient()
	if err != nil {
		return err
	}

	fmt.Println("📋 Listing Terraform module dependencies...")
	fmt.Println()

	// Get all projects
	projects, err := client.GetProjects(ctx)
	if err != nil {
		return fmt.Errorf("failed to get projects: %w", err)
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

	// Build module versions map
	moduleVersions := make(map[string][]string)
	for _, project := range projects {
		repos, err := client.GetRepositories(ctx, project.ID)
		if err != nil {
			continue
		}

		for _, repo := range repos {
			if isTerraformModule(repo.Name) || hasModuleStructure(ctx, client, project.ID, repo.ID) {
				tags, err := client.GetTags(ctx, project.ID, repo.ID)
				if err != nil {
					continue
				}
				source := buildModuleSource(client.Organization(), project.Name, repo.Name)
				moduleVersions[source] = tags
			}
		}
	}

	// Collect all dependencies
	var allDeps []dependencyInfo

	for _, project := range projects {
		repos, err := client.GetRepositories(ctx, project.ID)
		if err != nil {
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
			tfFiles, err := client.GetTerraformFiles(ctx, project.ID, repo.ID)
			if err != nil {
				continue
			}

			for _, file := range tfFiles {
				content, err := client.GetFileContent(ctx, project.ID, repo.ID, file.Path)
				if err != nil {
					continue
				}

				deps, err := scanner.ScanTerraformFile(content, file.Path)
				if err != nil {
					continue
				}

				for _, dep := range deps {
					latestVer := findLatestVersion(dep.Source, moduleVersions)
					needsUpgrade := latestVer != "" && dep.Version != latestVer && upgrader.IsNewerVersion(dep.Version, latestVer)

					info := dependencyInfo{
						Project:      project.Name,
						Repository:   repo.Name,
						ModuleName:   dep.Name,
						Source:       extractRepoName(dep.Source),
						CurrentVer:   dep.Version,
						LatestVer:    latestVer,
						FilePath:     dep.FilePath,
						NeedsUpgrade: needsUpgrade,
					}

					if needsUpgrade {
						diff := upgrader.AnalyzeVersionDiff(dep.Version, latestVer)
						if diff.IsMajor {
							info.UpgradeType = "major"
						} else if diff.IsMinor {
							info.UpgradeType = "minor"
						} else {
							info.UpgradeType = "patch"
						}
					}

					if !showOutdatedOnly || needsUpgrade {
						allDeps = append(allDeps, info)
					}
				}
			}
		}
	}

	if len(allDeps) == 0 {
		fmt.Println("No Terraform module dependencies found.")
		return nil
	}

	// Output based on format
	switch outputFormat {
	case "json":
		printJSON(allDeps)
	case "markdown":
		printMarkdown(allDeps)
	default:
		printTable(allDeps)
	}

	return nil
}

func printTable(deps []dependencyInfo) {
	// Calculate column widths
	projectW := len("Project")
	repoW := len("Repository")
	moduleW := len("Module")
	sourceW := len("Source")
	currentW := len("Current")
	latestW := len("Latest")
	statusW := len("Status")

	for _, d := range deps {
		if len(d.Project) > projectW {
			projectW = len(d.Project)
		}
		if len(d.Repository) > repoW {
			repoW = len(d.Repository)
		}
		if len(d.ModuleName) > moduleW {
			moduleW = len(d.ModuleName)
		}
		if len(d.Source) > sourceW {
			sourceW = len(d.Source)
		}
		if len(d.CurrentVer) > currentW {
			currentW = len(d.CurrentVer)
		}
		if len(d.LatestVer) > latestW && d.LatestVer != "" {
			latestW = len(d.LatestVer)
		}
	}

	// Limit widths
	if projectW > 30 {
		projectW = 30
	}
	if repoW > 30 {
		repoW = 30
	}
	if sourceW > 40 {
		sourceW = 40
	}

	// Print header
	fmt.Printf("%-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s\n",
		projectW, "Project",
		repoW, "Repository",
		moduleW, "Module",
		sourceW, "Source",
		currentW, "Current",
		latestW, "Latest",
		statusW, "Status")

	fmt.Println(strings.Repeat("-", projectW+repoW+moduleW+sourceW+currentW+latestW+statusW+14))

	// Print rows
	for _, d := range deps {
		status := "✅ Up to date"
		if d.NeedsUpgrade {
			switch d.UpgradeType {
			case "major":
				status = "🔴 Major update"
			case "minor":
				status = "🟡 Minor update"
			default:
				status = "🟢 Patch update"
			}
		}

		latest := d.LatestVer
		if latest == "" {
			latest = "N/A"
		}

		fmt.Printf("%-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %s\n",
			projectW, truncate(d.Project, projectW),
			repoW, truncate(d.Repository, repoW),
			moduleW, d.ModuleName,
			sourceW, truncate(d.Source, sourceW),
			currentW, d.CurrentVer,
			latestW, latest,
			status)
	}

	// Summary
	outdated := 0
	for _, d := range deps {
		if d.NeedsUpgrade {
			outdated++
		}
	}

	fmt.Println()
	fmt.Printf("Total: %d dependencies, %d outdated\n", len(deps), outdated)
}

func printMarkdown(deps []dependencyInfo) {
	fmt.Println("| Project | Repository | Module | Source | Current | Latest | Status |")
	fmt.Println("|---------|------------|--------|--------|---------|--------|--------|")

	for _, d := range deps {
		status := "✅ Up to date"
		if d.NeedsUpgrade {
			switch d.UpgradeType {
			case "major":
				status = "🔴 Major"
			case "minor":
				status = "🟡 Minor"
			default:
				status = "🟢 Patch"
			}
		}

		latest := d.LatestVer
		if latest == "" {
			latest = "N/A"
		}

		fmt.Printf("| %s | %s | %s | %s | %s | %s | %s |\n",
			d.Project, d.Repository, d.ModuleName, d.Source, d.CurrentVer, latest, status)
	}
}

func printJSON(deps []dependencyInfo) {
	fmt.Println("[")
	for i, d := range deps {
		comma := ","
		if i == len(deps)-1 {
			comma = ""
		}
		fmt.Printf(`  {"project": "%s", "repository": "%s", "module": "%s", "source": "%s", "current": "%s", "latest": "%s", "needsUpgrade": %t}%s`+"\n",
			d.Project, d.Repository, d.ModuleName, d.Source, d.CurrentVer, d.LatestVer, d.NeedsUpgrade, comma)
	}
	fmt.Println("]")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
