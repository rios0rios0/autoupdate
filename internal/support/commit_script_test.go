package support_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/support"
)

func TestCommitAndPushScript(t *testing.T) {
	t.Parallel()

	commit := support.CommitAndPush{
		UpgradedWhen:   `[ "$GO_VERSION_CHANGED" = "true" ]`,
		UpgradeMessage: "chore(deps): upgraded Go version to `$GO_VERSION` and updated all dependencies",
		DepsMessage:    "chore(deps): update Go module dependencies",
	}

	t.Run("should stage, commit and push when the worktree changed", func(t *testing.T) {
		t.Parallel()

		// when
		script := support.CommitAndPushScript(commit)

		// then
		assert.Contains(t, script, `if [ -n "$(git status --porcelain)" ]; then`)
		assert.Contains(t, script, "    git add -A\n")
		assert.Contains(t, script, `    git push origin "$BRANCH_NAME" 2>&1`)
		assert.Contains(t, script, `    echo "CHANGES_PUSHED=true"`)
	})

	t.Run("should report nothing to push when the worktree is clean", func(t *testing.T) {
		t.Parallel()

		// when
		script := support.CommitAndPushScript(commit)

		// then
		assert.Contains(t, script, `    echo "No changes detected."`)
		assert.Contains(t, script, `    echo "CHANGES_PUSHED=false"`)
	})

	t.Run("should pick the subject with the condition and keep the version in it", func(t *testing.T) {
		t.Parallel()

		// when
		script := support.CommitAndPushScript(commit)

		// then
		assert.Contains(t, script, `    if [ "$GO_VERSION_CHANGED" = "true" ]; then`)
		assert.Contains(
			t,
			script,
			"        git commit -m \"chore(deps): upgraded Go version to \\`$GO_VERSION\\` and updated all dependencies\"",
		)
		assert.Contains(t, script, `        git commit -m "chore(deps): update Go module dependencies"`)
		assert.NotContains(t, script, "to `$GO_VERSION`")
	})

	t.Run("should place the guard between the change check and the staging when one is given", func(t *testing.T) {
		t.Parallel()

		// given
		guarded := commit
		guarded.Guard = "    # abandon a cosmetic change\n    exit 0\n"

		// when
		script := support.CommitAndPushScript(guarded)

		// then
		check := strings.Index(script, `if [ -n "$(git status --porcelain)" ]; then`)
		guard := strings.Index(script, "    # abandon a cosmetic change")
		staging := strings.Index(script, "    git add -A")
		assert.Less(t, check, guard)
		assert.Less(t, guard, staging)
	})

	t.Run("should emit no guard when none is given", func(t *testing.T) {
		t.Parallel()

		// when
		script := support.CommitAndPushScript(commit)

		// then
		assert.Contains(t, script, "then\n    echo \"Changes detected, committing and pushing...\"")
	})
}
