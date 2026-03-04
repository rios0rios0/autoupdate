package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	infraRepos "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
	goRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
	jsRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/javascript"
	pyRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/python"
)

const (
	providerGitHub      = "github"
	providerAzureDevOps = "azuredevops"
	providerGitLab      = "gitlab"
)

// Local is the interface for the local command (standalone mode).
type Local interface {
	Execute(ctx context.Context, opts LocalOptions) error
}

// LocalOptions holds runtime options for the local mode.
type LocalOptions struct {
	RepoDir string
	DryRun  bool
	Verbose bool
	Token   string
}

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

// LocalCommand handles the standalone local mode: upgrades dependencies in
// a given directory, pushes a branch, and creates a PR.
type LocalCommand struct {
	providerRegistry *infraRepos.ProviderRegistry
}

// NewLocalCommand creates a new LocalCommand with the given provider registry.
func NewLocalCommand(providerRegistry *infraRepos.ProviderRegistry) *LocalCommand {
	return &LocalCommand{
		providerRegistry: providerRegistry,
	}
}

// Execute is the entry point for the standalone local mode.
func (it *LocalCommand) Execute(ctx context.Context, opts LocalOptions) error {
	repoDir, err := filepath.Abs(opts.RepoDir)
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
	token := opts.Token
	if token == "" {
		token = resolveTokenFromEnv(remote.ProviderType)
	}

	if !opts.DryRun && token == "" {
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
	prInfo, upgradeErr := runLocalUpgrade(ctx, repoDir, projType, remote.ProviderType, token, opts)
	if upgradeErr != nil {
		return upgradeErr
	}

	if opts.DryRun {
		return nil
	}

	if !prInfo.HasChanges {
		logger.Info("No dependency changes detected, nothing to do.")
		return nil
	}

	// Build repository struct for the provider API.
	repo := entities.Repository{
		ID:            remote.RepoName,
		Name:          remote.RepoName,
		Organization:  remote.Org,
		Project:       remote.Project,
		DefaultBranch: defaultBranch,
	}

	return it.createLocalPRForProject(ctx, remote.ProviderType, token, repo, prInfo)
}

// detectProjectType determines what kind of project is in the given directory.
func detectProjectType(repoDir string) (projectType, error) {
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
	opts LocalOptions,
) (*localPRInfo, error) {
	switch projType {
	case projectGo:
		return runGoLocalUpgrade(ctx, repoDir, providerType, token, opts)
	case projectPython:
		return runPythonLocalUpgrade(ctx, repoDir, providerType, token, opts)
	case projectJavaScript:
		return runJSLocalUpgrade(ctx, repoDir, providerType, token, opts)
	default:
		return nil, fmt.Errorf("unsupported project type: %s", projType)
	}
}

func runGoLocalUpgrade(
	ctx context.Context,
	repoDir, providerType, token string,
	opts LocalOptions,
) (*localPRInfo, error) {
	result, err := goRepo.RunLocalUpgrade(ctx, repoDir, goRepo.LocalUpgradeOptions{
		DryRun:       opts.DryRun,
		Verbose:      opts.Verbose,
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
	opts LocalOptions,
) (*localPRInfo, error) {
	result, err := pyRepo.RunLocalUpgrade(ctx, repoDir, pyRepo.LocalUpgradeOptions{
		DryRun:       opts.DryRun,
		Verbose:      opts.Verbose,
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
	opts LocalOptions,
) (*localPRInfo, error) {
	result, err := jsRepo.RunLocalUpgrade(ctx, repoDir, jsRepo.LocalUpgradeOptions{
		DryRun:       opts.DryRun,
		Verbose:      opts.Verbose,
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

// createLocalPRForProject creates a pull request using the provider API.
func (it *LocalCommand) createLocalPRForProject(
	ctx context.Context,
	providerType, token string,
	repo entities.Repository,
	info *localPRInfo,
) error {
	provider, err := it.providerRegistry.Get(providerType, token)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	prTitle, prDesc := generatePRContent(info)

	targetBranch := repo.DefaultBranch
	if !strings.HasPrefix(targetBranch, "refs/heads/") {
		targetBranch = "refs/heads/" + targetBranch
	}

	pr, createErr := provider.CreatePullRequest(ctx, repo, entities.PullRequestInput{
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

// generatePRContent returns the title and description for a PR.
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
		desc := goRepo.GenerateGoPRDescription(
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
		desc := pyRepo.GeneratePRDescription(
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
		desc := jsRepo.GeneratePRDescription(
			info.LatestVersion, info.PackageManager, info.VersionUpdated,
		)
		return title, desc

	default:
		return "chore(deps): updated dependencies", "Automated dependency update."
	}
}

// parseGitRemote runs `git remote get-url origin` and parses the result.
func parseGitRemote(ctx context.Context, repoDir string) (*remoteInfo, error) {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = repoDir

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git remote get-url origin: %w", err)
	}

	return parseRemoteURL(strings.TrimSpace(string(output)))
}

// parseRemoteURL extracts provider, org, project, and repo name from a Git remote URL.
func parseRemoteURL(rawURL string) (*remoteInfo, error) {
	cleaned := strings.TrimSuffix(rawURL, ".git")

	if strings.Contains(cleaned, "dev.azure.com") || strings.Contains(cleaned, "ssh.dev.azure.com") {
		return parseAzureDevOpsURL(cleaned)
	}

	if strings.Contains(cleaned, "github.com") {
		o, r, e := parseStandardGitURL(cleaned, "github.com")
		if e != nil {
			return nil, e
		}
		return &remoteInfo{ProviderType: providerGitHub, Org: o, RepoName: r}, nil
	}

	if strings.Contains(cleaned, "gitlab.com") {
		o, r, e := parseStandardGitURL(cleaned, "gitlab.com")
		if e != nil {
			return nil, e
		}
		return &remoteInfo{ProviderType: providerGitLab, Org: o, RepoName: r}, nil
	}

	return nil, fmt.Errorf("unsupported git remote URL: %s", rawURL)
}

func parseAzureDevOpsURL(url string) (*remoteInfo, error) {
	if strings.HasPrefix(url, "git@") && strings.Contains(url, ":v3/") {
		_, after, _ := strings.Cut(url, ":v3/")
		pathPart := after
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

func parseStandardGitURL(url, hostname string) (string, string, error) {
	var pathPart string

	if strings.HasPrefix(url, "git@") {
		parts := strings.SplitN(url, ":", 2) //nolint:mnd // host:path
		if len(parts) < 2 {                  //nolint:mnd // need both parts
			return "", "", fmt.Errorf("invalid SSH URL: %s", url)
		}
		pathPart = parts[1]
	} else {
		_, after, ok := strings.Cut(url, hostname)
		if !ok {
			return "", "", fmt.Errorf("hostname %s not found in URL: %s", hostname, url)
		}
		pathPart = strings.TrimPrefix(after, "/")
	}

	segments := strings.Split(pathPart, "/")
	if len(segments) < 2 { //nolint:mnd // need org + repo
		return "", "", fmt.Errorf("cannot extract org/repo from URL: %s", url)
	}

	return segments[0], segments[1], nil
}

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

func detectDefaultBranch(ctx context.Context, repoDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoDir

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --abbrev-ref HEAD: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}
