package application

import (
	"context"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/config"
	"github.com/rios0rios0/autoupdate/domain"
	providerPkg "github.com/rios0rios0/autoupdate/infrastructure/provider"
	updaterPkg "github.com/rios0rios0/autoupdate/infrastructure/updater"
)

// UpdateService orchestrates the full dependency update flow:
// discover repositories -> detect ecosystems -> create update PRs.
type UpdateService struct {
	providerRegistry *providerPkg.Registry
	updaterRegistry  *updaterPkg.Registry
}

// NewUpdateService creates a new service with the given registries.
func NewUpdateService(
	providerRegistry *providerPkg.Registry,
	updaterRegistry *updaterPkg.Registry,
) *UpdateService {
	return &UpdateService{
		providerRegistry: providerRegistry,
		updaterRegistry:  updaterRegistry,
	}
}

// RunOptions holds runtime options for a single run.
type RunOptions struct {
	DryRun       bool
	Verbose      bool
	ProviderName string // If set, only process this provider (CLI override)
	OrgOverride  string // If set, only process this org (CLI override)
	UpdaterName  string // If set, only run this updater (CLI override)
}

// Run executes the full update cycle using the provided configuration.
func (s *UpdateService) Run(
	ctx context.Context,
	cfg *config.Config,
	runOpts RunOptions,
) error {
	if runOpts.Verbose {
		logger.SetLevel(logger.DebugLevel)
	}

	totalPRs := 0
	totalRepos := 0
	totalErrors := 0

	for _, provCfg := range cfg.Providers {
		// Skip if CLI filter is set and doesn't match
		if runOpts.ProviderName != "" && provCfg.Type != runOpts.ProviderName {
			continue
		}

		provider, err := s.providerRegistry.Get(provCfg.Type, provCfg.Token)
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
				prs, errs := s.processRepository(ctx, provider, repo, cfg, runOpts)
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
func (s *UpdateService) processRepository(
	ctx context.Context,
	provider domain.Provider,
	repo domain.Repository,
	cfg *config.Config,
	runOpts RunOptions,
) ([]domain.PullRequest, int) {
	var allPRs []domain.PullRequest
	errorCount := 0

	updaters := s.updaterRegistry.All()
	for _, u := range updaters {
		// Skip if CLI filter is set and doesn't match
		if runOpts.UpdaterName != "" && u.Name() != runOpts.UpdaterName {
			continue
		}

		// Skip if disabled in config
		if updaterCfg, ok := cfg.Updaters[u.Name()]; ok {
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
		opts := domain.UpdateOptions{
			DryRun:  runOpts.DryRun,
			Verbose: runOpts.Verbose,
		}

		if updaterCfg, ok := cfg.Updaters[u.Name()]; ok {
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
