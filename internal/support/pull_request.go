package support

import (
	"context"
	"fmt"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// PullRequestSpec describes the pull request an updater opens for an upgrade
// branch.
type PullRequestSpec struct {
	// LogPrefix tags the log line, e.g. "csharp".
	LogPrefix string
	// BranchName is the upgrade branch, without the ref prefix.
	BranchName  string
	Title       string
	Description string
}

// BranchPullRequest describes a pull request opened from file changes the
// provider commits on the updater's behalf, which is how the updaters that
// edit files through the provider API -- rather than through a clone -- push
// their upgrade.
type BranchPullRequest struct {
	PullRequestSpec

	CommitMessage string
	Changes       []entities.FileChange
}

// PullRequestOpen reports whether the provider already has a pull request open
// for branchName, in which case the run has nothing left to do.
//
// A failed check is logged and answered with false: the provider being
// momentarily unreachable is not a reason to skip the upgrade, and a duplicate
// pull request is the cheaper of the two failures.
func PullRequestOpen(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	logPrefix, branchName string,
) bool {
	exists, err := provider.PullRequestExists(ctx, repo, branchName)
	if err != nil {
		logger.Warnf("[%s] Failed to check existing PRs: %v", logPrefix, err)
	}
	if exists {
		logger.Infof("[%s] PR already exists for branch %q, skipping", logPrefix, branchName)
	}

	return exists
}

// TargetBranchRef returns the ref a pull request targets: the repository's
// default branch, unless the run overrides it.
func TargetBranchRef(repo entities.Repository, opts entities.UpdateOptions) string {
	if opts.TargetBranch != "" {
		return refsHeadsPrefix + opts.TargetBranch
	}

	return repo.DefaultBranch
}

// OpenPullRequest creates the pull request for a branch that has already been
// pushed and logs where it ended up.
func OpenPullRequest(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
	spec PullRequestSpec,
) ([]entities.PullRequest, error) {
	pr, err := provider.CreatePullRequest(ctx, repo, entities.PullRequestInput{
		SourceBranch: refsHeadsPrefix + spec.BranchName,
		TargetBranch: TargetBranchRef(repo, opts),
		Title:        spec.Title,
		Description:  spec.Description,
		AutoComplete: opts.AutoComplete,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}

	logger.Infof(
		"[%s] Created PR #%d for %s/%s: %s",
		spec.LogPrefix, pr.ID, repo.Organization, repo.Name, pr.URL,
	)

	return []entities.PullRequest{*pr}, nil
}

// PushChangesAndOpenPullRequest commits the changes on a new branch through the
// provider API, based on the branch the pull request will target, and then
// opens that pull request.
func PushChangesAndOpenPullRequest(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	opts entities.UpdateOptions,
	pr BranchPullRequest,
) ([]entities.PullRequest, error) {
	err := provider.CreateBranchWithChanges(ctx, repo, entities.BranchInput{
		BranchName:    pr.BranchName,
		BaseBranch:    TargetBranchRef(repo, opts),
		Changes:       pr.Changes,
		CommitMessage: pr.CommitMessage,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create branch: %w", err)
	}

	return OpenPullRequest(ctx, provider, repo, opts, pr.PullRequestSpec)
}
