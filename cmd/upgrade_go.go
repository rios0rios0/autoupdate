package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/azuredevops"
	"github.com/rios0rios0/autoupdate/internal/golang"
	"github.com/spf13/cobra"
)

var (
	goTargetBranch  string
	goProjectFilter string
	goRepoFilter    string
)

var upgradeGoCmd = &cobra.Command{
	Use:   "upgrade-go",
	Short: "Create PRs to upgrade Go projects (go.mod version + dependencies)",
	Long: `Scan all accessible Azure DevOps repositories for Go projects (with go.mod),
upgrade the Go version to the latest stable release, run 'go get -u ./...' to
update all dependencies, and create Pull Requests with the changes.

If a repository has a 'config.sh' file at the root, it will be sourced before
running go commands (to set GOPRIVATE, git credentials, etc.).`,
	RunE: runUpgradeGo,
}

func init() {
	upgradeGoCmd.Flags().StringVar(&goTargetBranch, "target-branch", "main", "Target branch for PRs")
	upgradeGoCmd.Flags().StringVar(&goProjectFilter, "project", "", "Filter by project name (optional)")
	upgradeGoCmd.Flags().StringVar(&goRepoFilter, "repo", "", "Filter by repository name (optional)")
	rootCmd.AddCommand(upgradeGoCmd)
}

func runUpgradeGo(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	client, err := createClient()
	if err != nil {
		return err
	}

	fmt.Println("🚀 Starting Go dependency upgrade process...")
	fmt.Println()

	// Fetch latest stable Go version
	fmt.Println("🔍 Fetching latest stable Go version...")
	latestGoVersion, err := golang.FetchLatestGoVersion()
	if err != nil {
		return fmt.Errorf("failed to fetch latest Go version: %w", err)
	}
	fmt.Printf("   Latest stable Go version: %s\n\n", latestGoVersion)

	// Get all projects
	projects, err := client.GetProjects(ctx)
	if err != nil {
		return fmt.Errorf("failed to get projects: %w", err)
	}

	// Filter projects if specified
	if goProjectFilter != "" {
		var filtered []azuredevops.Project
		for _, p := range projects {
			if p.Name == goProjectFilter {
				filtered = append(filtered, p)
			}
		}
		projects = filtered
	}

	fmt.Println("📦 Scanning for Go projects...")

	type goRepo struct {
		project     azuredevops.Project
		repo        azuredevops.Repository
		hasConfigSH bool
	}

	var goRepos []goRepo

	for _, project := range projects {
		repos, err := client.GetRepositories(ctx, project.ID)
		if err != nil {
			if verbose {
				fmt.Printf("   ⚠️  Warning: failed to get repos for project %s: %v\n", project.Name, err)
			}
			continue
		}

		// Filter repos if specified
		if goRepoFilter != "" {
			var filtered []azuredevops.Repository
			for _, r := range repos {
				if r.Name == goRepoFilter {
					filtered = append(filtered, r)
				}
			}
			repos = filtered
		}

		for _, repo := range repos {
			// Check if this is a Go project (has go.mod at root)
			if !client.HasFile(ctx, project.ID, repo.ID, "/go.mod") {
				continue
			}

			// Check if config.sh exists
			hasConfigSH := client.HasFile(ctx, project.ID, repo.ID, "/config.sh")

			goRepos = append(goRepos, goRepo{
				project:     project,
				repo:        repo,
				hasConfigSH: hasConfigSH,
			})

			configNote := ""
			if hasConfigSH {
				configNote = " (has config.sh)"
			}
			fmt.Printf("   Found Go project: %s/%s%s\n", project.Name, repo.Name, configNote)
		}
	}

	if len(goRepos) == 0 {
		fmt.Println("   No Go projects found.")
		return nil
	}

	fmt.Printf("\n   Found %d Go projects\n\n", len(goRepos))

	if dryRun {
		fmt.Println("📝 [DRY RUN] Would create PRs for:")
		for _, gr := range goRepos {
			configNote := ""
			if gr.hasConfigSH {
				configNote = " (will run config.sh first)"
			}
			fmt.Printf("   - %s/%s: upgrade Go to %s + update dependencies%s\n",
				gr.project.Name, gr.repo.Name, latestGoVersion, configNote)
		}
		fmt.Printf("\n🏁 Dry run complete. Would have processed %d Go projects.\n", len(goRepos))
		return nil
	}

	// Process each Go project
	fmt.Println("📝 Upgrading Go projects and creating PRs...")
	fmt.Println()

	createdPRs := 0
	for _, gr := range goRepos {
		repoKey := fmt.Sprintf("%s/%s", gr.project.Name, gr.repo.Name)
		fmt.Printf("   ⏳ Processing %s...\n", repoKey)

		// Determine clone URL
		cloneURL := gr.repo.RemoteURL
		if cloneURL == "" {
			// Construct it if not available
			cloneURL = fmt.Sprintf("%s/%s/_git/%s", client.BaseURL(), gr.project.Name, gr.repo.Name)
		}

		// Build branch name
		branchName := fmt.Sprintf("go-deps-upgrade/go-%s", latestGoVersion)

		// Determine default branch
		defaultBranch := gr.repo.DefaultBranch
		if defaultBranch == "" {
			defaultBranch = "refs/heads/" + goTargetBranch
		}

		// Run the upgrade
		result, err := golang.UpgradeGoRepo(ctx, golang.UpgradeParams{
			CloneURL:      client.AuthCloneURL(cloneURL),
			DefaultBranch: defaultBranch,
			BranchName:    branchName,
			GoVersion:     latestGoVersion,
			PAT:           client.Token(),
			HasConfigSH:   gr.hasConfigSH,
			Verbose:       verbose,
		})

		if err != nil {
			fmt.Printf("   ❌ Failed to upgrade %s: %v\n", repoKey, err)
			if verbose && result != nil {
				fmt.Printf("      Output:\n%s\n", indentOutput(result.Output))
			}
			continue
		}

		if verbose && result != nil {
			fmt.Printf("      Output:\n%s\n", indentOutput(result.Output))
		}

		if !result.HasChanges {
			fmt.Printf("   ✅ %s is already up to date\n", repoKey)
			continue
		}

		// Create pull request via API
		prTitleStr := fmt.Sprintf("chore(deps): Upgrade Go to %s and update dependencies", latestGoVersion)
		prDescStr := generateGoPRDescription(repoKey, latestGoVersion, gr.hasConfigSH)

		pr, err := client.CreatePullRequest(ctx, gr.project.ID, gr.repo.ID, azuredevops.CreatePRRequest{
			SourceBranch: "refs/heads/" + branchName,
			TargetBranch: defaultBranch,
			Title:        prTitleStr,
			Description:  prDescStr,
			AutoComplete: autoComplete,
		})
		if err != nil {
			fmt.Printf("   ❌ Failed to create PR for %s: %v\n", repoKey, err)
			continue
		}

		fmt.Printf("   ✅ Created PR #%d for %s: %s\n", pr.ID, repoKey, pr.URL)
		createdPRs++
	}

	fmt.Println()
	fmt.Printf("🏁 Go upgrade complete. Created %d PRs out of %d Go projects.\n", createdPRs, len(goRepos))

	return nil
}

func generateGoPRDescription(repoKey, goVersion string, hasConfigSH bool) string {
	var sb strings.Builder
	sb.WriteString("## Summary\n\n")
	sb.WriteString(fmt.Sprintf("This PR upgrades the Go version to **%s** and updates all module dependencies.\n\n", goVersion))

	sb.WriteString("### Changes\n\n")
	sb.WriteString(fmt.Sprintf("- Updated `go.mod` Go directive to `%s`\n", goVersion))
	sb.WriteString("- Ran `go get -u ./...` to update all dependencies\n")
	sb.WriteString("- Ran `go mod tidy` to clean up\n")

	if hasConfigSH {
		sb.WriteString("- `config.sh` was sourced before running Go commands (private package settings)\n")
	}

	sb.WriteString("\n### Review Checklist\n\n")
	sb.WriteString("- [ ] Verify build passes\n")
	sb.WriteString("- [ ] Verify tests pass\n")
	sb.WriteString("- [ ] Review dependency changes in `go.sum`\n")

	sb.WriteString("\n---\n")
	sb.WriteString("*This PR was automatically created by [autoupdate](https://github.com/rios0rios0/autoupdate)*\n")

	return sb.String()
}

func indentOutput(output string) string {
	lines := strings.Split(output, "\n")
	var indented []string
	for _, line := range lines {
		indented = append(indented, "      "+line)
	}
	return strings.Join(indented, "\n")
}
