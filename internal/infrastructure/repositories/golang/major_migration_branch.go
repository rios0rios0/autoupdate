package golang

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/support"
)

const (
	// migrationRemote is the remote the run's checkout came from, which is
	// where the provider publishes every prepared branch.
	migrationRemote = "origin"

	// migrationRemoteTimeout bounds one git command that talks to the remote.
	// Looking at a branch is housekeeping ahead of the real work, so a remote
	// that stops answering degrades the run to what it did before it looked,
	// rather than stalling it.
	migrationRemoteTimeout = 2 * time.Minute

	// gitInterruptGrace is how long a git command that was interrupted is
	// given to clean up after itself before it is killed.
	gitInterruptGrace = 10 * time.Second

	// lsRemoteNoMatch is the status `git ls-remote --exit-code` exits with when
	// the remote has no ref of that name: an answer rather than a failure.
	lsRemoteNoMatch = 2

	// notAnAncestor is the status `git merge-base --is-ancestor` exits with
	// when the first commit is not an ancestor of the second.
	notAnAncestor = 1

	// noExitStatus stands for a git command that never exited with a status of
	// its own: it could not be started, or a signal stopped it.
	noExitStatus = -1

	// rebuildableCommits is how many commits a branch may carry beyond its base
	// and still be this tool's alone: the migration commit the provider made.
	rebuildableCommits = 1

	// rebuildProbe is how many commits are listed to find that out. One more
	// than a rebuildable branch can hold is enough to tell, however many more
	// somebody added.
	rebuildProbe = rebuildableCommits + 1
)

// objectNamePattern matches a commit the way git names one, under SHA-1 or
// SHA-256. A ref's commit is read from what the remote answers, and only a name
// of this shape is ever handed back to git as an argument.
var objectNamePattern = regexp.MustCompile(`^[0-9a-f]{40}(?:[0-9a-f]{24})?$`)

// revListCommitHeader matches the line `git rev-list --format` opens each
// commit with, which is how its output is split back into commits.
var revListCommitHeader = regexp.MustCompile(`^commit [0-9a-f]{40}(?:[0-9a-f]{24})?$`)

// migrationBranchState is what the remote already holds under a migration's
// branch name, which decides what the run does about it.
type migrationBranchState int

const (
	// migrationBranchAbsent means there is no such branch, so it is created.
	migrationBranchAbsent migrationBranchState = iota
	// migrationBranchCurrent means the branch already contains the tip of its
	// base. It is left as it is until the base moves on, which is when it is
	// rebuilt -- and when a newer release of the same major is picked up.
	migrationBranchCurrent
	// migrationBranchStale means the branch is behind its base and carries
	// nothing but the commit this tool had the provider make -- or no commit
	// beyond the base at all. It is deleted and prepared again on top of the
	// base.
	migrationBranchStale
	// migrationBranchClaimed means the branch is behind its base and carries
	// commits this tool did not make: somebody is finishing the migration on
	// it, and it is theirs to rebase. It is left exactly as it is.
	migrationBranchClaimed
	// migrationBranchUnknown means the remote could not be asked. The run
	// prepares the branch as though it were absent and lets the provider's
	// refusal say otherwise, which is all it did before it looked.
	migrationBranchUnknown
)

// existingMigrationBranch is what the remote holds under a migration's branch
// name.
type existingMigrationBranch struct {
	state migrationBranchState
	// tip is the commit the branch pointed at when it was looked at. Deleting
	// the branch is leased on it, so a branch somebody pushed to since is
	// never deleted.
	tip string
}

// refreshMigrationBase fetches the base branch into the checkout's
// remote-tracking ref, which is what every migration worktree is checked out
// from.
//
// The provider commits a prepared branch on top of the base's tip as the remote
// has it at that moment, but every file it commits is read from the worktree.
// Prepared from an older copy of the base, the migration would carry that
// copy's version of every file it touched, and the branch would quietly revert
// whatever was merged in between -- the opposite of a branch ahead of its base.
// A batch run's clone is minutes old; a standalone run's checkout can be weeks
// old. A fetch that fails leaves the checkout's own copy in place, which is
// what every run used before this one fetched at all.
func refreshMigrationBase(ctx context.Context, params migrationParams, env []string) {
	base := baseBranchName(params.BaseBranch)
	if base == "" {
		return
	}

	refspec := "+" + refsHeads(base) + ":refs/remotes/" + migrationRemote + "/" + base
	_, _, err := remoteGit(ctx, params.RepoDir, env, params.AuthToken,
		"fetch", "--quiet", "--no-tags", "--no-recurse-submodules", migrationRemote, refspec,
	)
	if err != nil {
		logger.Warnf(
			"[golang] Could not fetch %s before preparing the migrations, "+
				"so they are prepared from the checkout's copy of it: %v",
			base, err,
		)
	}
}

// inspectMigrationBranch reports what the remote holds under the migration's
// branch name, measured against the base the worktree has checked out.
//
// A prepared branch is ahead of its base only on the day it is created: the
// provider commits it on top of the base's tip, and the base moves on with
// every merge after that. Left alone, a branch prepared weeks ago keeps the
// migration on top of a base that no longer exists, and a pull request opened
// from it drags along everything the base has changed since. So the branch is
// looked at before anything is prepared, the way the upgrade's own branch is
// recreated on top of the base on every run rather than reused.
//
// The branch is fetched into the worktree's own FETCH_HEAD and into no ref at
// all, so looking leaves the checkout's references -- the operator's own, in
// standalone mode -- exactly as they were.
func inspectMigrationBranch(
	ctx context.Context, worktree, branch string, env []string, token string,
) existingMigrationBranch {
	ref := refsHeads(branch)

	listing, status, err := remoteGit(ctx, worktree, env, token,
		"ls-remote", "--exit-code", migrationRemote, ref,
	)
	if status == lsRemoteNoMatch {
		return existingMigrationBranch{state: migrationBranchAbsent, tip: ""}
	}
	if err != nil {
		return unknownMigrationBranch(branch, "look the branch up", err)
	}

	tip := remoteRefTip(listing, ref)
	if tip == "" {
		// The pattern matches the tail of a ref name, so a ref that merely ends
		// in this one's name answers as well; none of them is the branch.
		return existingMigrationBranch{state: migrationBranchAbsent, tip: ""}
	}

	_, _, err = remoteGit(ctx, worktree, env, token,
		"fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--refmap=", migrationRemote, ref,
	)
	if err != nil {
		return unknownMigrationBranch(branch, "fetch the branch", err)
	}

	beyond, err := commitsBeyondBase(ctx, worktree, tip, env, token)
	if err != nil {
		return unknownMigrationBranch(branch, "read the commits on the branch", err)
	}
	if len(beyond) == 0 {
		// Everything on the branch is in the base already -- a branch merged
		// and never deleted, or one pointing at the base itself -- so it
		// carries no migration, and rebuilding it loses nothing.
		return existingMigrationBranch{state: migrationBranchStale, tip: tip}
	}

	_, status, err = migrationGit(ctx, worktree, env, token, "merge-base", "--is-ancestor", "HEAD", tip)
	if err == nil {
		return existingMigrationBranch{state: migrationBranchCurrent, tip: tip}
	}
	if status != notAnAncestor {
		return unknownMigrationBranch(branch, "compare the branch with its base", err)
	}
	if !holdsOnlyMigrationCommit(beyond) {
		return existingMigrationBranch{state: migrationBranchClaimed, tip: tip}
	}

	return existingMigrationBranch{state: migrationBranchStale, tip: tip}
}

// unknownMigrationBranch is what a branch the remote could not be asked about
// is taken to be.
func unknownMigrationBranch(branch, attempt string, err error) existingMigrationBranch {
	logger.Warnf(
		"[golang] Could not %s for the migration branch %s, so it is prepared as though it were not there: %v",
		attempt, branch, err,
	)

	return existingMigrationBranch{state: migrationBranchUnknown, tip: ""}
}

// remoteRefTip returns the commit `git ls-remote` reports for exactly the given
// ref, or nothing.
func remoteRefTip(listing, ref string) string {
	for line := range strings.SplitSeq(listing, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == ref && objectNamePattern.MatchString(fields[0]) {
			return fields[0]
		}
	}

	return ""
}

// commitsBeyondBase returns the subjects of the commits the branch has and the
// base does not, newest first -- at most rebuildProbe of them, which is as many
// as it takes to tell whether the branch is this tool's alone.
func commitsBeyondBase(
	ctx context.Context, worktree, tip string, env []string, token string,
) ([]string, error) {
	listing, _, err := migrationGit(ctx, worktree, env, token,
		"rev-list", "--max-count="+strconv.Itoa(rebuildProbe), "--format=%s", "HEAD.."+tip,
	)
	if err != nil {
		return nil, err
	}

	return revListSubjects(listing), nil
}

// holdsOnlyMigrationCommit reports whether the commits a branch carries beyond
// its base are the one commit this tool had the provider make -- so that
// deleting the branch loses nothing anybody wrote.
//
// It is deliberately narrow. A second commit, a merge, or a single commit under
// a subject of somebody else's is a person finishing the migration, and a
// branch with their work on it is theirs to rebase. Output this cannot read
// errs the same way: a branch left behind costs a rebase, and a branch deleted
// from under somebody costs their work.
//
// The one case it cannot tell apart is the migration commit amended in place
// and pushed back under its own subject, which reads as untouched.
func holdsOnlyMigrationCommit(subjects []string) bool {
	if len(subjects) == 0 || len(subjects) > rebuildableCommits {
		return false
	}
	for _, subject := range subjects {
		if !strings.HasPrefix(subject, migrationCommitSubjectPrefix) {
			return false
		}
	}

	return true
}

// revListSubjects splits the output of `git rev-list --format=%s` into one
// subject per commit. A commit whose subject is empty still counts as one.
func revListSubjects(listing string) []string {
	subjects := make([]string, 0, rebuildProbe)
	awaitingSubject := false

	for line := range strings.SplitSeq(strings.TrimSuffix(listing, "\n"), "\n") {
		switch {
		case revListCommitHeader.MatchString(line):
			subjects = append(subjects, "")
			awaitingSubject = true
		case awaitingSubject:
			subjects[len(subjects)-1] = line
			awaitingSubject = false
		}
	}

	return subjects
}

// deleteStaleMigrationBranch deletes a branch the inspection found stale, so
// that the provider can create it again on top of the base: the provider APIs
// create a branch, and none of them moves one.
//
// The deletion is leased on the commit the inspection read. A branch somebody
// pushed to since is no longer stale, and the remote refuses the push rather
// than taking their commit with it. The pre-push hook is skipped, as it is on
// every push this tool makes through go-git: it checks the commits a push
// publishes, and a deletion publishes none.
func deleteStaleMigrationBranch(
	ctx context.Context, worktree, branch, tip string, env []string, token string,
) error {
	ref := refsHeads(branch)

	_, _, err := remoteGit(ctx, worktree, env, token,
		"push", "--quiet", "--no-verify", "--force-with-lease="+ref+":"+tip, migrationRemote, ":"+ref,
	)

	return err
}

// pullRequestOpenFrom asks the provider whether a pull request is open from the
// branch.
//
// Unlike support.PullRequestOpen it hands the error back. A failed check is
// still no reason to skip preparing the migration, but it is a reason not to
// delete the branch: deleting the source branch of an open pull request closes
// it on GitHub and strands it on Azure DevOps, so a branch is only ever deleted
// once the provider has said that no pull request is open from it.
func pullRequestOpenFrom(ctx context.Context, params migrationParams, branch string) (bool, error) {
	open, err := params.Provider.PullRequestExists(ctx, params.Repo, branch)
	if err != nil {
		logger.Warnf("[golang] Could not check for a pull request open from %s: %v", branch, err)
		return false, err
	}
	if open {
		logger.Infof("[golang] A pull request is already open from %s, so it is left as it is", branch)
	}

	return open, nil
}

// keptBranchNote says why the branch already on the remote is left as it is,
// or nothing when the run goes on to prepare it.
//
// Every state is named rather than left to a default, so that a state added
// later has to decide, here, whether it leaves the branch alone.
func keptBranchNote(state migrationBranchState, base string) string {
	switch state {
	case migrationBranchCurrent:
		return "the branch already existed and is up to date with `" + base + "`, so this run did not rebuild it"
	case migrationBranchClaimed:
		return "the branch is behind `" + base + "` and has commits this tool did not make, " +
			"so it was left as it is"
	case migrationBranchAbsent, migrationBranchStale, migrationBranchUnknown:
		// The run goes on to prepare the branch.
	}

	return ""
}

// staleBranchNote is what the row says about a branch that fell behind its
// base and could not be deleted to make way for the rebuilt one.
func staleBranchNote(base string, err error) string {
	return "the branch is behind `" + base + "` and could not be replaced: " + err.Error()
}

// remoteGit is migrationGit for a command that talks to the remote, bounded by
// migrationRemoteTimeout.
func remoteGit(
	ctx context.Context, dir string, env []string, token string, args ...string,
) (string, int, error) {
	remoteCtx, cancel := context.WithTimeout(ctx, migrationRemoteTimeout)
	defer cancel()

	return migrationGit(remoteCtx, dir, env, token, args...)
}

// migrationGit runs one git command in dir and returns what it printed on its
// standard output, together with the status it exited with.
//
// A command cut short -- by migrationRemoteTimeout, or by the run being
// cancelled -- is interrupted rather than killed, because git removes the lock
// files it holds when it is interrupted and leaves them behind when it is
// killed, and in standalone mode those are in the operator's own checkout. It
// is killed only if it has not stopped within gitInterruptGrace.
//
// The error carries what git printed on its standard error, scrubbed of the
// token: a remote URL with the credential in it is exactly what a failed fetch
// or push echoes back, and the error ends up in a note a reader sees.
func migrationGit(
	ctx context.Context, dir string, env []string, token string, args ...string,
) (string, int, error) {
	cmd := support.GitCommand(ctx, dir, args...)
	if env != nil {
		cmd.Env = env
	}
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = gitInterruptGrace
	var stderr strings.Builder
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if err == nil {
		return string(output), 0, nil
	}

	status := noExitStatus
	if exitErr, isExit := errors.AsType[*exec.ExitError](err); isExit {
		status = exitErr.ExitCode()
	}

	detail := collapseWhitespace(support.RedactTokens(stderr.String(), token))
	if detail == "" {
		detail = support.RedactTokens(err.Error(), token)
	}

	return string(output), status, fmt.Errorf("git %s failed: %s", args[0], detail)
}
