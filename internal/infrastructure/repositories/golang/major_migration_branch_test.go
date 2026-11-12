package golang_test

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

// libMigrationBranch is the branch the fixture's finding is prepared on.
const libMigrationBranch = "chore/migrate-example-lib-v3"

// changelogMergedMeanwhile is the fixture's changelog once something was
// merged into the base after a checkout of it was taken.
const changelogMergedMeanwhile = `# Changelog

## [Unreleased]

### Fixed

- fixed something merged after the checkout was taken

## [1.0.0] - 2026-01-01

- Initial release
`

// migrationOrigin is a bare repository standing in for the provider's copy of
// the repository, and the checkout that publishes to it the way merges reach a
// base.
type migrationOrigin struct {
	bare string
	seed string
}

// newMigrationOrigin publishes the fixture as main on a bare repository.
func newMigrationOrigin(t *testing.T, files map[string]string) migrationOrigin {
	t.Helper()

	seed := newMigrationCheckout(t, files)
	bare := filepath.Join(t.TempDir(), "origin.git")
	runGit(t, seed, "init", "--quiet", "--bare", "--initial-branch=main", bare)
	runGit(t, seed, "remote", "add", "origin", bare)
	runGit(t, seed, "push", "--quiet", "origin", "main")

	return migrationOrigin{bare: bare, seed: seed}
}

// publishMigrationBranch cuts the fixture's migration branch from main as it
// stands, commits each message on it, publishes it and returns its tip.
func (o migrationOrigin) publishMigrationBranch(t *testing.T, messages ...string) string {
	t.Helper()

	runGit(t, o.seed, "checkout", "--quiet", "-b", libMigrationBranch, "main")
	for _, message := range messages {
		runGit(t, o.seed, "commit", "--quiet", "--allow-empty", "-m", message)
	}
	runGit(t, o.seed, "push", "--quiet", "origin", libMigrationBranch)
	tip := gitOutput(t, o.seed, "rev-parse", "HEAD")
	runGit(t, o.seed, "checkout", "--quiet", "main")

	return tip
}

// mergeIntoMain commits a change on main and publishes it, which is what every
// merge does to the base a migration branch was prepared on.
func (o migrationOrigin) mergeIntoMain(t *testing.T, files map[string]string) {
	t.Helper()

	for name, content := range files {
		writeTestFile(t, o.seed, name, content)
	}
	runGit(t, o.seed, "add", "-A")
	runGit(t, o.seed, "commit", "--quiet", "-m", "merged meanwhile")
	runGit(t, o.seed, "push", "--quiet", "origin", "main")
}

// clone checks the origin out the way a run's checkout is: with main tracked
// as origin/main, and none of the operator's hooks or signing settings.
func (o migrationOrigin) clone(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "checkout")
	noHooks := filepath.Join(t.TempDir(), "no-hooks")
	runGit(t, o.seed, "-c", "core.hooksPath="+noHooks, "clone", "--quiet", o.bare, dir)
	runGit(t, dir, "config", "user.email", "autoupdate@example.com")
	runGit(t, dir, "config", "user.name", "autoupdate")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	runGit(t, dir, "config", "core.hooksPath", noHooks)

	return dir
}

// migrationBranchTip returns the commit the origin holds under the fixture's
// migration branch, or nothing when it holds no such branch.
func (o migrationOrigin) migrationBranchTip(t *testing.T) string {
	t.Helper()

	return gitOutput(t, o.bare, "for-each-ref", "--format=%(objectname)", "refs/heads/"+libMigrationBranch)
}

// gitOutput runs one git command in dir and returns what it printed.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	output, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	require.NoError(t, err, "git %s", strings.Join(args, " "))

	return strings.TrimSpace(string(output))
}

// migrationCommit is the message the provider creates a migration branch
// with, which is how a later run recognises the branch as this tool's alone.
func migrationCommit() string {
	return golang.MigrationCommitMessage(libMigration(), true, "")
}

func TestMigrationsForRunKeepsTheBranchAheadOfItsBase(t *testing.T) {
	t.Parallel()

	t.Run("should rebuild a branch that fell behind its base and holds only the migration commit", func(t *testing.T) {
		t.Parallel()

		// given: a branch an earlier run prepared, on a base that has moved on
		// since. A pull request opened from it would carry everything merged
		// in between.
		origin := newMigrationOrigin(t, migrationCheckoutFixture())
		origin.publishMigrationBranch(t, migrationCommit())
		origin.mergeIntoMain(t, map[string]string{"CHANGELOG.md": changelogMergedMeanwhile})
		repoDir := origin.clone(t)
		provider := &repositorydoubles.SpyProviderRepository{ProviderName: "github"}
		runner := &stubGoRunner{}

		// when
		migrations := golang.MigrationsForRun(t.Context(), libMigrationParams(repoDir, provider, runner), true)

		// then
		require.Len(t, migrations, 1)
		assert.True(t, migrations[0].Prepared)
		assert.True(t, migrations[0].Compiles, "the rebuilt branch is one this run built")
		assert.Empty(t, migrations[0].Note, "a rebuilt branch is as good as a new one")
		assert.Empty(t, origin.migrationBranchTip(t),
			"the stale branch is deleted, since the provider can only create a branch")
		require.Len(t, provider.BranchInputs, 1, "and it is created again on top of the base")
		input := provider.BranchInputs[0]
		assert.Equal(t, libMigrationBranch, input.BranchName)
		assert.Equal(t, "refs/heads/main", input.BaseBranch)
		assert.Contains(t, pushedContents(input.Changes)["CHANGELOG.md"],
			"fixed something merged after the checkout was taken",
			"the migration is built from the base it is now proposed against")
	})

	t.Run("should rebuild a branch that carries nothing beyond its base", func(t *testing.T) {
		t.Parallel()

		cases := map[string]func(t *testing.T, origin migrationOrigin){
			"a branch pointing at the base itself": func(t *testing.T, origin migrationOrigin) {
				t.Helper()
				origin.publishMigrationBranch(t)
			},
			"a branch merged into the base and never deleted": func(t *testing.T, origin migrationOrigin) {
				t.Helper()
				origin.publishMigrationBranch(t, migrationCommit())
				runGit(t, origin.seed, "merge", "--quiet", "--no-ff", "--no-edit", libMigrationBranch)
				runGit(t, origin.seed, "push", "--quiet", "origin", "main")
			},
		}

		for name, publish := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				// given: a branch with no commit of its own, so it carries no
				// migration and nothing anybody wrote
				origin := newMigrationOrigin(t, migrationCheckoutFixture())
				publish(t, origin)
				repoDir := origin.clone(t)
				provider := &repositorydoubles.SpyProviderRepository{ProviderName: "github"}

				// when
				migrations := golang.MigrationsForRun(
					t.Context(), libMigrationParams(repoDir, provider, &stubGoRunner{}), true,
				)

				// then
				require.Len(t, migrations, 1)
				assert.True(t, migrations[0].Prepared)
				assert.True(t, migrations[0].Compiles)
				assert.Empty(t, origin.migrationBranchTip(t))
				assert.Len(t, provider.BranchInputs, 1)
			})
		}
	})

	t.Run("should not delete a stale branch while it cannot tell whether a pull request is open", func(t *testing.T) {
		t.Parallel()

		// given: a stale branch, and a provider that cannot answer whether a
		// pull request is open from it. Deleting the source of an open pull
		// request closes it, so the branch stays and the provider refuses to
		// create it again, as it would refuse any branch that is there.
		origin := newMigrationOrigin(t, migrationCheckoutFixture())
		tip := origin.publishMigrationBranch(t, migrationCommit())
		origin.mergeIntoMain(t, map[string]string{"CHANGELOG.md": changelogMergedMeanwhile})
		repoDir := origin.clone(t)
		provider := &repositorydoubles.SpyProviderRepository{
			ProviderName:    "github",
			PRExistsErr:     errors.New("provider unavailable"),
			CreateBranchErr: errors.New("422 Reference already exists []"),
		}

		// when
		migrations := golang.MigrationsForRun(
			t.Context(), libMigrationParams(repoDir, provider, &stubGoRunner{}), true,
		)

		// then
		require.Len(t, migrations, 1)
		assert.Equal(t, tip, origin.migrationBranchTip(t))
		assert.True(t, migrations[0].Prepared)
		assert.False(t, migrations[0].Compiles)
		assert.Contains(t, migrations[0].Note, "the branch already existed")
	})

	t.Run("should leave a branch that is up to date with its base without building anything", func(t *testing.T) {
		t.Parallel()

		// given: a branch prepared on top of the base as it still is
		origin := newMigrationOrigin(t, migrationCheckoutFixture())
		tip := origin.publishMigrationBranch(t, migrationCommit())
		repoDir := origin.clone(t)
		provider := &repositorydoubles.SpyProviderRepository{ProviderName: "github"}
		runner := &stubGoRunner{}

		// when
		migrations := golang.MigrationsForRun(t.Context(), libMigrationParams(repoDir, provider, runner), true)

		// then
		require.Len(t, migrations, 1)
		assert.True(t, migrations[0].Prepared, "the migration exists")
		assert.False(t, migrations[0].Compiles, "and this run did not build it")
		assert.Contains(t, migrations[0].Note, "up to date with `main`")
		assert.Equal(t, tip, origin.migrationBranchTip(t))
		assert.Empty(t, provider.BranchInputs)
		assert.Empty(t, runner.calls, "nothing is built for a branch that is left as it is")
	})

	t.Run("should leave a branch somebody has added their own work to", func(t *testing.T) {
		t.Parallel()

		cases := map[string][]string{
			"a commit on top of the migration": {
				migrationCommit(), "fix the calls the new major renamed",
			},
			"a migration somebody made by hand": {
				"refactor: moved to lib v3 by hand",
			},
		}

		for name, messages := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				// given: a branch behind its base, holding work this tool did
				// not do. Rebuilding it would throw that work away.
				origin := newMigrationOrigin(t, migrationCheckoutFixture())
				tip := origin.publishMigrationBranch(t, messages...)
				origin.mergeIntoMain(t, map[string]string{"CHANGELOG.md": changelogMergedMeanwhile})
				repoDir := origin.clone(t)
				provider := &repositorydoubles.SpyProviderRepository{ProviderName: "github"}
				runner := &stubGoRunner{}

				// when
				migrations := golang.MigrationsForRun(
					t.Context(), libMigrationParams(repoDir, provider, runner), true,
				)

				// then
				require.Len(t, migrations, 1)
				assert.True(t, migrations[0].Prepared)
				assert.False(t, migrations[0].Compiles)
				assert.Contains(t, migrations[0].Note,
					"the branch is behind `main` and has commits this tool did not make")
				assert.Equal(t, tip, origin.migrationBranchTip(t),
					"the branch is theirs to rebase")
				assert.Empty(t, provider.BranchInputs)
				assert.Empty(t, runner.calls)
			})
		}
	})

	t.Run("should leave the branch of an open pull request however far behind it is", func(t *testing.T) {
		t.Parallel()

		// given: somebody opened the pull request, so the migration is theirs
		origin := newMigrationOrigin(t, migrationCheckoutFixture())
		tip := origin.publishMigrationBranch(t, migrationCommit())
		origin.mergeIntoMain(t, map[string]string{"CHANGELOG.md": changelogMergedMeanwhile})
		repoDir := origin.clone(t)
		provider := &repositorydoubles.SpyProviderRepository{ProviderName: "github", PRExistsResult: true}

		// when
		migrations := golang.MigrationsForRun(
			t.Context(), libMigrationParams(repoDir, provider, &stubGoRunner{}), true,
		)

		// then
		require.Len(t, migrations, 1)
		assert.Contains(t, migrations[0].Note, "already open")
		assert.Equal(t, tip, origin.migrationBranchTip(t))
		assert.Empty(t, provider.BranchInputs)
	})

	t.Run("should report a stale branch the remote would not let it delete", func(t *testing.T) {
		t.Parallel()

		// given: a remote that refuses every branch deletion
		origin := newMigrationOrigin(t, migrationCheckoutFixture())
		tip := origin.publishMigrationBranch(t, migrationCommit())
		origin.mergeIntoMain(t, map[string]string{"CHANGELOG.md": changelogMergedMeanwhile})
		runGit(t, origin.bare, "config", "receive.denyDeletes", "true")
		repoDir := origin.clone(t)
		provider := &repositorydoubles.SpyProviderRepository{ProviderName: "github"}

		// when
		migrations := golang.MigrationsForRun(
			t.Context(), libMigrationParams(repoDir, provider, &stubGoRunner{}), true,
		)

		// then
		require.Len(t, migrations, 1)
		assert.True(t, migrations[0].Prepared, "the stale branch still carries the migration")
		assert.False(t, migrations[0].Compiles)
		assert.Contains(t, migrations[0].Note, "the branch is behind `main` and could not be replaced")
		assert.Equal(t, tip, origin.migrationBranchTip(t))
		assert.Empty(t, provider.BranchInputs, "nothing is created over a branch that is still there")
	})

	t.Run("should prepare a new branch from the base as the remote has it now", func(t *testing.T) {
		t.Parallel()

		// given: a checkout taken before something was merged into the base,
		// which is what a standalone run's checkout often is. The provider
		// commits on top of the base's current tip, so contents read from the
		// older copy would revert what was merged.
		origin := newMigrationOrigin(t, migrationCheckoutFixture())
		repoDir := origin.clone(t)
		origin.mergeIntoMain(t, map[string]string{"CHANGELOG.md": changelogMergedMeanwhile})
		provider := &repositorydoubles.SpyProviderRepository{ProviderName: "github"}

		// when
		migrations := golang.MigrationsForRun(
			t.Context(), libMigrationParams(repoDir, provider, &stubGoRunner{}), true,
		)

		// then
		require.Len(t, migrations, 1)
		assert.True(t, migrations[0].Prepared)
		require.Len(t, provider.BranchInputs, 1)
		changelog := pushedContents(provider.BranchInputs[0].Changes)["CHANGELOG.md"]
		assert.Contains(t, changelog, "fixed something merged after the checkout was taken")
		assert.Contains(t, changelog, "changed the imports and the module requirement")
	})
}

func TestDeleteStaleMigrationBranch(t *testing.T) {
	t.Parallel()

	t.Run("should delete the branch at the commit it was inspected at", func(t *testing.T) {
		t.Parallel()

		// given
		origin := newMigrationOrigin(t, migrationCheckoutFixture())
		tip := origin.publishMigrationBranch(t, migrationCommit())
		repoDir := origin.clone(t)

		// when
		err := golang.DeleteStaleMigrationBranch(t.Context(), repoDir, libMigrationBranch, tip)

		// then
		require.NoError(t, err)
		assert.Empty(t, origin.migrationBranchTip(t))
	})

	t.Run("should refuse to delete a branch somebody pushed to since it was inspected", func(t *testing.T) {
		t.Parallel()

		// given: the branch moved between the inspection and the deletion
		origin := newMigrationOrigin(t, migrationCheckoutFixture())
		inspected := origin.publishMigrationBranch(t, migrationCommit())
		repoDir := origin.clone(t)
		runGit(t, origin.seed, "checkout", "--quiet", libMigrationBranch)
		runGit(t, origin.seed, "commit", "--quiet", "--allow-empty", "-m", "fix the renamed calls")
		runGit(t, origin.seed, "push", "--quiet", "origin", libMigrationBranch)
		moved := gitOutput(t, origin.seed, "rev-parse", "HEAD")

		// when
		err := golang.DeleteStaleMigrationBranch(t.Context(), repoDir, libMigrationBranch, inspected)

		// then
		require.Error(t, err)
		assert.Equal(t, moved, origin.migrationBranchTip(t), "their commit is still there")
	})
}

func TestMigrationBranchListings(t *testing.T) {
	t.Parallel()

	t.Run("should read the tip of exactly the ref that was asked for", func(t *testing.T) {
		t.Parallel()

		// given: ls-remote matches a pattern against the tail of every ref
		sha := strings.Repeat("a", 40)
		other := strings.Repeat("b", 40)
		listing := other + "\trefs/remotes/fork/refs/heads/" + libMigrationBranch + "\n" +
			sha + "\trefs/heads/" + libMigrationBranch + "\n"

		// when
		tip := golang.RemoteRefTip(listing, "refs/heads/"+libMigrationBranch)

		// then
		assert.Equal(t, sha, tip)
	})

	t.Run("should refuse a commit name git would not have printed", func(t *testing.T) {
		t.Parallel()

		// given: whatever comes back from the remote is handed to git as an
		// argument, so only a commit name may make it that far
		listing := "--output=/tmp/x\trefs/heads/" + libMigrationBranch + "\n"

		// when
		tip := golang.RemoteRefTip(listing, "refs/heads/"+libMigrationBranch)

		// then
		assert.Empty(t, tip)
	})

	t.Run("should count every commit rev-list lists, an empty subject included", func(t *testing.T) {
		t.Parallel()

		// given
		first := strings.Repeat("c", 40)
		second := strings.Repeat("d", 64)
		listing := "commit " + first + "\n" +
			"chore(deps): prepare the migration of a to a/v2\n" +
			"commit " + second + "\n" +
			"\n"

		// when
		subjects := golang.RevListSubjects(listing)

		// then
		assert.Equal(t, []string{"chore(deps): prepare the migration of a to a/v2", ""}, subjects)
		assert.Empty(t, golang.RevListSubjects(""))
	})
}

// pushedContents maps every file the provider was handed to its content.
func pushedContents(changes []entities.FileChange) map[string]string {
	contents := make(map[string]string, len(changes))
	for _, change := range changes {
		contents[change.Path] = change.Content
	}

	return contents
}
