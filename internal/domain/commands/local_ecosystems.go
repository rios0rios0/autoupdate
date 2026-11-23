package commands

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlocal"
	"github.com/rios0rios0/autoupdate/internal/support"
	langEntities "github.com/rios0rios0/langforge/pkg/domain/entities"
	langRegistry "github.com/rios0rios0/langforge/pkg/infrastructure/registry"
)

// primaryUpgrade is what the language langforge reads first in the checkout contributes to
// a local run: the dedicated upgrade Go, JavaScript, Python and Dart have, which keeps the
// credential rules of local mode (its git only ever receives git's token) and, for Go,
// prepares the major migrations after its push.
type primaryUpgrade struct {
	language langEntities.Language
	// updater is the language's name in the configuration's `updaters` section, or the
	// empty string for a language with no updater.
	updater string
	// dedicated reports that the language has a dedicated local upgrade, whether or not
	// the configuration lets it run.
	dedicated bool
	// disabled reports that the configuration turns the language's updater off.
	disabled bool
	// handler is the dedicated upgrade to run: nil when the language has none, or when
	// its updater is disabled.
	handler localUpgradeHandler
}

// detectPrimaryUpgrade reads the checkout's primary language and the dedicated upgrade it
// gets. A checkout langforge reads no language in is not an error yet: the updaters may
// still find an ecosystem in it (a Dockerfile, a pipeline, Terraform below the root), and
// the error is returned for Execute to report when none does.
func detectPrimaryUpgrade(repoDir string, settings *entities.Settings) (primaryUpgrade, error) {
	langProvider, err := langRegistry.NewDefaultRegistry().Detect(repoDir)
	if err != nil {
		return primaryUpgrade{language: langEntities.LanguageUnknown}, err
	}

	primary := primaryUpgrade{language: langProvider.Language()}
	primary.updater = updaterNameForLanguage()[primary.language]
	logger.Infof("Detected project type: %s", primary.language)

	handler, handlerErr := localUpgradeHandlerFor(primary.language)
	primary.dedicated = handlerErr == nil

	if !updaterEnabledForLanguage(settings, primary.language) {
		logger.Infof("Skipping %s: its updater is disabled in the configuration", primary.language)
		primary.disabled = true
		return primary, nil
	}

	if primary.dedicated {
		primary.handler = handler
	}
	return primary, nil
}

// dedicatedUpdater names the updater the dedicated upgrade stands in for, so the run does
// not upgrade the same ecosystem twice, or the empty string when there is none.
func (p primaryUpgrade) dedicatedUpdater() string {
	if !p.dedicated {
		return ""
	}
	return p.updater
}

// nothingToUpgrade is what a run that found nothing to upgrade returns: langforge's error
// when it read no language and no updater found an ecosystem either, nothing when the
// configuration turned the primary language's updater off, and otherwise the project type
// that has no upgrade.
func (p primaryUpgrade) nothingToUpgrade(detectErr error) error {
	switch {
	case detectErr != nil:
		return detectErr
	case p.disabled:
		return nil
	default:
		return fmt.Errorf("unsupported project type: %s", p.language)
	}
}

// localUpdatersFor returns, in name order, the updaters that upgrade the checkout through
// ApplyUpdates -- the path `autoupdate run` takes for every ecosystem -- leaving out the one
// a dedicated upgrade covers: every registered updater that implements
// repositories.LocalUpdater, is enabled, and detects its ecosystem in the checkout.
//
// Detection reads the checkout from disk (support.CheckoutProvider), through each updater's
// own Detect, so it matches what `autoupdate run` finds in the same tree: a Go module below
// the root, Terraform kept in subdirectories, a Dockerfile, a pipeline.
func (it *LocalCommand) localUpdatersFor(
	ctx context.Context,
	repoDir string,
	repo entities.Repository,
	opts LocalOptions,
	dedicated string,
) []applicableUpdater {
	if it.updaterRegistry == nil {
		return nil
	}

	updaters := it.updaterRegistry.All()
	slices.SortFunc(updaters, func(a, b repositories.UpdaterRepository) int {
		return strings.Compare(a.Name(), b.Name())
	})

	files := support.NewCheckoutProvider(repoDir, repo, nil, "")
	var applicable []applicableUpdater
	for _, u := range updaters {
		if u.Name() == dedicated {
			continue
		}
		if _, ok := u.(repositories.LocalUpdater); !ok {
			continue
		}
		if !updaterEnabled(opts.Settings, u.Name()) {
			continue
		}
		if !u.Detect(ctx, files, repo) {
			continue
		}

		logger.Infof("[%s] Detected in %s", u.Name(), repoDir)
		applicable = append(applicable, applicableUpdater{updater: u, opts: entities.UpdateOptions{
			DryRun:                 opts.DryRun,
			Verbose:                opts.Verbose,
			AllowMajorUpdates:      entities.MajorUpdatesAllowed(opts.Settings),
			PrepareMajorMigrations: entities.PrepareMajorMigrationsEnabled(opts.Settings),
		}})
	}

	return applicable
}

// rehearseEveryEcosystem is the dry run of upgradeEveryEcosystem: the dedicated upgrade
// rehearses itself, and the others are named. Nothing is asked of the provider API and
// nothing on disk changes.
func (it *LocalCommand) rehearseEveryEcosystem(
	ctx context.Context,
	repoDir string,
	remote *remoteInfo,
	tokens localTokens,
	repo entities.Repository,
	opts LocalOptions,
	primary primaryUpgrade,
	others []applicableUpdater,
) error {
	branchName := buildAggregateBranchName(entities.ResolveAggregateBranchPrefix(opts.Settings), time.Now())
	logger.Infof("[DRY RUN] Would upgrade every ecosystem of %s on %s, in one pull request", repoDir, branchName)

	if primary.handler != nil {
		if _, err := primary.handler(
			ctx,
			repoDir,
			remote.ProviderType,
			tokens,
			repo,
			opts,
			it.providerRegistry,
		); err != nil {
			return err
		}
	}

	logAggregateDryRun(others, repo)
	return nil
}

// upgradeEveryEcosystem upgrades a checkout the dedicated upgrade does not cover on its own
// -- one with several ecosystems, or one only the clone-based updaters upgrade -- on one
// branch, named the way `autoupdate run` names its aggregate branch, and opens one pull
// request for all of it.
//
// The dedicated upgrade runs first and commits and pushes on that branch as it always has;
// the other updaters then run on top of it with the snapshot isolation `autoupdate run`
// gives them, so a failing one costs only its own changes, and what they changed becomes a
// second commit. The operator's uncommitted work is parked for the whole run and put back
// on the branch the run started from, exactly as a dedicated upgrade alone would.
func (it *LocalCommand) upgradeEveryEcosystem(
	ctx context.Context,
	repoDir string,
	remote *remoteInfo,
	tokens localTokens,
	repo entities.Repository,
	opts LocalOptions,
	primary primaryUpgrade,
	others []applicableUpdater,
) error {
	branchName := buildAggregateBranchName(entities.ResolveAggregateBranchPrefix(opts.Settings), time.Now())

	api, err := it.apiProvider(ctx, remote.ProviderType, tokens)
	if err != nil {
		return err
	}

	// A pull request already open from today's branch is today's run, as in batch mode.
	if exists, checkErr := api.PullRequestExists(ctx, repo, branchName); checkErr != nil {
		logger.Warnf("Failed to check for an open pull request from %s: %v", branchName, checkErr)
	} else if exists {
		logger.Infof("A pull request from %s is already open, skipping", branchName)
		return nil
	}

	stashCtx, err := gitlocal.NewLocalGitContext(repoDir, it.providerRegistry)
	if err != nil {
		return err
	}
	stashed, err := stashCtx.StashIfDirty()
	if err != nil {
		return err
	}
	if stashed {
		defer restoreOperatorWork(stashCtx, repo.DefaultBranch)
	}

	var applied []appliedUpdaterResult
	if primary.handler != nil {
		dedicated, dedicatedErr := it.runDedicatedOnBranch(
			ctx,
			repoDir,
			remote,
			tokens,
			repo,
			opts,
			primary,
			branchName,
		)
		if dedicatedErr != nil {
			return dedicatedErr
		}
		applied = append(applied, dedicated...)
	}

	// Opened after the dedicated upgrade, which committed through a context of its own.
	runCtx, err := gitlocal.NewLocalGitContext(repoDir, it.providerRegistry)
	if err != nil {
		return err
	}
	if switchErr := switchToRunBranch(runCtx, branchName); switchErr != nil {
		return switchErr
	}

	provider := support.NewCheckoutProvider(repoDir, repo, api, tokens.Git)
	othersApplied, err := applyOtherEcosystems(ctx, runCtx, repoDir, branchName, provider, repo, others, tokens.Git)
	if err != nil {
		return err
	}
	applied = append(applied, othersApplied...)

	if len(applied) == 0 {
		logger.Info("No dependency changes detected, nothing to do.")
		return nil
	}

	return it.openPullRequest(ctx, remote.ProviderType, tokens, repo, branchName,
		buildAggregatePRTitle(applied), buildAggregatePRDescription(applied))
}

// runDedicatedOnBranch runs the dedicated upgrade on the run's branch, and returns what it
// contributes to the pull request: nothing when it changed nothing.
func (it *LocalCommand) runDedicatedOnBranch(
	ctx context.Context,
	repoDir string,
	remote *remoteInfo,
	tokens localTokens,
	repo entities.Repository,
	opts LocalOptions,
	primary primaryUpgrade,
	branchName string,
) ([]appliedUpdaterResult, error) {
	opts.branchName = branchName
	info, err := primary.handler(ctx, repoDir, remote.ProviderType, tokens, repo, opts, it.providerRegistry)
	if err != nil {
		return nil, err
	}
	if !info.HasChanges {
		return nil, nil
	}

	title, description := generatePRContent(info)
	return []appliedUpdaterResult{{name: primary.updater, result: &repositories.LocalUpdateResult{
		BranchName:    branchName,
		CommitMessage: title,
		PRTitle:       title,
		PRDescription: description,
	}}}, nil
}

// applyOtherEcosystems runs the clone-based updaters on the run's branch, isolating each
// one's failure with the snapshot commits `autoupdate run` uses, then folds the snapshots
// into the one commit it pushes. Nothing is pushed when they changed nothing.
func applyOtherEcosystems(
	ctx context.Context,
	runCtx *gitlocal.LocalGitContext,
	repoDir, branchName string,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	others []applicableUpdater,
	gitToken string,
) ([]appliedUpdaterResult, error) {
	snapshots, err := gitlocal.OpenCheckout(repoDir)
	if err != nil {
		return nil, err
	}
	base, err := snapshots.HeadHash()
	if err != nil {
		return nil, err
	}

	// No tooling directory and no shared cache: standalone mode keeps the operator's own
	// package-manager caches, which support.ScriptEnv leaves in place without one.
	applied, failures := runUpdatersOnBranch(ctx, snapshots, "", nil, others, provider, repo)
	if failures > 0 {
		logger.Warnf("%d updater(s) failed on %s; their partial changes were discarded", failures, repoDir)
	}

	if len(applied) == 0 {
		// runUpdatersOnBranch rolls each failure back as it happens, unless the rollback
		// itself failed; whatever that left goes now, so the branch is exactly what the
		// dedicated upgrade pushed, if anything.
		if failures > 0 {
			if restoreErr := snapshots.RestoreSnapshot(base); restoreErr != nil {
				return nil, restoreErr
			}
		}
		return nil, nil
	}

	if flattenErr := snapshots.FlattenOnto(base); flattenErr != nil {
		return nil, flattenErr
	}

	pushed, err := runCtx.StageCommitAndPush(branchName, buildAggregateCommitMessage(applied), gitToken)
	if err != nil {
		return nil, err
	}
	if !pushed {
		return nil, nil
	}

	return applied, nil
}

// apiProvider builds the provider API client the run's updaters and its pre-checks talk
// to, with the API's token asked for now.
func (it *LocalCommand) apiProvider(
	ctx context.Context, providerType string, tokens localTokens,
) (repositories.ProviderRepository, error) {
	token, err := tokens.API(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not authenticate to the %s API: %w", providerType, err)
	}

	provider, err := it.providerRegistry.Get(providerType, token)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider: %w", err)
	}
	return provider, nil
}

// switchToRunBranch leaves the checkout on the run's branch: where the dedicated upgrade
// left it, or a new branch from HEAD when no dedicated upgrade created one.
func switchToRunBranch(runCtx *gitlocal.LocalGitContext, branchName string) error {
	current, err := runCtx.CurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to determine current branch: %w", err)
	}
	if current == branchName {
		return nil
	}

	if createErr := runCtx.CreateBranch(branchName); createErr != nil {
		return fmt.Errorf("failed to create branch %s: %w", branchName, createErr)
	}
	return nil
}

// restoreOperatorWork puts the checkout back on the branch the run started from, with the
// work it parked reapplied. It only logs its failures, because it runs while the run is
// unwinding and must not displace the error that stopped it.
func restoreOperatorWork(stashCtx *gitlocal.LocalGitContext, originalBranch string) {
	if err := stashCtx.CheckoutBranch(originalBranch); err != nil {
		logger.Warnf("Failed to switch back to %s: %v", originalBranch, err)
	}
	if err := stashCtx.RestoreStash(); err != nil {
		logger.Warnf("Failed to restore stash: %v", err)
	}
}
