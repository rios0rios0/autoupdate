package support

import (
	"fmt"
	"strings"
)

// CommitAndPush parameterises CommitAndPushScript.
type CommitAndPush struct {
	// UpgradedWhen is the bash condition under which the run moved the
	// language version as well as the dependencies, and so takes
	// UpgradeMessage as its commit subject; for example
	// `[ "$GO_VERSION_CHANGED" = "true" ]`.
	UpgradedWhen string

	// UpgradeMessage is the commit subject of a run that moved the version. It
	// may reference the script's variables, which expand when it runs.
	UpgradeMessage string

	// DepsMessage is the commit subject when only the dependencies moved.
	DepsMessage string

	// Guard, when set, is emitted right after the change check and before
	// anything is staged, so a run can still abandon the commit. JavaScript
	// uses it to skip a lockfile that only synced its project version.
	Guard string
}

// CommitAndPushScript returns the closing block of every clone-based upgrade
// script: commit whatever the upgrade changed, under one of two subjects, push
// the branch, and report through CHANGES_PUSHED whether there was anything to
// push -- the marker the Go side reads to decide about the pull request.
//
// The subjects wrap the version in backticks, which bash reads as command
// substitution inside double quotes, so they are escaped here: unescaped, the
// version silently vanished from the subject. Doing it in one place is what
// keeps a seventh copy of the block from forgetting it.
func CommitAndPushScript(commit CommitAndPush) string {
	return fmt.Sprintf(`if [ -n "$(git status --porcelain)" ]; then
%s    echo "Changes detected, committing and pushing..."
    git add -A
    if %s; then
        git commit -m "%s"
    else
        git commit -m "%s"
    fi
    git push origin "$BRANCH_NAME" 2>&1
    echo "CHANGES_PUSHED=true"
else
    echo "No changes detected."
    echo "CHANGES_PUSHED=false"
fi
`,
		commit.Guard,
		commit.UpgradedWhen,
		escapeBackticks(commit.UpgradeMessage),
		escapeBackticks(commit.DepsMessage),
	)
}

// escapeBackticks makes a commit subject safe inside a double-quoted bash
// string.
func escapeBackticks(subject string) string {
	return strings.ReplaceAll(subject, "`", "\\`")
}
