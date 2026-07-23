package commands

import (
	"context"
	"slices"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlocal"
)

// filterStaleAggregateBranches returns the aggregate branches autoupdate owns and may
// safely remove: every branch carrying the aggregate prefix, except the target branch
// the pull requests are opened against.
//
// Because the aggregate branch is dated, a run on a new day never reuses the previous
// day's branch. Left alone, an unattended daily run stacks up one abandoned branch per
// day for as long as nobody merges. Merge status is deliberately ignored: the branch for
// the current day is recreated immediately afterwards, so an unmerged one is the residue
// of a day nobody reviewed rather than work worth keeping.
//
// The result is sorted so the cleanup order, and its log output, is deterministic.
func filterStaleAggregateBranches(branches []string, prefix, targetBranch string) []string {
	stale := make([]string, 0, len(branches))
	for _, branch := range branches {
		if branch == targetBranch {
			continue
		}
		if !strings.HasPrefix(branch, prefix) {
			continue
		}
		stale = append(stale, branch)
	}
	slices.Sort(stale)
	return stale
}

// cleanupStaleAggregateBranches closes the pull request attached to every stale
// aggregate branch and deletes the branch, so repeated unattended runs cannot leave a
// trail of dated branches waiting on a review that never comes.
//
// It is best-effort by design: every failure is logged and the remaining branches are
// still processed. Cleanup is housekeeping that runs ahead of the real work, so it must
// never stop a repository from being updated.
func cleanupStaleAggregateBranches(
	ctx context.Context,
	batchCtx *gitlocal.BatchGitContext,
	provider repositories.ProviderRepository,
	repo entities.Repository,
	settings *entities.Settings,
	targetBranch string,
	authMethods []transport.AuthMethod,
) {
	branches, err := batchCtx.ListRemoteBranches(authMethods)
	if err != nil {
		logger.Warnf("[autoupdate] Could not list branches for %s/%s, skipping cleanup: %v",
			repo.Organization, repo.Name, err)
		return
	}

	prefix := entities.ResolveAggregateBranchPrefix(settings)
	stale := filterStaleAggregateBranches(branches, prefix, targetBranch)
	if len(stale) == 0 {
		return
	}

	logger.Infof("[autoupdate] Cleaning up %d stale %q branch(es) in %s/%s",
		len(stale), prefix+"*", repo.Organization, repo.Name)

	for _, branch := range stale {
		// The pull request is closed first: once the source branch is gone, a provider
		// can no longer resolve the pull request that belonged to it.
		closed, closeErr := provider.ClosePullRequest(ctx, repo, branch)
		if closeErr != nil {
			// The branch stays put so the pair remains retryable. Deleting it now would
			// strand an open pull request whose source branch no longer exists, and the
			// next run would not even see the branch to try closing it again.
			logger.Warnf(
				"[autoupdate] Could not close the pull request for %q in %s/%s, "+
					"keeping the branch so a later run can retry: %v",
				branch, repo.Organization, repo.Name, closeErr,
			)
			continue
		}

		if closed {
			logger.Infof("[autoupdate] Closed the pull request for the stale branch %q in %s/%s",
				branch, repo.Organization, repo.Name)
		}

		if deleteErr := batchCtx.DeleteBranch(branch, authMethods); deleteErr != nil {
			logger.Warnf("[autoupdate] Could not delete the stale branch %q in %s/%s: %v",
				branch, repo.Organization, repo.Name, deleteErr)
			continue
		}

		logger.Infof("[autoupdate] Deleted the stale branch %q in %s/%s",
			branch, repo.Organization, repo.Name)
	}
}
