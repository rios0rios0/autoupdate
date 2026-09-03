package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	logger "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	infraRepos "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlocal"
	"github.com/rios0rios0/autoupdate/internal/support"
	gitops "github.com/rios0rios0/gitforge/pkg/git/infrastructure"
)

// defaultRepoConcurrency is the number of repositories processed in parallel
// within an organization when neither the config file nor the CLI overrides it.
// Repository processing is I/O-bound (clone + remote API calls), so a small
// fan-out yields a near-linear speedup without overwhelming provider rate
// limits or local disk. Set concurrency to 1 to restore sequential processing.
const defaultRepoConcurrency = 4

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
	Concurrency  int    // If > 0, process this many repos in parallel (CLI override)
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

	gitlocal.CleanupStaleTempDirs()

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
	concurrency := resolveConcurrency(settings, runOpts)
	logger.Infof("Found %d repositories in %q (processing up to %d in parallel)",
		len(repos), org, concurrency)

	// Repository processing is independent and I/O-bound (clone + remote API
	// calls), so repos are processed concurrently with a bounded worker pool.
	// Each repo clones into its own temp directory, the shared provider/updater
	// instances are stateless, and the aggregate counters are guarded by a
	// mutex, so the fan-out is safe. Per-repo failures are counted (not
	// propagated) to preserve the "continue on error" behavior, so the
	// goroutines never return an error and Wait cannot fail.
	var (
		mu          sync.Mutex
		totalPRs    int
		totalErrors int
	)

	var group errgroup.Group
	group.SetLimit(concurrency)
	for _, repo := range repos {
		group.Go(func() error {
			prs, errs := it.processRepository(ctx, provider, repo, settings, runOpts)
			mu.Lock()
			totalPRs += len(prs)
			totalErrors += errs
			mu.Unlock()
			return nil
		})
	}
	_ = group.Wait()

	return totalPRs, len(repos), totalErrors
}

// resolveConcurrency determines how many repositories to process in parallel.
// Precedence: CLI override (runOpts) > config file (settings) > built-in
// default. A zero value at a given level means "not set" and falls through to
// the next level; any explicitly set value below 1 is clamped to 1 (fully
// sequential).
func resolveConcurrency(settings *entities.Settings, runOpts RunOptions) int {
	concurrency := defaultRepoConcurrency
	if settings != nil && settings.Concurrency != 0 {
		concurrency = settings.Concurrency
	}
	if runOpts.Concurrency != 0 {
		concurrency = runOpts.Concurrency
	}
	if concurrency < 1 {
		concurrency = 1
	}
	return concurrency
}

// filterRepositories removes repositories that match the exclusion criteria
// defined in the settings (e.g. forks, archived repos, or anything in the
// global exclude_repos list).
func filterRepositories(
	repos []entities.Repository,
	settings *entities.Settings,
) []entities.Repository {
	if !settings.ExcludeForks && !settings.ExcludeArchived && len(settings.ExcludeRepos) == 0 {
		return repos
	}

	filtered := make([]entities.Repository, 0, len(repos))
	for _, repo := range repos {
		// The same predicates the project layer gets a second pass at, through
		// entities.ExcludesSelf. Sharing them is what keeps the two from drifting.
		if excluded, rule := entities.ExcludesSelf(settings, repo); excluded {
			logger.Infof("Skipping %s: matched %s", entities.RepoKey(repo), rule)
			continue
		}
		filtered = append(filtered, repo)
	}
	return filtered
}

// loadRepoSettings reads the target repository's `.autoupdate.yaml` over the provider API
// and returns the settings this repository is processed with, or nil when the file asks to
// be skipped.
//
// One fetch answers both questions the file can settle, because they come from the same
// document. Transient fetch and parse errors fail open -- on the operator's settings,
// unchanged -- so a flaky API call cannot silently disable every update in an organization.
func loadRepoSettings(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	settings *entities.Settings,
) *entities.Settings {
	key := entities.RepoKey(repo)

	config, err := support.LoadRemoteRepoConfig(ctx, provider, repo)
	if err != nil {
		logger.Warnf("Could not read %s for %s: %v (continuing without it)",
			entities.RepoConfigFile, key, err)
		return settings
	}

	if config.IsSkipped() {
		if config.Reason != "" {
			logger.Infof("Skipping %s: %s requested skip (%s)",
				key, entities.RepoConfigFile, config.Reason)
		} else {
			logger.Infof("Skipping %s: %s requested skip", key, entities.RepoConfigFile)
		}
		return nil
	}

	merged, err := entities.ApplyRepoOverlay(settings, config)
	if err != nil {
		logger.Warnf("Could not apply %s for %s: %v (continuing without it)",
			entities.RepoConfigFile, key, err)
		return settings
	}

	// The second pass filterRepositories could not do: it runs organization-wide, before
	// any repository's own file exists to be read.
	if excluded, rule := entities.ExcludesSelf(merged, repo); excluded {
		logger.Infof("Skipping %s: its own %s matched %s", key, entities.RepoConfigFile, rule)
		return nil
	}

	return merged
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
	// The repository's own configuration is the last layer. It is folded onto a copy, not
	// through the shared settings: this runs inside the per-organization fan-out, so a
	// layer that wrote through would leak one repository's configuration into another's.
	settings = loadRepoSettings(ctx, provider, repo, settings)
	if settings == nil {
		return nil, 0
	}

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

		if updaterCfg, ok := settings.Updaters[u.Name()]; ok && !updaterCfg.IsEnabled() {
			continue
		}

		if !u.Detect(ctx, provider, repo) {
			continue
		}

		logger.Infof("[%s] Detected in %s/%s", u.Name(), repo.Organization, repo.Name)

		opts := entities.UpdateOptions{
			DryRun:            runOpts.DryRun,
			Verbose:           runOpts.Verbose,
			AllowMajorUpdates: entities.MajorUpdatesAllowed(settings),
		}
		if updaterCfg, ok := settings.Updaters[u.Name()]; ok {
			opts.AutoComplete = updaterCfg.IsAutoComplete()
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

// appliedUpdaterResult pairs the updater name with the result it returned
// from ApplyUpdates so the synthesis helpers can build a single aggregate
// commit and PR.
type appliedUpdaterResult struct {
	name   string
	result *repositories.LocalUpdateResult
}

// processLocalUpdaters clones the repository once and runs every applicable
// local updater against a single aggregate branch, producing one signed
// commit and one pull request that bundles all the changes. Per-updater
// failure isolation is handled via snapshot commits on BatchGitContext.
func (it *RunCommand) processLocalUpdaters(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	settings *entities.Settings,
	updaters []applicableUpdater,
) ([]entities.PullRequest, int) {
	if allDryRun(updaters) {
		logAggregateDryRun(updaters, repo)
		return nil, 0
	}

	// Same-day idempotency: short-circuit before touching git if the aggregate
	// PR already exists for the target branch on this day.
	aggregateBranch := buildAggregateBranchName(
		entities.ResolveAggregateBranchPrefix(settings), time.Now(),
	)

	exists, checkErr := provider.PullRequestExists(ctx, repo, aggregateBranch)
	if checkErr != nil {
		logger.Warnf("[autoupdate] Failed to check existing PRs for %s/%s: %v",
			repo.Organization, repo.Name, checkErr)
	} else if exists {
		logger.Infof("[autoupdate] PR already exists for %s/%s on branch %q, skipping",
			repo.Organization, repo.Name, aggregateBranch)
		return nil, 0
	}

	// The clone base ref and the PR target ref must match so the aggregate
	// branch is not based on a different ref than the PR targets (which would
	// produce unexpected diffs and conflicts when TargetBranch is overridden).
	targetBranch := strings.TrimPrefix(
		resolveAggregateTargetBranch(repo, updaters), "refs/heads/",
	)

	cloneURL := provider.CloneURL(repo)
	serviceType := gitlocal.ResolveServiceTypeFromURL(it.providerRegistry, cloneURL)
	authMethods := gitlocal.CollectBatchAuthMethods(
		it.providerRegistry, serviceType, provider.AuthToken(), settings,
	)

	gitOps := gitops.NewGitOperations(it.providerRegistry)
	batchCtx, err := gitlocal.CloneRepository(
		gitOps, cloneURL, targetBranch, authMethods, it.providerRegistry,
	)
	if err != nil {
		logger.Errorf("Failed to clone %s/%s: %v", repo.Organization, repo.Name, err)
		return nil, 1
	}
	defer batchCtx.Close()

	// Drop the dated branches left behind by earlier runs, closing their pull requests,
	// before today's branch is created. Reaching this point means no pull request exists
	// for today yet, so nothing is closed without a replacement being opened for it.
	if entities.CleanupEnabled(settings) {
		cleanupStaleAggregateBranches(
			ctx, batchCtx, provider, repo, settings, targetBranch, authMethods,
		)
	}

	if branchErr := batchCtx.CreateBranchFromDefault(aggregateBranch); branchErr != nil {
		logger.Errorf("[autoupdate] Failed to create branch %s for %s/%s: %v",
			aggregateBranch, repo.Organization, repo.Name, branchErr)
		return nil, 1
	}

	applied, errorCount := it.runUpdatersOnBranch(ctx, batchCtx, updaters, provider, repo)
	if len(applied) == 0 {
		logger.Infof("[autoupdate] %s/%s: no updaters produced changes",
			repo.Organization, repo.Name)
		return nil, errorCount
	}

	pr, pushErrs := it.commitPushAndOpenPR(
		ctx, batchCtx, provider, repo, settings, authMethods,
		aggregateBranch, applied, updaters,
	)
	if pr == nil {
		return nil, errorCount + pushErrs
	}
	return []entities.PullRequest{*pr}, errorCount + pushErrs
}

// runUpdatersOnBranch runs each applicable LocalUpdater against the shared
// worktree on the aggregate branch with snapshot-based failure isolation.
// On success with real changes, the snapshot advances so the next updater
// builds on top of it. On failure, the worktree hard-resets to the last
// known-good snapshot, discarding only the failing updater's partial writes.
func (it *RunCommand) runUpdatersOnBranch(
	ctx context.Context,
	batchCtx *gitlocal.BatchGitContext,
	updaters []applicableUpdater,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) ([]appliedUpdaterResult, int) {
	snapshot, err := batchCtx.HeadHash()
	if err != nil {
		logger.Errorf("[autoupdate] Failed to resolve HEAD for %s/%s: %v",
			repo.Organization, repo.Name, err)
		return nil, 1
	}

	var applied []appliedUpdaterResult
	errorCount := 0

	for _, au := range updaters {
		name := au.updater.Name()
		lu := au.updater.(repositories.LocalUpdater) //nolint:errcheck,forcetypeassert // checked at partition time

		if au.opts.DryRun {
			logger.Infof("[%s] [DRY RUN] Would apply updates to %s/%s via aggregate pipeline",
				name, repo.Organization, repo.Name)
			continue
		}

		result, applyErr := lu.ApplyUpdates(ctx, batchCtx.RepoDir(), provider, repo, au.opts)
		if applyErr != nil {
			if errors.Is(applyErr, repositories.ErrNoUpdatesNeeded) {
				logger.Infof("[%s] %s/%s: already up to date",
					name, repo.Organization, repo.Name)
				continue
			}
			logger.Errorf("[%s] Failed to apply updates to %s/%s: %v",
				name, repo.Organization, repo.Name, applyErr)
			errorCount++
			if rbErr := batchCtx.RestoreSnapshot(snapshot); rbErr != nil {
				// A failed restore leaves the worktree in an unknown state,
				// so any subsequent updater would be building on top of a
				// potentially corrupted tree. Abort the repo entirely
				// instead of silently producing a tainted aggregate PR.
				logger.Errorf(
					"[%s] failed to restore snapshot after updater failure for %s/%s: "+
						"updater error: %v; restore error: %v",
					name, repo.Organization, repo.Name, applyErr, rbErr,
				)
				return nil, errorCount
			}
			continue
		}

		if result == nil {
			continue
		}

		applied = append(applied, appliedUpdaterResult{name: name, result: result})
		logger.Infof("[%s] %s/%s: staged changes for aggregate PR",
			name, repo.Organization, repo.Name)

		hasChanges, hcErr := batchCtx.HasChanges()
		if hcErr != nil {
			logger.Warnf("[%s] Failed to detect changes after apply: %v", name, hcErr)
			continue
		}
		if !hasChanges {
			continue
		}

		newSnap, snapErr := batchCtx.AdvanceSnapshot(snapshot)
		if snapErr != nil {
			// If the snapshot fails to advance, a later updater failure
			// would RestoreSnapshot back to the older hash and discard
			// this updater's successful changes. Treat the advance
			// failure as fatal for the repo to preserve correctness.
			logger.Errorf(
				"[%s] failed to advance snapshot for %s/%s: %v",
				name, repo.Organization, repo.Name, snapErr,
			)
			errorCount++
			return nil, errorCount
		}
		snapshot = newSnap
	}

	return applied, errorCount
}

// commitPushAndOpenPR flattens the snapshot chain back into the worktree,
// builds the aggregate commit/PR text, signs and pushes the single commit,
// and opens the consolidated pull request.
func (it *RunCommand) commitPushAndOpenPR(
	ctx context.Context,
	batchCtx *gitlocal.BatchGitContext,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	settings *entities.Settings,
	authMethods []transport.AuthMethod,
	branchName string,
	applied []appliedUpdaterResult,
	updaters []applicableUpdater,
) (*entities.PullRequest, int) {
	if flattenErr := batchCtx.FlattenToWorktree(); flattenErr != nil {
		logger.Errorf("[autoupdate] Failed to flatten worktree for %s/%s: %v",
			repo.Organization, repo.Name, flattenErr)
		return nil, 1
	}

	commitMsg := buildAggregateCommitMessage(applied)

	pushed, pushErr := batchCtx.CommitSignedAndPush(branchName, commitMsg, settings, authMethods)
	if pushErr != nil {
		logger.Errorf("[autoupdate] Failed to commit/push for %s/%s: %v",
			repo.Organization, repo.Name, pushErr)
		return nil, 1
	}
	if !pushed {
		logger.Infof("[autoupdate] %s/%s: no net changes after apply, skipping PR",
			repo.Organization, repo.Name)
		return nil, 0
	}

	pr, createErr := provider.CreatePullRequest(ctx, repo, entities.PullRequestInput{
		SourceBranch: "refs/heads/" + branchName,
		TargetBranch: resolveAggregateTargetBranch(repo, updaters),
		Title:        buildAggregatePRTitle(applied),
		Description:  buildAggregatePRDescription(applied),
		AutoComplete: anyAutoComplete(updaters),
	})
	if createErr != nil {
		logger.Errorf("[autoupdate] Failed to create PR for %s/%s: %v",
			repo.Organization, repo.Name, createErr)
		return nil, 1
	}

	logger.Infof("[autoupdate] Created PR #%d for %s/%s: %s",
		pr.ID, repo.Organization, repo.Name, pr.URL)

	if switchErr := batchCtx.SwitchToDefault(); switchErr != nil {
		logger.Warnf("[autoupdate] Failed to switch back to default branch: %v", switchErr)
	}

	return pr, 0
}

// buildAggregateBranchName returns the deterministic consolidated branch name for a
// given prefix and run timestamp, as `<prefix>YYYY-MM-DD` in UTC. Two invocations on
// the same UTC day for the same repo land on the same branch, which together with the
// PullRequestExists pre-check makes same-day re-runs idempotent without force-pushing
// over an under-review pull request.
//
// The prefix comes from entities.ResolveAggregateBranchPrefix, so the branch a run
// creates and the branches cleanup removes are always named by the same value.
func buildAggregateBranchName(prefix string, now time.Time) string {
	return prefix + now.UTC().Format("2006-01-02")
}

// buildAggregateCommitMessage synthesizes a single commit message that
// covers every updater that produced changes. For a single contributor
// the message passes through verbatim so the one-PR path is
// indistinguishable from the pre-refactor single-updater output.
func buildAggregateCommitMessage(applied []appliedUpdaterResult) string {
	if len(applied) == 1 {
		return applied[0].result.CommitMessage
	}

	var sb strings.Builder
	sb.WriteString("chore(deps): bumped dependencies via autoupdate\n\n")
	for _, a := range applied {
		fmt.Fprintf(&sb, "- [%s] %s\n", a.name, firstLine(a.result.CommitMessage))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// buildAggregatePRTitle returns the PR title. For a single updater it is
// that updater's original title; for multiple it lists the contributing
// updater names so reviewers can see at a glance which ecosystems moved.
func buildAggregatePRTitle(applied []appliedUpdaterResult) string {
	if len(applied) == 1 {
		return applied[0].result.PRTitle
	}
	names := make([]string, 0, len(applied))
	for _, a := range applied {
		names = append(names, a.name)
	}
	return fmt.Sprintf("chore(deps): bumped dependencies (%s)", strings.Join(names, ", "))
}

// buildAggregatePRDescription renders the PR body with one section per
// contributing updater, preserving each updater's original PR description
// so reviewers see the full context for every change.
func buildAggregatePRDescription(applied []appliedUpdaterResult) string {
	if len(applied) == 1 {
		return applied[0].result.PRDescription
	}

	var sb strings.Builder
	sb.WriteString("## Summary\n\n")
	sb.WriteString(
		"This pull request bundles updates from multiple autoupdate updaters into a single branch " +
			"so reviewers see the full set of changes for this repository in one place.\n\n",
	)
	sb.WriteString("Contributing updaters:\n\n")
	for _, a := range applied {
		fmt.Fprintf(&sb, "- `%s` — %s\n", a.name, firstLine(a.result.PRTitle))
	}
	sb.WriteString("\n---\n\n")
	for _, a := range applied {
		fmt.Fprintf(&sb, "## %s\n\n", a.name)
		if a.result.PRDescription != "" {
			sb.WriteString(a.result.PRDescription)
			sb.WriteString("\n\n")
		} else {
			sb.WriteString("_(no description provided by updater)_\n\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// resolveAggregateTargetBranch picks the target branch for the aggregate
// PR. All enabled updaters should agree (they read the same settings); if
// any one overrides TargetBranch we honor the first non-empty override
// for determinism.
func resolveAggregateTargetBranch(
	repo entities.Repository, updaters []applicableUpdater,
) string {
	for _, au := range updaters {
		if au.opts.TargetBranch != "" {
			return "refs/heads/" + au.opts.TargetBranch
		}
	}
	return repo.DefaultBranch
}

// anyAutoComplete returns true if at least one applicable updater requested
// auto-complete. The aggregate PR represents the same logical "bump
// dependencies" change every contributor individually requested, so honoring
// auto-complete when anyone asks for it preserves their intent.
func anyAutoComplete(updaters []applicableUpdater) bool {
	for _, au := range updaters {
		if au.opts.AutoComplete {
			return true
		}
	}
	return false
}

// allDryRun reports whether every applicable updater is running in dry-run
// mode, in which case the aggregate pipeline can short-circuit before
// cloning anything.
func allDryRun(updaters []applicableUpdater) bool {
	if len(updaters) == 0 {
		return false
	}
	for _, au := range updaters {
		if !au.opts.DryRun {
			return false
		}
	}
	return true
}

// logAggregateDryRun emits the dry-run summary line for an aggregate run
// where every applicable updater is dry-run.
func logAggregateDryRun(updaters []applicableUpdater, repo entities.Repository) {
	names := make([]string, 0, len(updaters))
	for _, au := range updaters {
		names = append(names, au.updater.Name())
	}
	logger.Infof(
		"[autoupdate] [DRY RUN] Would apply %d updater(s) to %s/%s: %s",
		len(updaters), repo.Organization, repo.Name, strings.Join(names, ", "),
	)
}

// firstLine returns the first newline-delimited segment of s, or s itself
// when there is no newline. Used to extract subject lines from multi-line
// commit messages and PR titles.
func firstLine(s string) string {
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before
	}
	return s
}
