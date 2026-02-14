package commands

import (
	"context"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	infraRepos "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
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

// processRepository runs all applicable updaters on a single repository.
func (it *RunCommand) processRepository(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	settings *entities.Settings,
	runOpts RunOptions,
) ([]entities.PullRequest, int) {
	var allPRs []entities.PullRequest
	errorCount := 0

	updaters := it.updaterRegistry.All()
	for _, u := range updaters {
		// Skip if CLI filter is set and doesn't match
		if runOpts.UpdaterName != "" && u.Name() != runOpts.UpdaterName {
			continue
		}

		// Skip if disabled in config
		if updaterCfg, ok := settings.Updaters[u.Name()]; ok {
			if !updaterCfg.Enabled {
				continue
			}
		}

		// Detect if this updater applies to the repo
		if !u.Detect(ctx, provider, repo) {
			continue
		}

		logger.Infof("[%s] Detected in %s/%s", u.Name(), repo.Organization, repo.Name)

		// Build update options
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

		prs, err := u.CreateUpdatePRs(ctx, provider, repo, opts)
		if err != nil {
			logger.Errorf(
				"[%s] Failed to update %s/%s: %v",
				u.Name(), repo.Organization, repo.Name, err,
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
