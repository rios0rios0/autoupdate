package commands

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	infraRepos "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
	goRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
	jsRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/javascript"
	pyRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/python"
	"github.com/rios0rios0/autoupdate/internal/support"
	configHelpers "github.com/rios0rios0/gitforge/pkg/config/domain/helpers"
	gitInfra "github.com/rios0rios0/gitforge/pkg/git/infrastructure"
	globalEntities "github.com/rios0rios0/gitforge/pkg/global/domain/entities"
	langEntities "github.com/rios0rios0/langforge/pkg/domain/entities"
	langRegistry "github.com/rios0rios0/langforge/pkg/infrastructure/registry"
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
	// Settings is optional. When supplied, the global exclude_repos list
	// is honored in local mode; missing settings means only the per-repo
	// .autoupdate.yaml controls whether the update runs.
	Settings *entities.Settings
}

// remoteInfo holds the parsed components of a Git remote URL.
type remoteInfo struct {
	ProviderType string
	ServiceType  globalEntities.ServiceType
	Org          string
	Project      string // Azure DevOps only
	RepoName     string
}

// serviceTypeToProvider returns a map from gitforge ServiceType to the provider name strings used by autoupdate.
func serviceTypeToProvider() map[globalEntities.ServiceType]string {
	return map[globalEntities.ServiceType]string{
		globalEntities.UNKNOWN:     "",
		globalEntities.GITHUB:      providerGitHub,
		globalEntities.GITLAB:      providerGitLab,
		globalEntities.AZUREDEVOPS: providerAzureDevOps,
		globalEntities.BITBUCKET:   "",
		globalEntities.CODECOMMIT:  "",
		globalEntities.CODEBERG:    "",
	}
}

// localPRInfo holds the information needed to create a PR after a local upgrade.
type localPRInfo struct {
	BranchName     string
	LatestVersion  string
	VersionUpdated bool
	PackageManager string // JavaScript only
	ProjectType    langEntities.Language
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

	if skipped, skipErr := checkLocalRepoConfigSkip(repoDir); skipErr != nil {
		return skipErr
	} else if skipped {
		return nil
	}

	// Detect Git provider from remote URL — done early so the global
	// exclude_repos list can short-circuit before paying the cost of
	// language detection or any updater work.
	remote, parseErr := parseGitRemote(ctx, repoDir)
	if parseErr != nil {
		return fmt.Errorf("failed to detect git provider: %w", parseErr)
	}
	logger.Infof("Detected provider: %s, org: %s, repo: %s", remote.ProviderType, remote.Org, remote.RepoName)

	if isExcludedByGlobalList(opts.Settings, remote) {
		return nil
	}

	// Detect project type using langforge's registry
	langProvider, detectErr := langRegistry.NewDefaultRegistry().Detect(repoDir)
	if detectErr != nil {
		return detectErr
	}
	projType := langProvider.Language()
	logger.Infof("Detected project type: %s", projType)

	// Resolve auth token
	token := opts.Token
	if token == "" {
		token = configHelpers.ResolveTokenFromEnv(remote.ServiceType)
	}

	if !opts.DryRun && token == "" {
		return fmt.Errorf(
			"no auth token found for %s; set --token or the appropriate env var (%s)",
			remote.ProviderType, configHelpers.TokenEnvHint(remote.ServiceType),
		)
	}

	// Detect current branch (used as the PR target / default branch)
	defaultBranch, branchErr := detectDefaultBranch(ctx, repoDir)
	if branchErr != nil {
		return fmt.Errorf("failed to detect current branch: %w", branchErr)
	}
	logger.Infof("Default branch: %s", defaultBranch)

	// Run the appropriate upgrade
	prInfo, upgradeErr := runLocalUpgrade(ctx, repoDir, projType, remote.ProviderType, token, opts, it.providerRegistry)
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

// localUpgradeHandler runs the local upgrade for a specific language and returns PR info.
type localUpgradeHandler func(
	ctx context.Context,
	repoDir, providerType, token string,
	opts LocalOptions,
	registry *infraRepos.ProviderRegistry,
) (*localPRInfo, error)

// localUpgradeHandlers returns a map from langforge Language to local upgrade handler.
func localUpgradeHandlers() map[langEntities.Language]localUpgradeHandler {
	return map[langEntities.Language]localUpgradeHandler{
		langEntities.LanguageGo:         runGoLocalUpgrade,
		langEntities.LanguageNode:       runJSLocalUpgrade,
		langEntities.LanguagePython:     runPythonLocalUpgrade,
		langEntities.LanguageJava:       nil,
		langEntities.LanguageJavaGradle: nil,
		langEntities.LanguageJavaMaven:  nil,
		langEntities.LanguageCSharp:     nil,
		langEntities.LanguageRuby:       nil,
		langEntities.LanguageTerraform:  nil,
		langEntities.LanguageYAML:       nil,
		langEntities.LanguagePipeline:   nil,
		langEntities.LanguageDockerfile: nil,
		langEntities.LanguageUnknown:    nil,
	}
}

// runLocalUpgrade dispatches to the appropriate updater based on project type.
func runLocalUpgrade(
	ctx context.Context,
	repoDir string,
	projType langEntities.Language,
	providerType, token string,
	opts LocalOptions,
	registry *infraRepos.ProviderRegistry,
) (*localPRInfo, error) {
	handler, ok := localUpgradeHandlers()[projType]
	if !ok || handler == nil {
		return nil, fmt.Errorf("unsupported project type: %s", projType)
	}
	return handler(ctx, repoDir, providerType, token, opts, registry)
}

func runGoLocalUpgrade(
	ctx context.Context,
	repoDir, providerType, token string,
	opts LocalOptions,
	registry *infraRepos.ProviderRegistry,
) (*localPRInfo, error) {
	result, err := goRepo.RunLocalUpgrade(ctx, repoDir, goRepo.LocalUpgradeOptions{
		DryRun:       opts.DryRun,
		Verbose:      opts.Verbose,
		AuthToken:    token,
		ProviderName: providerType,
		PushAuth:     registry,
	})
	if err != nil {
		return nil, err
	}
	return &localPRInfo{
		BranchName:     result.BranchName,
		LatestVersion:  result.LatestVersion,
		VersionUpdated: result.GoVersionUpdated,
		ProjectType:    langEntities.LanguageGo,
		HasChanges:     result.HasChanges,
	}, nil
}

func runPythonLocalUpgrade(
	ctx context.Context,
	repoDir, providerType, token string,
	opts LocalOptions,
	registry *infraRepos.ProviderRegistry,
) (*localPRInfo, error) {
	result, err := pyRepo.RunLocalUpgrade(ctx, repoDir, pyRepo.LocalUpgradeOptions{
		DryRun:       opts.DryRun,
		Verbose:      opts.Verbose,
		AuthToken:    token,
		ProviderName: providerType,
		PushAuth:     registry,
	})
	if err != nil {
		return nil, err
	}
	return &localPRInfo{
		BranchName:     result.BranchName,
		LatestVersion:  result.LatestVersion,
		VersionUpdated: result.PythonVersionUpdated,
		ProjectType:    langEntities.LanguagePython,
		HasChanges:     result.HasChanges,
	}, nil
}

func runJSLocalUpgrade(
	ctx context.Context,
	repoDir, providerType, token string,
	opts LocalOptions,
	registry *infraRepos.ProviderRegistry,
) (*localPRInfo, error) {
	result, err := jsRepo.RunLocalUpgrade(ctx, repoDir, jsRepo.LocalUpgradeOptions{
		DryRun:       opts.DryRun,
		Verbose:      opts.Verbose,
		AuthToken:    token,
		ProviderName: providerType,
		PushAuth:     registry,
	})
	if err != nil {
		return nil, err
	}
	return &localPRInfo{
		BranchName:     result.BranchName,
		LatestVersion:  result.LatestVersion,
		VersionUpdated: result.NodeVersionUpdated,
		PackageManager: result.PackageManager,
		ProjectType:    langEntities.LanguageNode,
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

// prContentGenerator produces PR title and description from localPRInfo.
type prContentGenerator func(info *localPRInfo) (string, string)

// prContentGenerators returns a map from langforge Language to PR content generator.
func prContentGenerators() map[langEntities.Language]prContentGenerator {
	return map[langEntities.Language]prContentGenerator{
		langEntities.LanguageGo: func(info *localPRInfo) (string, string) {
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
		},
		langEntities.LanguagePython: func(info *localPRInfo) (string, string) {
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
		},
		langEntities.LanguageNode: func(info *localPRInfo) (string, string) {
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
		},
		langEntities.LanguageJava:       nil,
		langEntities.LanguageJavaGradle: nil,
		langEntities.LanguageJavaMaven:  nil,
		langEntities.LanguageCSharp:     nil,
		langEntities.LanguageRuby:       nil,
		langEntities.LanguageTerraform:  nil,
		langEntities.LanguageYAML:       nil,
		langEntities.LanguagePipeline:   nil,
		langEntities.LanguageDockerfile: nil,
		langEntities.LanguageUnknown:    nil,
	}
}

// generatePRContent returns the title and description for a PR.
func generatePRContent(info *localPRInfo) (string, string) {
	generator, ok := prContentGenerators()[info.ProjectType]
	if !ok || generator == nil {
		return "chore(deps): updated dependencies", "Automated dependency update."
	}
	return generator(info)
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
// Delegates to gitforge's ParseRemoteURL and converts the result to autoupdate's remoteInfo.
func parseRemoteURL(rawURL string) (*remoteInfo, error) {
	parsed, err := gitInfra.ParseRemoteURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("unsupported git remote URL: %w", err)
	}

	providerName, ok := serviceTypeToProvider()[parsed.ServiceType]
	if !ok {
		return nil, fmt.Errorf("unsupported provider type for URL: %s", rawURL)
	}

	return &remoteInfo{
		ProviderType: providerName,
		ServiceType:  parsed.ServiceType,
		Org:          parsed.Organization,
		Project:      parsed.Project,
		RepoName:     parsed.RepoName,
	}, nil
}

// checkLocalRepoConfigSkip reads the per-repository .autoupdate.yaml from
// disk and reports whether the user requested that this project be
// skipped. A missing file is not an error; a malformed file is, because
// the user explicitly asked autoupdate to read it and we should surface
// problems instead of silently running anyway.
func checkLocalRepoConfigSkip(repoDir string) (bool, error) {
	cfg, err := support.LoadLocalRepoConfig(repoDir)
	if err != nil {
		return false, err
	}
	if !cfg.IsSkipped() {
		return false, nil
	}
	if cfg.Reason != "" {
		logger.Infof("Skipping %s: %s requested skip (%s)",
			repoDir, entities.RepoConfigFile, cfg.Reason)
	} else {
		logger.Infof("Skipping %s: %s requested skip", repoDir, entities.RepoConfigFile)
	}
	return true, nil
}

// isExcludedByGlobalList reports whether the parsed remote matches a
// pattern in the user's global exclude_repos list. The check is a no-op
// when no Settings were loaded (i.e. the user invoked local mode without
// a config file), so per-repo .autoupdate.yaml remains the only source
// of truth in that case.
func isExcludedByGlobalList(settings *entities.Settings, remote *remoteInfo) bool {
	if settings == nil || len(settings.ExcludeRepos) == 0 {
		return false
	}
	repo := entities.Repository{
		Organization: remote.Org,
		Project:      remote.Project,
		Name:         remote.RepoName,
	}
	excluded, pattern := settings.IsRepoExcluded(repo)
	if !excluded {
		return false
	}
	logger.Infof("Skipping %s/%s: matched exclude_repos pattern %q",
		remote.Org, remote.RepoName, pattern)
	return true
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
