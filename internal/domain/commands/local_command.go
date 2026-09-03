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
	dartRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/dart"
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
	PackageManager string // JavaScript package manager, or Python dependency manager
	ProjectType    langEntities.Language
	HasChanges     bool
	// AllowMajorUpdates is carried here rather than read at render time because
	// prContentGenerators is a static map with no access to the settings.
	AllowMajorUpdates bool
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

	// The repository's own configuration is the last layer, and it is read before anything
	// else so that a project that opted out pays for nothing.
	settings, skipped, skipErr := resolveLocalSettings(repoDir, opts.Settings)
	if skipErr != nil {
		return skipErr
	}
	if skipped {
		return nil
	}
	opts.Settings = settings

	// Detect Git provider from remote URL — done early so the global
	// exclude_repos list can short-circuit before paying the cost of
	// language detection or any updater work.
	remote, parseErr := parseGitRemote(ctx, repoDir)
	if parseErr != nil {
		return fmt.Errorf("failed to detect git provider: %w", parseErr)
	}
	logger.Infof("Detected provider: %s, org: %s, repo: %s", remote.ProviderType, remote.Org, remote.RepoName)

	if excluded, rule := entities.ExcludesSelf(opts.Settings, entities.Repository{
		Organization: remote.Org,
		Project:      remote.Project,
		Name:         remote.RepoName,
	}); excluded {
		logger.Infof("Skipping %s/%s: matched %s", remote.Org, remote.RepoName, rule)
		return nil
	}

	// Detect project type using langforge's registry
	langProvider, detectErr := langRegistry.NewDefaultRegistry().Detect(repoDir)
	if detectErr != nil {
		return detectErr
	}
	projType := langProvider.Language()
	logger.Infof("Detected project type: %s", projType)

	if !updaterEnabledForLanguage(opts.Settings, projType) {
		logger.Infof("Skipping %s: its updater is disabled in the configuration", projType)
		return nil
	}

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

// updaterNameForLanguage maps a detected langforge Language to the updater name the
// configuration keys on, so `updaters.<name>.enabled: false` means the same thing in
// `autoupdate .` as it does in `autoupdate run`.
//
// Without it the local path dispatched on the language alone and never consulted
// `settings.Updaters`, so an updater disabled in configuration still ran here.
func updaterNameForLanguage() map[langEntities.Language]string {
	// The three Java flavours share one updater, which is why the name is a constant here
	// rather than repeated: they are one entry in the configuration.
	const updaterJava = "java"

	return map[langEntities.Language]string{
		langEntities.LanguageGo:         "golang",
		langEntities.LanguageNode:       "javascript",
		langEntities.LanguagePython:     "python",
		langEntities.LanguageDart:       "dart",
		langEntities.LanguageJava:       updaterJava,
		langEntities.LanguageJavaGradle: updaterJava,
		langEntities.LanguageJavaMaven:  updaterJava,
		langEntities.LanguageCSharp:     "csharp",
		langEntities.LanguageRuby:       "ruby",
		langEntities.LanguageTerraform:  "terraform",
		langEntities.LanguagePipeline:   "pipeline",
		langEntities.LanguageDockerfile: "dockerfile",
		// A YAML or unidentified project has no updater of its own, so there is no
		// configuration entry that could turn one off.
		langEntities.LanguageYAML:    "",
		langEntities.LanguageUnknown: "",
	}
}

// updaterEnabledForLanguage reports whether the configuration leaves this language's
// updater on. A language with no updater name, or settings that never mention it, is left
// enabled -- the default everywhere else.
func updaterEnabledForLanguage(settings *entities.Settings, projType langEntities.Language) bool {
	if settings == nil {
		return true
	}

	name, known := updaterNameForLanguage()[projType]
	if !known || name == "" {
		return true
	}

	config, configured := settings.Updaters[name]
	if !configured {
		return true
	}

	return config.IsEnabled()
}

// localUpgradeHandlers returns a map from langforge Language to local upgrade handler.
func localUpgradeHandlers() map[langEntities.Language]localUpgradeHandler {
	return map[langEntities.Language]localUpgradeHandler{
		langEntities.LanguageGo:         runGoLocalUpgrade,
		langEntities.LanguageNode:       runJSLocalUpgrade,
		langEntities.LanguagePython:     runPythonLocalUpgrade,
		langEntities.LanguageDart:       runDartLocalUpgrade,
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

		AllowMajorUpdates: entities.MajorUpdatesAllowed(opts.Settings),
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

		AllowMajorUpdates: entities.MajorUpdatesAllowed(opts.Settings),
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

		AllowMajorUpdates: entities.MajorUpdatesAllowed(opts.Settings),
	})
	if err != nil {
		return nil, err
	}
	return &localPRInfo{
		BranchName:     result.BranchName,
		LatestVersion:  result.LatestVersion,
		VersionUpdated: result.PythonVersionUpdated,
		PackageManager: result.Toolchain,
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

		AllowMajorUpdates: entities.MajorUpdatesAllowed(opts.Settings),
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

func runDartLocalUpgrade(
	ctx context.Context,
	repoDir, providerType, token string,
	opts LocalOptions,
	registry *infraRepos.ProviderRegistry,
) (*localPRInfo, error) {
	result, err := dartRepo.RunLocalUpgrade(ctx, repoDir, dartRepo.LocalUpgradeOptions{
		DryRun:       opts.DryRun,
		Verbose:      opts.Verbose,
		AuthToken:    token,
		ProviderName: providerType,
		PushAuth:     registry,

		AllowMajorUpdates: entities.MajorUpdatesAllowed(opts.Settings),
	})
	if err != nil {
		return nil, err
	}
	return &localPRInfo{
		BranchName:     result.BranchName,
		LatestVersion:  result.LatestVersion,
		VersionUpdated: result.SDKUpdated,
		// The toolchain rides in PackageManager: it is the same kind of fact
		// (which tool actually ran) and the PR description needs it.
		PackageManager: result.Toolchain,
		ProjectType:    langEntities.LanguageDart,
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
				info.LatestVersion, false, info.VersionUpdated, info.AllowMajorUpdates,
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
				info.LatestVersion, info.PackageManager, info.VersionUpdated,
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
		langEntities.LanguageDart: func(info *localPRInfo) (string, string) {
			title := "chore(deps): updated Dart pub dependencies"
			if info.VersionUpdated {
				title = fmt.Sprintf(
					"chore(deps): upgraded Flutter to `%s` and updated all pub dependencies",
					info.LatestVersion,
				)
			}
			desc := dartRepo.GeneratePRDescription(
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

// resolveLocalSettings reads the per-repository `.autoupdate.yaml` from disk once and
// answers both questions it can settle: whether the project opted out, and what settings
// this repository is updated under.
//
// A missing file is not an error. A malformed one is, because the user explicitly asked
// autoupdate to read it, and running anyway on settings that silently ignored it would be
// worse than stopping.
func resolveLocalSettings(
	repoDir string, base *entities.Settings,
) (*entities.Settings, bool, error) {
	config, err := support.LoadLocalRepoConfig(repoDir)
	if err != nil {
		return nil, false, err
	}

	if config.IsSkipped() {
		if config.Reason != "" {
			logger.Infof("Skipping %s: %s requested skip (%s)",
				repoDir, entities.RepoConfigFile, config.Reason)
		} else {
			logger.Infof("Skipping %s: %s requested skip", repoDir, entities.RepoConfigFile)
		}
		return nil, true, nil
	}

	merged, err := entities.ApplyRepoOverlay(base, config)
	if err != nil {
		return nil, false, err
	}

	return merged, false, nil
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
