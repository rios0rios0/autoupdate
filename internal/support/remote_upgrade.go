package support

import (
	"context"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// RemoteUpgrade is one updater's clone-and-push run against a repository. The
// provider-facing steps are shared -- the check for a pull request that is
// already open, the dry run, the "nothing to push" outcome and the pull
// request itself -- and the updater supplies what differs.
type RemoteUpgrade struct {
	// LogPrefix tags the log lines, e.g. "csharp".
	LogPrefix string

	// BranchName is the branch the run pushes and opens the pull request from.
	BranchName string

	// DryRun logs what the run would have done; it is called in place of
	// Upgrade when the run is a dry run.
	DryRun func()

	// Upgrade clones the repository, applies the upgrade and pushes the
	// branch, reporting whether anything was pushed and, when it was, the pull
	// request to open for it.
	Upgrade func(ctx context.Context) (RemoteUpgradeResult, error)
}

// RemoteUpgradeResult is what RemoteUpgrade.Upgrade reports back.
type RemoteUpgradeResult struct {
	// Pushed is false when the upgrade found nothing to change.
	Pushed bool
	// PullRequest describes the pull request to open when Pushed is true.
	PullRequest PullRequestSpec
}

// RunRemoteUpgrade drives one clone-and-push run: it skips a branch that
// already has a pull request open, honours a dry run, runs the upgrade, and
// opens the pull request when the upgrade pushed something.
func RunRemoteUpgrade(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
	run RemoteUpgrade,
) ([]entities.PullRequest, error) {
	if PullRequestOpen(ctx, provider, repo, run.LogPrefix, run.BranchName) {
		return []entities.PullRequest{}, nil
	}

	if opts.DryRun {
		run.DryRun()
		return []entities.PullRequest{}, nil
	}

	result, err := run.Upgrade(ctx)
	if err != nil {
		return nil, err
	}

	if !result.Pushed {
		logger.Infof("[%s] %s/%s: already up to date", run.LogPrefix, repo.Organization, repo.Name)
		return []entities.PullRequest{}, nil
	}

	return OpenPullRequest(ctx, provider, repo, opts, result.PullRequest)
}

// DryRunPlan is what a dry run reports instead of cloning.
type DryRunPlan struct {
	// Runtime names what the version pin tracks, e.g. "Node.js".
	Runtime string
	// Version is the release the pin would move to.
	Version string
	// UpgradesVersion reports whether the pin would move at all; otherwise
	// only the dependencies are refreshed.
	UpgradesVersion bool
	// Dependencies names what the run refreshes, e.g. "JavaScript dependencies".
	Dependencies string
}

// LogRemoteDryRun logs what a clone-and-push run would have done.
func LogRemoteDryRun(logPrefix string, repo entities.Repository, plan DryRunPlan) {
	if plan.UpgradesVersion {
		logger.Infof(
			"[%s] [DRY RUN] Would upgrade %s to %s and update deps for %s/%s",
			logPrefix, plan.Runtime, plan.Version, repo.Organization, repo.Name,
		)
		return
	}

	logger.Infof(
		"[%s] [DRY RUN] Would update %s for %s/%s",
		logPrefix, plan.Dependencies, repo.Organization, repo.Name,
	)
}
