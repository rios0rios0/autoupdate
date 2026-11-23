package commands

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	infraRepos "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
	dartRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/dart"
	goRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
	jsRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/javascript"
	pyRepo "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/python"
	"github.com/rios0rios0/autoupdate/internal/support"
	gitInfra "github.com/rios0rios0/gitforge/v4/pkg/git/infrastructure"
	globalEntities "github.com/rios0rios0/gitforge/v4/pkg/global/domain/entities"
	langEntities "github.com/rios0rios0/langforge/pkg/domain/entities"
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

	// branchName, when set, is the branch a dedicated upgrade is made on instead of the
	// one named after what it upgrades. Execute sets it when the checkout uses other
	// ecosystems too, so every upgrade lands on one branch and in one pull request.
	branchName string
}

// remoteInfo holds the parsed components of a Git remote URL.
type remoteInfo struct {
	ProviderType string
	ServiceType  globalEntities.ServiceType
	Org          string
	Project      string // Azure DevOps only
	RepoName     string
	RemoteURL    string
	// PushURLs are every URL a push to origin may dial (OriginRepository.PushURLs), which
	// is what decides whether it needs a token. They differ from RemoteURL when origin has
	// a pushurl or a second url, or when a global insteadOf rule rewrites what git prints
	// but not what go-git pushes to. Execute reads them just before the credentials.
	PushURLs []string
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
	// Migrations are the newer majors the Go upgrade could not reach, each with
	// what the run did about it. Empty for every other language.
	Migrations []goRepo.MigrationPrepared
}

// LocalCommand handles the standalone local mode: upgrades dependencies in
// a given directory, pushes a branch, and creates a PR.
type LocalCommand struct {
	providerRegistry *infraRepos.ProviderRegistry
	// updaterRegistry holds the updaters `autoupdate run` runs. Local mode runs every one
	// of them that applies to the checkout and that no dedicated upgrade already covers.
	updaterRegistry *infraRepos.UpdaterRegistry
	// credentials supplies the provider API's token when --token is empty. The
	// container binds gitforge's default chain: the forge's environment variables,
	// then its CLI.
	credentials repositories.CredentialResolver
	// gitCredentials supplies git's token when --token is empty: the forge's
	// environment variables alone, never a CLI.
	gitCredentials repositories.GitCredentialResolver
	// origins says what a push to origin dials and which server origin is on.
	origins repositories.OriginRepository
}

// NewLocalCommand creates a new LocalCommand with the given provider and updater
// registries, the resolvers it falls back to for the provider API's token and git's when
// no --token is given, and the reader of what a push to origin involves.
func NewLocalCommand(
	providerRegistry *infraRepos.ProviderRegistry,
	updaterRegistry *infraRepos.UpdaterRegistry,
	credentials repositories.CredentialResolver,
	gitCredentials repositories.GitCredentialResolver,
	origins repositories.OriginRepository,
) *LocalCommand {
	return &LocalCommand{
		providerRegistry: providerRegistry,
		updaterRegistry:  updaterRegistry,
		credentials:      credentials,
		gitCredentials:   gitCredentials,
		origins:          origins,
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

	repo := entities.Repository{
		ID:           remote.RepoName,
		Name:         remote.RepoName,
		Organization: remote.Org,
		Project:      remote.Project,
		RemoteURL:    remote.RemoteURL,
	}

	// The language langforge reads first decides the dedicated upgrade, when it has one;
	// every other ecosystem the checkout uses is upgraded by its updater's ApplyUpdates,
	// as `autoupdate run` upgrades it. Both are settled from disk before gh or az is asked
	// for a token, so a checkout nothing applies to stops here.
	primary, detectErr := detectPrimaryUpgrade(repoDir, opts.Settings)
	others := it.localUpdatersFor(ctx, repoDir, repo, opts, primary.dedicatedUpdater())
	if primary.handler == nil && len(others) == 0 {
		return primary.nothingToUpgrade(detectErr)
	}

	// Resolved before anything touches the checkout, so a run with no token to push
	// or to open its pull request with stops before it creates a branch.
	tokens, authErr := it.authenticateLocalRun(ctx, opts, repoDir, remote)
	if authErr != nil {
		return authErr
	}

	// Detect current branch (used as the PR target / default branch)
	defaultBranch, branchErr := detectDefaultBranch(ctx, repoDir)
	if branchErr != nil {
		return fmt.Errorf("failed to detect current branch: %w", branchErr)
	}
	logger.Infof("Default branch: %s", defaultBranch)

	// Set before the upgrade runs, because the Go updater prepares a migration
	// branch through this repository: the pull request it links to targets the
	// same branch this run's own pull request does.
	repo.DefaultBranch = defaultBranch

	if len(others) == 0 {
		return it.runDedicatedUpgrade(ctx, repoDir, remote, tokens, repo, opts, primary.handler)
	}

	if opts.DryRun {
		return it.rehearseEveryEcosystem(ctx, repoDir, remote, tokens, repo, opts, primary, others)
	}

	return it.upgradeEveryEcosystem(ctx, repoDir, remote, tokens, repo, opts, primary, others)
}

// runDedicatedUpgrade is the run of a checkout that only uses the primary language's
// ecosystem: the dedicated upgrade makes its branch, commit and push, and the pull request
// is opened for what it pushed.
func (it *LocalCommand) runDedicatedUpgrade(
	ctx context.Context,
	repoDir string,
	remote *remoteInfo,
	tokens localTokens,
	repo entities.Repository,
	opts LocalOptions,
	handler localUpgradeHandler,
) error {
	prInfo, upgradeErr := handler(ctx, repoDir, remote.ProviderType, tokens, repo, opts, it.providerRegistry)
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

	return it.openLocalPullRequest(ctx, remote.ProviderType, tokens, repo, prInfo)
}

// authenticateLocalRun reads what a push to origin dials and decides the run's tokens.
//
// What the push will dial decides whether it needs a token, so a checkout whose origin
// go-git cannot read is refused rather than guessed at from what git printed: the push
// reads it through go-git too, and would fail on it after the upgrade. A rehearsal pushes
// nothing and opens nothing, so a missing token does not stop it; it still says what
// would have stopped the real run.
func (it *LocalCommand) authenticateLocalRun(
	ctx context.Context, opts LocalOptions, repoDir string, remote *remoteInfo,
) (localTokens, error) {
	pushURLs, err := it.origins.PushURLs(repoDir)
	if err != nil {
		return localTokens{}, fmt.Errorf("failed to read the URLs a push to origin dials: %w", err)
	}
	remote.PushURLs = pushURLs

	tokens, err := it.resolveLocalTokens(ctx, opts, remote)
	if err != nil {
		if !opts.DryRun {
			return localTokens{}, err
		}
		logger.Warnf("[DRY RUN] A real run would stop here: %v", err)
	}

	return tokens, nil
}

// openLocalPullRequest opens the pull request for the pushed upgrade branch, with the title
// and description of the upgrade that pushed it.
func (it *LocalCommand) openLocalPullRequest(
	ctx context.Context, providerType string, tokens localTokens, repo entities.Repository, info *localPRInfo,
) error {
	title, description := generatePRContent(info)
	return it.openPullRequest(ctx, providerType, tokens, repo, info.BranchName, title, description)
}

// openPullRequest opens the pull request for a pushed branch. The API's token is asked for
// again now rather than reused from before the upgrade, which may have taken most of an
// Azure CLI token's hour.
func (it *LocalCommand) openPullRequest(
	ctx context.Context,
	providerType string,
	tokens localTokens,
	repo entities.Repository,
	branchName, title, description string,
) error {
	apiToken, err := tokens.API(ctx)
	if err != nil {
		return fmt.Errorf("pushed %s but could not open its pull request: %w", branchName, err)
	}

	return it.createLocalPRForProject(ctx, providerType, apiToken, repo, branchName, title, description)
}

// localUpgradeHandler runs the local upgrade for a specific language and returns PR info.
// It hands tokens.Git to git and tokens.API to the provider API, never the other way round.
type localUpgradeHandler func(
	ctx context.Context,
	repoDir, providerType string,
	tokens localTokens,
	repo entities.Repository,
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

	return updaterEnabled(settings, name)
}

// updaterEnabled reports whether the configuration leaves the named updater on. Settings
// that never mention it leave it enabled, the default everywhere else.
func updaterEnabled(settings *entities.Settings, name string) bool {
	if settings == nil {
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

// localUpgradeHandlerFor returns the local upgrade for a project type, or an error when
// local mode has none for it.
func localUpgradeHandlerFor(projType langEntities.Language) (localUpgradeHandler, error) {
	handler, ok := localUpgradeHandlers()[projType]
	if !ok || handler == nil {
		return nil, fmt.Errorf("unsupported project type: %s", projType)
	}
	return handler, nil
}

func runGoLocalUpgrade(
	ctx context.Context,
	repoDir, providerType string,
	tokens localTokens,
	repo entities.Repository,
	opts LocalOptions,
	registry *infraRepos.ProviderRegistry,
) (*localPRInfo, error) {
	result, err := goRepo.RunLocalUpgrade(ctx, repoDir, goRepo.LocalUpgradeOptions{
		DryRun:       opts.DryRun,
		Verbose:      opts.Verbose,
		AuthToken:    tokens.Git,
		ProviderName: providerType,
		PushAuth:     registry,

		AllowMajorUpdates: entities.MajorUpdatesAllowed(opts.Settings),
		BranchName:        opts.branchName,

		Repository: repo,
		MigrationProvider: func(ctx context.Context) repositories.ProviderRepository {
			return localMigrationProvider(ctx, registry, providerType, tokens, opts)
		},
		PrepareMajorMigrations: entities.PrepareMajorMigrationsEnabled(opts.Settings),
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
		Migrations:        result.Migrations,
	}, nil
}

// localMigrationProvider resolves the provider API a prepared migration branch
// would be pushed through, and answers nil when there is none to be had.
//
// The Go upgrade calls it once the upgrade is pushed and a newer major is waiting,
// and it asks for the API's token then, not before the upgrade: an Azure CLI token
// can expire while the upgrade runs.
//
// The upgrade does not depend on it: a provider that cannot be reached costs
// the notice its links, which is cheaper than failing a run that has already
// produced the upgrade it was asked for.
func localMigrationProvider(
	ctx context.Context,
	registry *infraRepos.ProviderRegistry,
	providerType string,
	tokens localTokens,
	opts LocalOptions,
) repositories.ProviderRepository {
	// Nothing would be prepared under either of these, so the client is not
	// built: a rehearsal opens no pull request, and disabled preparation
	// prepares no branch.
	if registry == nil || opts.DryRun || !entities.PrepareMajorMigrationsEnabled(opts.Settings) {
		return nil
	}

	token, err := tokens.API(ctx)
	if err != nil {
		logger.Warnf(
			"[golang] Could not authenticate to the %s API to prepare migrations: %v", providerType, err,
		)
		return nil
	}

	provider, err := registry.Get(providerType, token)
	if err != nil {
		logger.Warnf(
			"[golang] Could not reach the %s API to prepare migrations: %v", providerType, err,
		)
		return nil
	}

	return provider
}

func runPythonLocalUpgrade(
	ctx context.Context,
	repoDir, providerType string,
	tokens localTokens,
	_ entities.Repository,
	opts LocalOptions,
	registry *infraRepos.ProviderRegistry,
) (*localPRInfo, error) {
	result, err := pyRepo.RunLocalUpgrade(ctx, repoDir, pyRepo.LocalUpgradeOptions{
		DryRun:       opts.DryRun,
		Verbose:      opts.Verbose,
		AuthToken:    tokens.Git,
		ProviderName: providerType,
		PushAuth:     registry,

		AllowMajorUpdates: entities.MajorUpdatesAllowed(opts.Settings),
		BranchName:        opts.branchName,
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
	repoDir, providerType string,
	tokens localTokens,
	_ entities.Repository,
	opts LocalOptions,
	registry *infraRepos.ProviderRegistry,
) (*localPRInfo, error) {
	result, err := jsRepo.RunLocalUpgrade(ctx, repoDir, jsRepo.LocalUpgradeOptions{
		DryRun:       opts.DryRun,
		Verbose:      opts.Verbose,
		AuthToken:    tokens.Git,
		ProviderName: providerType,
		PushAuth:     registry,

		AllowMajorUpdates: entities.MajorUpdatesAllowed(opts.Settings),
		BranchName:        opts.branchName,
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
	repoDir, providerType string,
	tokens localTokens,
	_ entities.Repository,
	opts LocalOptions,
	registry *infraRepos.ProviderRegistry,
) (*localPRInfo, error) {
	result, err := dartRepo.RunLocalUpgrade(ctx, repoDir, dartRepo.LocalUpgradeOptions{
		DryRun:       opts.DryRun,
		Verbose:      opts.Verbose,
		AuthToken:    tokens.Git,
		ProviderName: providerType,
		PushAuth:     registry,

		AllowMajorUpdates: entities.MajorUpdatesAllowed(opts.Settings),
		BranchName:        opts.branchName,
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

		AllowMajorUpdates: entities.MajorUpdatesAllowed(opts.Settings),
	}, nil
}

// createLocalPRForProject creates a pull request using the provider API.
func (it *LocalCommand) createLocalPRForProject(
	ctx context.Context,
	providerType, token string,
	repo entities.Repository,
	branchName, prTitle, prDesc string,
) error {
	provider, err := it.providerRegistry.Get(providerType, token)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	targetBranch := repo.DefaultBranch
	if !strings.HasPrefix(targetBranch, "refs/heads/") {
		targetBranch = "refs/heads/" + targetBranch
	}

	pr, createErr := provider.CreatePullRequest(ctx, repo, entities.PullRequestInput{
		SourceBranch: "refs/heads/" + branchName,
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
			title := "chore(deps): updated Go module dependencies"
			if info.VersionUpdated {
				title = fmt.Sprintf(
					"chore(deps): updated Go version to `%s` and dependencies",
					info.LatestVersion,
				)
			}
			// The description is the one the batch flow renders, from the same
			// findings: the newer majors `go get -u` cannot reach, and the branch
			// the run prepared for each of them.
			desc := goRepo.GenerateGoPRDescription(
				info.LatestVersion, false, info.VersionUpdated, info.AllowMajorUpdates,
				info.Migrations,
			)
			return title, desc
		},
		langEntities.LanguagePython: func(info *localPRInfo) (string, string) {
			title := "chore(deps): updated Python dependencies"
			if info.VersionUpdated {
				title = fmt.Sprintf(
					"chore(deps): updated Python to `%s` and dependencies",
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
					"chore(deps): updated Node.js to `%s` and dependencies",
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
					"chore(deps): updated Flutter to `%s` and pub dependencies",
					info.LatestVersion,
				)
			}
			desc := dartRepo.GeneratePRDescription(
				info.LatestVersion, info.PackageManager, info.VersionUpdated,
				info.AllowMajorUpdates,
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
	output, err := support.GitCommand(ctx, repoDir, "remote", "get-url", "origin").Output()
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
		RemoteURL:    rawURL,
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
	output, err := support.GitCommand(ctx, repoDir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --abbrev-ref HEAD: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}
