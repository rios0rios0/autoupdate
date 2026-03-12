package commands

import (
	"context"
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
		// Skip if CLI filter is set and doesn't match
		if runOpts.ProviderName != "" && provCfg.Type != runOpts.ProviderName {
			continue
		}

		provider, err := it.providerRegistry.Get(provCfg.Type, provCfg.Token)
		if err != nil {
			logger.Errorf("Failed to initialize provider %q: %v", provCfg.Type, err)
			totalErrors++
			continue
		}

		logger.Infof("Processing provider: %s", provider.Name())

		for _, org := range provCfg.Organizations {
			// Skip if CLI filter is set and doesn't match
			if runOpts.OrgOverride != "" && org != runOpts.OrgOverride {
				continue
			}

			logger.Infof("Discovering repositories in %q...", org)

			repos, discoverErr := provider.DiscoverRepositories(ctx, org)
			if discoverErr != nil {
				logger.Errorf("Failed to discover repos in %q: %v", org, discoverErr)
				totalErrors++
				continue
			}

			logger.Infof("Found %d repositories in %q", len(repos), org)

			for _, repo := range repos {
				totalRepos++
				prs, errs := it.processRepository(ctx, provider, repo, settings, runOpts)
				totalPRs += len(prs)
				totalErrors += errs
			}
		}
	}

	logger.Infof(
		"Run complete: %d repos processed, %d PRs created, %d errors",
		totalRepos, totalPRs, totalErrors,
	)
	return nil
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
	// Collect applicable updaters
	var localUpdaters []applicableUpdater
	var legacyUpdaters []applicableUpdater

	updaters := it.updaterRegistry.All()
	for _, u := range updaters {
		if runOpts.UpdaterName != "" && u.Name() != runOpts.UpdaterName {
			continue
		}

		if updaterCfg, ok := settings.Updaters[u.Name()]; ok {
			if !updaterCfg.Enabled {
				continue
			}
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
			localUpdaters = append(localUpdaters, au)
		} else {
			legacyUpdaters = append(legacyUpdaters, au)
		}
	}

	var allPRs []entities.PullRequest
	errorCount := 0

	// Process local updaters via clone-based pipeline
	if len(localUpdaters) > 0 {
		prs, errs := it.processLocalUpdaters(ctx, provider, repo, settings, localUpdaters)
		allPRs = append(allPRs, prs...)
		errorCount += errs
	}

	// Process legacy updaters via the original CreateUpdatePRs flow
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
		lu := au.updater.(repositories.LocalUpdater) //nolint:forcetypeassert // checked at partition time
		prs, errs := it.runLocalUpdater(ctx, batchCtx, lu, au, provider, repo, settings, authMethods)
		allPRs = append(allPRs, prs...)
		errorCount += errs
	}

	return allPRs, errorCount
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
		logger.Errorf("[%s] Failed to apply updates to %s/%s: %v",
			name, repo.Organization, repo.Name, err)
		if resetErr := batchCtx.ResetToDefault(); resetErr != nil {
			logger.Warnf("[%s] Failed to reset worktree to default branch: %v", name, resetErr)
		}
		return nil, 1
	}

	if result == nil {
		logger.Infof("[%s] %s/%s: already up to date", name, repo.Organization, repo.Name)
		return nil, 0
	}

	// Check if PR already exists for this branch
	exists, prCheckErr := provider.PullRequestExists(ctx, repo, result.BranchName)
	if prCheckErr != nil {
		logger.Warnf("[%s] Failed to check existing PRs: %v", name, prCheckErr)
	}
	if exists {
		logger.Infof("[%s] PR already exists for branch %q, skipping", name, result.BranchName)
		if resetErr := batchCtx.ResetToDefault(); resetErr != nil {
			logger.Warnf("[%s] Failed to reset worktree to default branch: %v", name, resetErr)
		}
		return nil, 0
	}

	// Create branch, commit, and push
	if branchErr := batchCtx.CreateBranchFromDefault(result.BranchName); branchErr != nil {
		logger.Errorf("[%s] Failed to create branch %s: %v", name, result.BranchName, branchErr)
		if resetErr := batchCtx.ResetToDefault(); resetErr != nil {
			logger.Warnf("[%s] Failed to reset worktree to default branch: %v", name, resetErr)
		}
		return nil, 1
	}

	// ApplyUpdates was called while on the default branch, so changes are in
	// the working tree. The branch switch doesn't discard uncommitted changes.
	pushed, pushErr := batchCtx.CommitSignedAndPush(result.BranchName, result.CommitMessage, settings, authMethods)
	if pushErr != nil {
		logger.Errorf("[%s] Failed to commit/push for %s/%s: %v",
			name, repo.Organization, repo.Name, pushErr)
		if resetErr := batchCtx.ResetToDefault(); resetErr != nil {
			logger.Warnf("[%s] Failed to reset worktree to default branch: %v", name, resetErr)
		}
		return nil, 1
	}

	if !pushed {
		logger.Infof("[%s] %s/%s: no changes after apply", name, repo.Organization, repo.Name)
		if resetErr := batchCtx.ResetToDefault(); resetErr != nil {
			logger.Warnf("[%s] Failed to reset worktree to default branch: %v", name, resetErr)
		}
		return nil, 0
	}

	// Create the PR
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
		if resetErr := batchCtx.ResetToDefault(); resetErr != nil {
			logger.Warnf("[%s] Failed to reset worktree to default branch: %v", name, resetErr)
		}
		return nil, 1
	}

	logger.Infof("[%s] Created PR #%d for %s/%s: %s",
		name, pr.ID, repo.Organization, repo.Name, pr.URL)

	// Switch back to default branch for the next updater
	if switchErr := batchCtx.SwitchToDefault(); switchErr != nil {
		logger.Warnf("[%s] Failed to switch back to default branch: %v", name, switchErr)
	}

	return []entities.PullRequest{*pr}, 0
}
