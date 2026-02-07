package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/azuredevops"
	"github.com/rios0rios0/autoupdate/internal/scanner"
	"github.com/rios0rios0/autoupdate/internal/upgrader"
	"github.com/spf13/cobra"
)

var (
	targetBranch   string
	commitMessage  string
	prTitle        string
	prDescription  string
	autoComplete   bool
	specificModule string
	targetVersion  string
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Create PRs to upgrade Terraform module dependencies",
	Long: `Scan all accessible Azure DevOps repositories for Terraform files,
identify outdated Git-based module dependencies, and create Pull Requests
to upgrade them to the latest available versions.`,
	RunE: runUpgrade,
}

func init() {
	upgradeCmd.Flags().StringVar(&targetBranch, "target-branch", "main", "Target branch for PRs")
	upgradeCmd.Flags().StringVar(&commitMessage, "commit-message", "", "Custom commit message (default: auto-generated)")
	upgradeCmd.Flags().StringVar(&prTitle, "pr-title", "", "Custom PR title (default: auto-generated)")
	upgradeCmd.Flags().StringVar(&prDescription, "pr-description", "", "Custom PR description (default: auto-generated)")
	upgradeCmd.Flags().BoolVar(&autoComplete, "auto-complete", false, "Set auto-complete on created PRs")
	upgradeCmd.Flags().StringVar(&specificModule, "module", "", "Only upgrade a specific module (e.g., terraform-modules/networking)")
	upgradeCmd.Flags().StringVar(&targetVersion, "version", "", "Upgrade to a specific version (requires --module)")
	upgradeCmd.Flags().StringVar(&projectFilter, "project", "", "Filter by project name (optional)")
	upgradeCmd.Flags().StringVar(&repoFilter, "repo", "", "Filter by repository name (optional)")
	rootCmd.AddCommand(upgradeCmd)
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	client, err := createClient()
	if err != nil {
		return err
	}

	if targetVersion != "" && specificModule == "" {
		return fmt.Errorf("--version requires --module to be specified")
	}

	fmt.Println("🚀 Starting Terraform dependency upgrade process...")
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

	// First, build a map of all module repositories and their latest tags
	fmt.Println("📦 Discovering module repositories and versions...")
	moduleVersions := make(map[string][]string) // source -> available versions

	for _, project := range projects {
		repos, err := client.GetRepositories(ctx, project.ID)
		if err != nil {
			continue
		}

		for _, repo := range repos {
			// Check if this repo looks like a Terraform module
			if isTerraformModule(repo.Name) || hasModuleStructure(ctx, client, project.ID, repo.ID) {
				tags, err := client.GetTags(ctx, project.ID, repo.ID)
				if err != nil {
					continue
				}

				// Build the source path for this module
				source := buildModuleSource(client.Organization(), project.Name, repo.Name)
				moduleVersions[source] = tags

				if verbose && len(tags) > 0 {
					fmt.Printf("   Found module: %s with %d versions (latest: %s)\n", source, len(tags), tags[0])
				}
			}
		}
	}

	fmt.Printf("   Found %d module repositories\n\n", len(moduleVersions))

	// Now scan all repos for dependencies and create upgrade PRs
	fmt.Println("🔍 Scanning repositories for outdated dependencies...")

	var upgrades []upgrader.UpgradeTask
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
			// Get Terraform files from the repository
			tfFiles, err := client.GetTerraformFiles(ctx, project.ID, repo.ID)
			if err != nil {
				continue
			}

			if len(tfFiles) == 0 {
				continue
			}

			// Scan each file for module dependencies
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
					// Skip if we're filtering for a specific module
					if specificModule != "" && !strings.Contains(dep.Source, specificModule) {
						continue
					}

					// Find available versions for this dependency
					latestVersion := findLatestVersion(dep.Source, moduleVersions)
					if latestVersion == "" {
						continue
					}

					// Use target version if specified, otherwise use latest
					newVersion := latestVersion
					if targetVersion != "" {
						newVersion = targetVersion
					}

					// Check if upgrade is needed
					if dep.Version != newVersion && upgrader.IsNewerVersion(dep.Version, newVersion) {
						upgrades = append(upgrades, upgrader.UpgradeTask{
							Project:     project,
							Repository:  repo,
							FilePath:    file.Path,
							Dependency:  dep,
							CurrentVer:  dep.Version,
							NewVersion:  newVersion,
							FileContent: content,
						})
					}
				}
			}
		}
	}

	if len(upgrades) == 0 {
		fmt.Println("✅ All dependencies are up to date!")
		return nil
	}

	fmt.Printf("   Found %d dependencies that need upgrading\n\n", len(upgrades))

	// Group upgrades by repository
	repoUpgrades := groupByRepository(upgrades)

	fmt.Println("📝 Creating Pull Requests...")
	fmt.Println()

	createdPRs := 0
	for repoKey, tasks := range repoUpgrades {
		if dryRun {
			fmt.Printf("   [DRY RUN] Would create PR for %s:\n", repoKey)
			for _, task := range tasks {
				fmt.Printf("      - %s: %s -> %s\n", task.Dependency.Source, task.CurrentVer, task.NewVersion)
			}
			continue
		}

		pr, err := createUpgradePR(ctx, client, tasks)
		if err != nil {
			fmt.Printf("   ❌ Failed to create PR for %s: %v\n", repoKey, err)
			continue
		}

		fmt.Printf("   ✅ Created PR #%d for %s: %s\n", pr.ID, repoKey, pr.URL)
		createdPRs++
	}

	fmt.Println()
	if dryRun {
		fmt.Printf("🏁 Dry run complete. Would have created %d PRs.\n", len(repoUpgrades))
	} else {
		fmt.Printf("🏁 Upgrade complete. Created %d PRs.\n", createdPRs)
	}

	return nil
}

func isTerraformModule(repoName string) bool {
	name := strings.ToLower(repoName)
	return strings.Contains(name, "terraform-module") ||
		strings.Contains(name, "tf-module") ||
		strings.HasPrefix(name, "module-")
}

func hasModuleStructure(ctx context.Context, client *azuredevops.Client, projectID, repoID string) bool {
	// Check if repo has typical module structure (main.tf, variables.tf, outputs.tf at root)
	items, err := client.GetRepositoryItems(ctx, projectID, repoID, "/")
	if err != nil {
		return false
	}

	hasMain := false
	hasVariables := false
	for _, item := range items {
		if item.Path == "/main.tf" {
			hasMain = true
		}
		if item.Path == "/variables.tf" {
			hasVariables = true
		}
	}

	return hasMain && hasVariables
}

func buildModuleSource(org, project, repo string) string {
	// Build Azure DevOps Git source URL format
	// git::https://dev.azure.com/org/project/_git/repo
	return fmt.Sprintf("git::https://%s/%s/_git/%s", org, project, repo)
}

func findLatestVersion(source string, moduleVersions map[string][]string) string {
	// Try exact match first
	if versions, ok := moduleVersions[source]; ok && len(versions) > 0 {
		return versions[0]
	}

	// Extract repo name from source for matching
	sourceRepoName := extractRepoName(source)

	// Try exact repo name match (in case source format differs slightly)
	for moduleSource, versions := range moduleVersions {
		moduleRepoName := extractRepoName(moduleSource)
		// Use exact repo name comparison to avoid partial matches
		// e.g., "helm_opensearch" should not match "helm_opensearch_dashboards"
		if sourceRepoName == moduleRepoName && len(versions) > 0 {
			return versions[0]
		}
	}

	return ""
}

func extractRepoName(source string) string {
	parts := strings.Split(source, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return source
}

func groupByRepository(upgrades []upgrader.UpgradeTask) map[string][]upgrader.UpgradeTask {
	result := make(map[string][]upgrader.UpgradeTask)
	for _, u := range upgrades {
		key := fmt.Sprintf("%s/%s", u.Project.Name, u.Repository.Name)
		result[key] = append(result[key], u)
	}
	return result
}

func createUpgradePR(ctx context.Context, client *azuredevops.Client, tasks []upgrader.UpgradeTask) (*azuredevops.PullRequest, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("no tasks provided")
	}

	project := tasks[0].Project
	repo := tasks[0].Repository

	// Create a unique branch name
	branchName := fmt.Sprintf("terraform-deps-upgrade/%s", generateBranchSuffix(tasks))

	// Get the latest commit on target branch
	defaultBranch := repo.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = "refs/heads/" + targetBranch
	}

	// Group tasks by file path to avoid multiple operations on the same file
	fileTasksMap := make(map[string][]upgrader.UpgradeTask)
	for _, task := range tasks {
		fileTasksMap[task.FilePath] = append(fileTasksMap[task.FilePath], task)
	}

	// Prepare file changes - apply all upgrades to each file
	var changes []azuredevops.FileChange
	for filePath, fileTasks := range fileTasksMap {
		// Start with the original content and apply all upgrades sequentially
		content := fileTasks[0].FileContent
		for _, task := range fileTasks {
			content = upgrader.ApplyUpgrade(content, task.Dependency, task.NewVersion)
		}
		changes = append(changes, azuredevops.FileChange{
			Path:       filePath,
			Content:    content,
			ChangeType: "edit",
		})
	}

	// Create branch and push changes
	err := client.CreateBranchWithChanges(ctx, project.ID, repo.ID, branchName, defaultBranch, changes, generateCommitMessage(tasks))
	if err != nil {
		return nil, fmt.Errorf("failed to create branch: %w", err)
	}

	// Create pull request
	title := prTitle
	if title == "" {
		title = generatePRTitle(tasks)
	}

	description := prDescription
	if description == "" {
		description = generatePRDescription(tasks)
	}

	pr, err := client.CreatePullRequest(ctx, project.ID, repo.ID, azuredevops.CreatePRRequest{
		SourceBranch: "refs/heads/" + branchName,
		TargetBranch: defaultBranch,
		Title:        title,
		Description:  description,
		AutoComplete: autoComplete,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}

	return pr, nil
}

func generateBranchSuffix(tasks []upgrader.UpgradeTask) string {
	if len(tasks) == 1 {
		return fmt.Sprintf("%s-%s", extractRepoName(tasks[0].Dependency.Source), tasks[0].NewVersion)
	}
	return fmt.Sprintf("batch-%d-modules", len(tasks))
}

func generateCommitMessage(tasks []upgrader.UpgradeTask) string {
	if commitMessage != "" {
		return commitMessage
	}

	if len(tasks) == 1 {
		return fmt.Sprintf("chore(deps): upgrade %s from %s to %s",
			extractRepoName(tasks[0].Dependency.Source),
			tasks[0].CurrentVer,
			tasks[0].NewVersion)
	}

	return fmt.Sprintf("chore(deps): upgrade %d Terraform module dependencies", len(tasks))
}

func generatePRTitle(tasks []upgrader.UpgradeTask) string {
	if len(tasks) == 1 {
		return fmt.Sprintf("chore(deps): Upgrade %s to %s",
			extractRepoName(tasks[0].Dependency.Source),
			tasks[0].NewVersion)
	}

	return fmt.Sprintf("chore(deps): Upgrade %d Terraform module dependencies", len(tasks))
}

func generatePRDescription(tasks []upgrader.UpgradeTask) string {
	var sb strings.Builder
	sb.WriteString("## Summary\n\n")
	sb.WriteString("This PR upgrades the following Terraform module dependencies:\n\n")

	sb.WriteString("| Module | Current Version | New Version | File |\n")
	sb.WriteString("|--------|-----------------|-------------|------|\n")

	for _, task := range tasks {
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
			extractRepoName(task.Dependency.Source),
			task.CurrentVer,
			task.NewVersion,
			task.FilePath))
	}

	sb.WriteString("\n---\n")
	sb.WriteString("*This PR was automatically created by [autoupdate](https://github.com/rios0rios0/autoupdate)*\n")

	return sb.String()
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
