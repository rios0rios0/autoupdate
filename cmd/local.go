package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	logger "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/rios0rios0/autoupdate/domain"
	goUpdater "github.com/rios0rios0/autoupdate/infrastructure/updater/golang"
	jsUpdater "github.com/rios0rios0/autoupdate/infrastructure/updater/javascript"
	pyUpdater "github.com/rios0rios0/autoupdate/infrastructure/updater/python"
)

const (
	providerGitHub      = "github"
	providerAzureDevOps = "azuredevops"
	providerGitLab      = "gitlab"
)

// remoteInfo holds the parsed components of a Git remote URL.
type remoteInfo struct {
	ProviderType string
	Org          string
	Project      string // Azure DevOps only
	RepoName     string
}

// projectType identifies the detected project ecosystem.
type projectType string

const (
	projectGo         projectType = "golang"
	projectPython     projectType = "python"
	projectJavaScript projectType = "javascript"
)

// localPRInfo holds the information needed to create a PR after a local upgrade.
type localPRInfo struct {
	BranchName     string
	LatestVersion  string
	VersionUpdated bool
	PackageManager string // JavaScript only
	ProjectType    projectType
	HasChanges     bool
}

// runLocal is the entry point for the standalone local mode.
// It upgrades dependencies in the given directory, pushes a branch,
// and creates a PR by auto-detecting the Git provider and project type.
func runLocal(_ *cobra.Command, args []string) error {
	ctx := context.Background()

	repoDir, err := filepath.Abs(args[0])
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Detect project type
	projType, detectErr := detectProjectType(repoDir)
	if detectErr != nil {
		return detectErr
	}
	logger.Infof("Detected project type: %s", projType)

	// Detect Git provider from remote URL
	remote, parseErr := parseGitRemote(ctx, repoDir)
	if parseErr != nil {
		return fmt.Errorf("failed to detect git provider: %w", parseErr)
	}
	logger.Infof("Detected provider: %s, org: %s, repo: %s", remote.ProviderType, remote.Org, remote.RepoName)

	// Resolve auth token
	token := tokenFlag
	if token == "" {
		token = resolveTokenFromEnv(remote.ProviderType)
	}

	if !dryRun && token == "" {
		return fmt.Errorf(
			"no auth token found for %s; set --token or the appropriate env var (%s)",
			remote.ProviderType, tokenEnvHint(remote.ProviderType),
		)
	}

	// Detect current branch (used as the PR target / default branch)
	defaultBranch, branchErr := detectDefaultBranch(ctx, repoDir)
	if branchErr != nil {
		return fmt.Errorf("failed to detect current branch: %w", branchErr)
	}
	logger.Infof("Default branch: %s", defaultBranch)

	// Run the appropriate upgrade
	prInfo, upgradeErr := runLocalUpgrade(ctx, repoDir, projType, remote.ProviderType, token)
	if upgradeErr != nil {
		return upgradeErr
	}

	if dryRun {
		return nil
	}

	if !prInfo.HasChanges {
		logger.Info("No dependency changes detected, nothing to do.")
		return nil
	}

	// Build repository struct for the provider API.
	repo := domain.Repository{
		ID:            remote.RepoName,
		Name:          remote.RepoName,
		Organization:  remote.Org,
		Project:       remote.Project,
		DefaultBranch: defaultBranch,
	}

	return createLocalPRForProject(ctx, remote.ProviderType, token, repo, prInfo)
}

// detectProjectType determines what kind of project is in the given directory.
func detectProjectType(repoDir string) (projectType, error) {
	// Check in order of specificity
	if _, err := os.Stat(filepath.Join(repoDir, "go.mod")); err == nil {
		return projectGo, nil
	}
	if _, err := os.Stat(filepath.Join(repoDir, "package.json")); err == nil {
		return projectJavaScript, nil
	}
	if _, err := os.Stat(filepath.Join(repoDir, "requirements.txt")); err == nil {
		return projectPython, nil
	}
	if _, err := os.Stat(filepath.Join(repoDir, "pyproject.toml")); err == nil {
		return projectPython, nil
	}
	return "", fmt.Errorf(
		"no supported project found in %s — expected go.mod, package.json, requirements.txt, or pyproject.toml",
		repoDir,
	)
}

// runLocalUpgrade dispatches to the appropriate updater based on project type.
func runLocalUpgrade(
	ctx context.Context,
	repoDir string,
	projType projectType,
	providerType, token string,
) (*localPRInfo, error) {
	switch projType {
	case projectGo:
		return runGoLocalUpgrade(ctx, repoDir, providerType, token)
	case projectPython:
		return runPythonLocalUpgrade(ctx, repoDir, providerType, token)
	case projectJavaScript:
		return runJSLocalUpgrade(ctx, repoDir, providerType, token)
	default:
		return nil, fmt.Errorf("unsupported project type: %s", projType)
	}
}

func runGoLocalUpgrade(
	ctx context.Context,
	repoDir, providerType, token string,
) (*localPRInfo, error) {
	result, err := goUpdater.RunLocalUpgrade(ctx, repoDir, goUpdater.LocalUpgradeOptions{
		DryRun:       dryRun,
		Verbose:      verbose,
		AuthToken:    token,
		ProviderName: providerType,
	})
	if err != nil {
		return nil, err
	}
	return &localPRInfo{
		BranchName:     result.BranchName,
		LatestVersion:  result.LatestVersion,
		VersionUpdated: result.GoVersionUpdated,
		ProjectType:    projectGo,
		HasChanges:     result.HasChanges,
	}, nil
}

func runPythonLocalUpgrade(
	ctx context.Context,
	repoDir, providerType, token string,
) (*localPRInfo, error) {
	result, err := pyUpdater.RunLocalUpgrade(ctx, repoDir, pyUpdater.LocalUpgradeOptions{
		DryRun:       dryRun,
		Verbose:      verbose,
		AuthToken:    token,
		ProviderName: providerType,
	})
	if err != nil {
		return nil, err
	}
	return &localPRInfo{
		BranchName:     result.BranchName,
		LatestVersion:  result.LatestVersion,
		VersionUpdated: result.PythonVersionUpdated,
		ProjectType:    projectPython,
		HasChanges:     result.HasChanges,
	}, nil
}

func runJSLocalUpgrade(
	ctx context.Context,
	repoDir, providerType, token string,
) (*localPRInfo, error) {
	result, err := jsUpdater.RunLocalUpgrade(ctx, repoDir, jsUpdater.LocalUpgradeOptions{
		DryRun:       dryRun,
		Verbose:      verbose,
		AuthToken:    token,
		ProviderName: providerType,
	})
	if err != nil {
		return nil, err
	}
	return &localPRInfo{
		BranchName:     result.BranchName,
		LatestVersion:  result.LatestVersion,
		VersionUpdated: result.NodeVersionUpdated,
		PackageManager: result.PackageManager,
		ProjectType:    projectJavaScript,
		HasChanges:     result.HasChanges,
	}, nil
}

// createLocalPRForProject creates a pull request using the provider API after
// a successful local upgrade, adapting the title and description based on
// the project type.
func createLocalPRForProject(
	ctx context.Context,
	providerType, token string,
	repo domain.Repository,
	info *localPRInfo,
) error {
	provRegistry := buildProviderRegistry()
	provider, err := provRegistry.Get(providerType, token)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	prTitle, prDesc := generatePRContent(info)

	targetBranch := repo.DefaultBranch
	if !strings.HasPrefix(targetBranch, "refs/heads/") {
		targetBranch = "refs/heads/" + targetBranch
	}

	pr, createErr := provider.CreatePullRequest(ctx, repo, domain.PullRequestInput{
		SourceBranch: "refs/heads/" + info.BranchName,
		TargetBranch: targetBranch,
		Title:        prTitle,
		Description:  prDesc,
	})
	if createErr != nil {
		return fmt.Errorf("failed to create PR: %w", createErr)
	}

	logger.Infof("Created PR #%d: %s", pr.ID, pr.URL)
	return nil
}

// generatePRContent returns the title and description for a PR based on
// the project type and upgrade result.
func generatePRContent(info *localPRInfo) (string, string) {
	switch info.ProjectType {
	case projectGo:
		title := "chore(deps): update Go module dependencies"
		if info.VersionUpdated {
			title = fmt.Sprintf(
				"chore(deps): upgraded Go version to `%s` and updated all dependencies",
				info.LatestVersion,
			)
		}
		desc := goUpdater.GenerateGoPRDescription(
			info.LatestVersion, false, info.VersionUpdated,
		)
		return title, desc

	case projectPython:
		title := "chore(deps): updated Python dependencies"
		if info.VersionUpdated {
			title = fmt.Sprintf(
				"chore(deps): upgraded Python to `%s` and updated all dependencies",
				info.LatestVersion,
			)
		}
		desc := pyUpdater.GeneratePRDescription(
			info.LatestVersion, info.VersionUpdated,
		)
		return title, desc

	case projectJavaScript:
		title := "chore(deps): updated JavaScript dependencies"
		if info.VersionUpdated {
			title = fmt.Sprintf(
				"chore(deps): upgraded Node.js to `%s` and updated all dependencies",
				info.LatestVersion,
			)
		}
		desc := jsUpdater.GeneratePRDescription(
			info.LatestVersion, info.PackageManager, info.VersionUpdated,
		)
		return title, desc

	default:
		return "chore(deps): updated dependencies", "Automated dependency update."
	}
}

// ---------------------------------------------------------------------------
// Git remote parsing
// ---------------------------------------------------------------------------

// parseGitRemote runs `git remote get-url origin` in the given directory
// and parses the result to extract provider type, organisation, project
// (Azure DevOps only), and repository name.
func parseGitRemote(ctx context.Context, repoDir string) (*remoteInfo, error) {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = repoDir

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git remote get-url origin: %w", err)
	}

	return parseRemoteURL(strings.TrimSpace(string(output)))
}

// parseRemoteURL extracts provider, org, project, and repo name from a
// Git remote URL.  It supports HTTPS and SSH formats for GitHub, GitLab,
// and Azure DevOps.
func parseRemoteURL(rawURL string) (*remoteInfo, error) {
	cleaned := strings.TrimSuffix(rawURL, ".git")

	// --- Azure DevOps ---
	if strings.Contains(cleaned, "dev.azure.com") || strings.Contains(cleaned, "ssh.dev.azure.com") {
		return parseAzureDevOpsURL(cleaned)
	}

	// --- GitHub ---
	if strings.Contains(cleaned, "github.com") {
		o, r, e := parseStandardGitURL(cleaned, "github.com")
		if e != nil {
			return nil, e
		}
		return &remoteInfo{ProviderType: providerGitHub, Org: o, RepoName: r}, nil
	}

	// --- GitLab ---
	if strings.Contains(cleaned, "gitlab.com") {
		o, r, e := parseStandardGitURL(cleaned, "gitlab.com")
		if e != nil {
			return nil, e
		}
		return &remoteInfo{ProviderType: providerGitLab, Org: o, RepoName: r}, nil
	}

	return nil, fmt.Errorf("unsupported git remote URL: %s", rawURL)
}

// parseAzureDevOpsURL handles both HTTPS and SSH remote formats for
// Azure DevOps, including custom SSH aliases.
//
//	HTTPS:     https://dev.azure.com/{org}/{project}/_git/{repo}
//	SSH:       git@ssh.dev.azure.com:v3/{org}/{project}/{repo}
//	SSH alias: git@dev.azure.com-{alias}:v3/{org}/{project}/{repo}
func parseAzureDevOpsURL(url string) (*remoteInfo, error) {
	// SSH format — match any host alias that contains "dev.azure.com" and
	// uses the :v3/{org}/{project}/{repo} path layout.  This covers:
	//   git@ssh.dev.azure.com:v3/...
	//   git@dev.azure.com-myalias:v3/...
	if strings.HasPrefix(url, "git@") && strings.Contains(url, ":v3/") {
		colonIdx := strings.Index(url, ":v3/")
		pathPart := url[colonIdx+len(":v3/"):]
		parts := strings.Split(pathPart, "/")
		if len(parts) >= 3 { //nolint:mnd // org/project/repo
			return &remoteInfo{
				ProviderType: providerAzureDevOps,
				Org:          parts[0],
				Project:      parts[1],
				RepoName:     parts[2],
			}, nil
		}
		return nil, fmt.Errorf("invalid Azure DevOps SSH URL: %s", url)
	}

	// HTTPS format – look for the _git segment
	parts := strings.Split(url, "/")
	for i, p := range parts {
		if p == "_git" && i+1 < len(parts) && i >= 2 {
			return &remoteInfo{
				ProviderType: providerAzureDevOps,
				Org:          parts[i-2],
				Project:      parts[i-1],
				RepoName:     parts[i+1],
			}, nil
		}
	}

	return nil, fmt.Errorf("invalid Azure DevOps URL: %s", url)
}

// parseStandardGitURL handles the common HTTPS/SSH layout used by
// GitHub and GitLab:
//
//	HTTPS: https://{host}/{org}/{repo}[.git]
//	SSH:   git@{host}:{org}/{repo}[.git]
func parseStandardGitURL(url, hostname string) (string, string, error) {
	var pathPart string

	if strings.HasPrefix(url, "git@") {
		// git@{host}:{org}/{repo}
		parts := strings.SplitN(url, ":", 2) //nolint:mnd // host:path
		if len(parts) < 2 {                  //nolint:mnd // need both parts
			return "", "", fmt.Errorf("invalid SSH URL: %s", url)
		}
		pathPart = parts[1]
	} else {
		// https://{host}/{org}/{repo}
		idx := strings.Index(url, hostname)
		if idx < 0 {
			return "", "", fmt.Errorf("hostname %s not found in URL: %s", hostname, url)
		}
		pathPart = strings.TrimPrefix(url[idx+len(hostname):], "/")
	}

	segments := strings.Split(pathPart, "/")
	if len(segments) < 2 { //nolint:mnd // need org + repo
		return "", "", fmt.Errorf("cannot extract org/repo from URL: %s", url)
	}

	return segments[0], segments[1], nil
}

// ---------------------------------------------------------------------------
// Token resolution
// ---------------------------------------------------------------------------

// resolveTokenFromEnv reads the auth token from well-known environment
// variables for the given provider type.
func resolveTokenFromEnv(providerType string) string {
	switch providerType {
	case providerGitHub:
		if t := os.Getenv("GITHUB_TOKEN"); t != "" {
			return t
		}
		return os.Getenv("GH_TOKEN")
	case providerAzureDevOps:
		if t := os.Getenv("AZURE_DEVOPS_EXT_PAT"); t != "" {
			return t
		}
		return os.Getenv("SYSTEM_ACCESSTOKEN")
	case providerGitLab:
		if t := os.Getenv("GITLAB_TOKEN"); t != "" {
			return t
		}
		return os.Getenv("GL_TOKEN")
	default:
		return ""
	}
}

// tokenEnvHint returns a human-friendly hint about which environment
// variable to set for the given provider.
func tokenEnvHint(providerType string) string {
	switch providerType {
	case providerGitHub:
		return "GITHUB_TOKEN or GH_TOKEN"
	case providerAzureDevOps:
		return "AZURE_DEVOPS_EXT_PAT or SYSTEM_ACCESSTOKEN"
	case providerGitLab:
		return "GITLAB_TOKEN or GL_TOKEN"
	default:
		return "<unknown provider>"
	}
}

// ---------------------------------------------------------------------------
// Branch detection
// ---------------------------------------------------------------------------

// detectDefaultBranch returns the name of the currently checked-out branch.
// When running in local mode the user is expected to be on the default
// branch (e.g. main) before invoking the tool.
func detectDefaultBranch(ctx context.Context, repoDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoDir

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --abbrev-ref HEAD: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}
