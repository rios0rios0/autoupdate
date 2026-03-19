package commands

import (
	"context"
	"errors"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	infraRepos "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlocal"
	gitops "github.com/rios0rios0/gitforge/pkg/git/infrastructure"
)

// Run is the interface for the run command (batch mode).
type Run interface {
	Execute(ctx context.Context, settings *entities.Settings, opts RunOptions) error
}

// RunOptions holds runtime options for a single run.
type RunOptions struct {
	DryRun       bool
	Verbose      bool
	ProviderName string // If set, only process this provider (CLI override)
	OrgOverride  string // If set, only process this org (CLI override)
	UpdaterName  string // If set, only run this updater (CLI override)
}

// RunCommand orchestrates the full dependency update flow:
// discover repositories -> detect ecosystems -> create update PRs.
type RunCommand struct {
	providerRegistry *infraRepos.ProviderRegistry
	updaterRegistry  *infraRepos.UpdaterRegistry
}

// NewRunCommand creates a new RunCommand with the given registries.
func NewRunCommand(
	providerRegistry *infraRepos.ProviderRegistry,
	updaterRegistry *infraRepos.UpdaterRegistry,
) *RunCommand {
	return &RunCommand{
		providerRegistry: providerRegistry,
		updaterRegistry:  updaterRegistry,
	}
}

// Execute runs the full update cycle using the provided configuration.
func (it *RunCommand) Execute(
	ctx context.Context,
	settings *entities.Settings,
	runOpts RunOptions,
) error {
	if runOpts.Verbose {
		logger.SetLevel(logger.DebugLevel)
	}

	totalPRs := 0
	totalRepos := 0
	totalErrors := 0

	for _, provCfg := range settings.Providers {
		if runOpts.ProviderName != "" && provCfg.Type != runOpts.ProviderName {
			continue
		}

		prs, repos, errs := it.processProvider(ctx, provCfg, settings, runOpts)
		totalPRs += prs
		totalRepos += repos
		totalErrors += errs
	}

	logger.Infof(
		"Run complete: %d repos processed, %d PRs created, %d errors",
		totalRepos, totalPRs, totalErrors,
	)
	return nil
}

// processProvider initializes a single provider and processes all its organizations.
func (it *RunCommand) processProvider(
	ctx context.Context,
	provCfg entities.ProviderConfig,
	settings *entities.Settings,
	runOpts RunOptions,
) (int, int, int) {
	provider, err := it.providerRegistry.Get(provCfg.Type, provCfg.Token)
	if err != nil {
		logger.Errorf("Failed to initialize provider %q: %v", provCfg.Type, err)
		return 0, 0, 1
	}

	logger.Infof("Processing provider: %s", provider.Name())

	totalPRs, totalRepos, totalErrors := 0, 0, 0
	for _, org := range provCfg.Organizations {
		if runOpts.OrgOverride != "" && org != runOpts.OrgOverride {
			continue
		}

		prs, repos, errs := it.processOrganization(ctx, provider, org, settings, runOpts)
		totalPRs += prs
		totalRepos += repos
		totalErrors += errs
	}

	return totalPRs, totalRepos, totalErrors
}

// processOrganization discovers repositories in an organization and processes each one.
func (it *RunCommand) processOrganization(
	ctx context.Context,
	provider repositories.ProviderRepository,
	org string,
	settings *entities.Settings,
	runOpts RunOptions,
) (int, int, int) {
	logger.Infof("Discovering repositories in %q...", org)

	repos, discoverErr := provider.DiscoverRepositories(ctx, org)
	if discoverErr != nil {
		logger.Errorf("Failed to discover repos in %q: %v", org, discoverErr)
		return 0, 0, 1
	}

	repos = filterRepositories(repos, settings)
	logger.Infof("Found %d repositories in %q", len(repos), org)

	totalPRs, totalRepos, totalErrors := 0, 0, 0
	for _, repo := range repos {
		totalRepos++
		prs, errs := it.processRepository(ctx, provider, repo, settings, runOpts)
		totalPRs += len(prs)
		totalErrors += errs
	}

	return totalPRs, totalRepos, totalErrors
}

// filterRepositories removes repositories that match the exclusion criteria
// defined in the settings (e.g. forks, archived repos).
func filterRepositories(
	repos []entities.Repository,
	settings *entities.Settings,
) []entities.Repository {
	if !settings.ExcludeForks && !settings.ExcludeArchived {
		return repos
	}

	filtered := make([]entities.Repository, 0, len(repos))
	for _, repo := range repos {
		if settings.ExcludeForks && repo.IsFork {
			logger.Debugf("Skipping fork: %s/%s", repo.Organization, repo.Name)
			continue
		}
		if settings.ExcludeArchived && repo.IsArchived {
			logger.Debugf("Skipping archived repo: %s/%s", repo.Organization, repo.Name)
			continue
		}
		filtered = append(filtered, repo)
	}
	return filtered
}

// applicableUpdater holds an updater and its resolved options.
type applicableUpdater struct {
	updater repositories.UpdaterRepository
	opts    entities.UpdateOptions
}

// processRepository runs all applicable updaters on a single repository.
// Updaters that implement LocalUpdater get the clone-based pipeline (clone once,
// branch per updater, signed commit, transport-detected push).
// Legacy updaters fall back to CreateUpdatePRs.
func (it *RunCommand) processRepository(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	settings *entities.Settings,
	runOpts RunOptions,
) ([]entities.PullRequest, int) {
	localUpdaters, legacyUpdaters := it.collectApplicableUpdaters(ctx, provider, repo, settings, runOpts)

	var allPRs []entities.PullRequest
	errorCount := 0

	if len(localUpdaters) > 0 {
		prs, errs := it.processLocalUpdaters(ctx, provider, repo, settings, localUpdaters)
		allPRs = append(allPRs, prs...)
		errorCount += errs
	}

	for _, au := range legacyUpdaters {
		prs, err := au.updater.CreateUpdatePRs(ctx, provider, repo, au.opts)
		if err != nil {
			logger.Errorf(
				"[%s] Failed to update %s/%s: %v",
				au.updater.Name(), repo.Organization, repo.Name, err,
			)
			errorCount++
			continue
		}

		for _, pr := range prs {
			logger.Infof("  Created PR #%d: %s (%s)", pr.ID, pr.Title, pr.URL)
		}
		allPRs = append(allPRs, prs...)
	}

	return allPRs, errorCount
}

// collectApplicableUpdaters partitions detected updaters into local and legacy groups.
func (it *RunCommand) collectApplicableUpdaters(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	settings *entities.Settings,
	runOpts RunOptions,
) ([]applicableUpdater, []applicableUpdater) {
	var local, legacy []applicableUpdater
	for _, u := range it.updaterRegistry.All() {
		if runOpts.UpdaterName != "" && u.Name() != runOpts.UpdaterName {
			continue
		}

		if updaterCfg, ok := settings.Updaters[u.Name()]; ok && !updaterCfg.Enabled {
			continue
		}

		if !u.Detect(ctx, provider, repo) {
			continue
		}

		logger.Infof("[%s] Detected in %s/%s", u.Name(), repo.Organization, repo.Name)

		opts := entities.UpdateOptions{
			DryRun:  runOpts.DryRun,
			Verbose: runOpts.Verbose,
		}
		if updaterCfg, ok := settings.Updaters[u.Name()]; ok {
			opts.AutoComplete = updaterCfg.AutoComplete
			if updaterCfg.TargetBranch != "" {
				opts.TargetBranch = updaterCfg.TargetBranch
			}
		}

		au := applicableUpdater{updater: u, opts: opts}
		if _, ok := u.(repositories.LocalUpdater); ok {
			local = append(local, au)
		} else {
			legacy = append(legacy, au)
		}
	}

	return local, legacy
}

// processLocalUpdaters clones the repository once and runs all local updaters
// against the clone, handling branch creation, signed commits, push, and PR
// creation centrally.
func (it *RunCommand) processLocalUpdaters(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	settings *entities.Settings,
	updaters []applicableUpdater,
) ([]entities.PullRequest, int) {
	cloneURL := provider.CloneURL(repo)
	defaultBranch := strings.TrimPrefix(repo.DefaultBranch, "refs/heads/")

	// Resolve service type and collect auth methods for clone and push
	serviceType := gitlocal.ResolveServiceTypeFromURL(it.providerRegistry, cloneURL)
	authMethods := gitlocal.CollectBatchAuthMethods(
		it.providerRegistry, serviceType, provider.AuthToken(), settings,
	)

	gitOps := gitops.NewGitOperations(it.providerRegistry)
	batchCtx, err := gitlocal.CloneRepository(gitOps, cloneURL, defaultBranch, authMethods, it.providerRegistry)
	if err != nil {
		logger.Errorf(
			"Failed to clone %s/%s: %v", repo.Organization, repo.Name, err,
		)
		return nil, len(updaters)
	}
	defer batchCtx.Close()

	var allPRs []entities.PullRequest
	errorCount := 0

	for _, au := range updaters {
		lu := au.updater.(repositories.LocalUpdater) //nolint:errcheck,forcetypeassert // checked at partition time
		prs, errs := it.runLocalUpdater(ctx, batchCtx, lu, au, provider, repo, settings, authMethods)
		allPRs = append(allPRs, prs...)
		errorCount += errs
	}

	return allPRs, errorCount
}

// resetWorktree resets the batch context to the default branch, logging any error.
func resetWorktree(batchCtx *gitlocal.BatchGitContext, name string) {
	if resetErr := batchCtx.ResetToDefault(); resetErr != nil {
		logger.Warnf("[%s] Failed to reset worktree to default branch: %v", name, resetErr)
	}
}

// runLocalUpdater executes a single local updater against the cloned repo.
func (it *RunCommand) runLocalUpdater(
	ctx context.Context,
	batchCtx *gitlocal.BatchGitContext,
	lu repositories.LocalUpdater,
	au applicableUpdater,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	settings *entities.Settings,
	authMethods []transport.AuthMethod,
) ([]entities.PullRequest, int) {
	name := au.updater.Name()

	if au.opts.DryRun {
		logger.Infof("[%s] [DRY RUN] Would apply updates to %s/%s via clone-based pipeline",
			name, repo.Organization, repo.Name)
		return nil, 0
	}

	result, err := lu.ApplyUpdates(ctx, batchCtx.RepoDir(), provider, repo, au.opts)
	if err != nil {
		if errors.Is(err, repositories.ErrNoUpdatesNeeded) {
			logger.Infof("[%s] %s/%s: already up to date", name, repo.Organization, repo.Name)
			return nil, 0
		}
		logger.Errorf("[%s] Failed to apply updates to %s/%s: %v",
			name, repo.Organization, repo.Name, err)
		resetWorktree(batchCtx, name)
		return nil, 1
	}

	exists, prCheckErr := provider.PullRequestExists(ctx, repo, result.BranchName)
	if prCheckErr != nil {
		logger.Warnf("[%s] Failed to check existing PRs: %v", name, prCheckErr)
	}
	if exists {
		logger.Infof("[%s] PR already exists for branch %q, skipping", name, result.BranchName)
		resetWorktree(batchCtx, name)
		return nil, 0
	}

	// CreateBranchFromDefault uses a force-checkout (go-git) which discards
	// uncommitted working-tree changes.  The upgrade script already modified
	// go.mod/go.sum on the default branch, so we must stash those changes
	// before the branch switch and pop them on the new branch.
	logger.Infof("[%s] Stashing upgrade changes before branch switch to %s", name, result.BranchName)
	if stashErr := batchCtx.StashChanges(); stashErr != nil {
		logger.Errorf("[%s] Failed to stash changes before branch switch: %v", name, stashErr)
		resetWorktree(batchCtx, name)
		return nil, 1
	}

	if branchErr := batchCtx.CreateBranchFromDefault(result.BranchName); branchErr != nil {
		logger.Errorf("[%s] Failed to create branch %s: %v", name, result.BranchName, branchErr)
		resetWorktree(batchCtx, name)
		return nil, 1
	}

	logger.Infof("[%s] Restoring upgrade changes on branch %s", name, result.BranchName)
	if popErr := batchCtx.PopStash(); popErr != nil {
		logger.Errorf("[%s] Failed to pop stash after branch switch: %v", name, popErr)
		resetWorktree(batchCtx, name)
		return nil, 1
	}

	pushed, pushErr := batchCtx.CommitSignedAndPush(result.BranchName, result.CommitMessage, settings, authMethods)
	if pushErr != nil {
		logger.Errorf("[%s] Failed to commit/push for %s/%s: %v",
			name, repo.Organization, repo.Name, pushErr)
		resetWorktree(batchCtx, name)
		return nil, 1
	}

	if !pushed {
		logger.Infof("[%s] %s/%s: no changes after apply", name, repo.Organization, repo.Name)
		resetWorktree(batchCtx, name)
		return nil, 0
	}

	return it.createLocalPR(ctx, batchCtx, au, provider, repo, result, name)
}

// createLocalPR creates a pull request for changes pushed by a local updater.
func (it *RunCommand) createLocalPR(
	ctx context.Context,
	batchCtx *gitlocal.BatchGitContext,
	au applicableUpdater,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	result *repositories.LocalUpdateResult,
	name string,
) ([]entities.PullRequest, int) {
	targetBranch := repo.DefaultBranch
	if au.opts.TargetBranch != "" {
		targetBranch = "refs/heads/" + au.opts.TargetBranch
	}

	pr, createErr := provider.CreatePullRequest(ctx, repo, entities.PullRequestInput{
		SourceBranch: "refs/heads/" + result.BranchName,
		TargetBranch: targetBranch,
		Title:        result.PRTitle,
		Description:  result.PRDescription,
		AutoComplete: au.opts.AutoComplete,
	})
	if createErr != nil {
		logger.Errorf("[%s] Failed to create PR for %s/%s: %v",
			name, repo.Organization, repo.Name, createErr)
		resetWorktree(batchCtx, name)
		return nil, 1
	}

	logger.Infof("[%s] Created PR #%d for %s/%s: %s",
		name, pr.ID, repo.Organization, repo.Name, pr.URL)

	if switchErr := batchCtx.SwitchToDefault(); switchErr != nil {
		logger.Warnf("[%s] Failed to switch back to default branch: %v", name, switchErr)
	}

	return []entities.PullRequest{*pr}, 0
}
