package golang_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

// migrationGoModFixture is the manifest of a repository that requires the
// dependency under migration.
const migrationGoModFixture = `module example.com/app

go 1.25

require github.com/example/lib v1.2.3
`

// migrationMainFixture imports the dependency and one package under it, which
// is what the rewrite is for.
const migrationMainFixture = `package main

import (
	"github.com/example/lib"
	"github.com/example/lib/pkg"
)

func main() {
	println(lib.Name(), pkg.Name())
}
`

// migrationChangelogFixture is where the migration records itself.
const migrationChangelogFixture = `# Changelog

## [Unreleased]

## [1.0.0] - 2026-01-01

- Initial release
`

// libMigration is one finding: a dependency that moved from an unsuffixed path
// to a /v3 one.
func libMigration() golang.MajorAvailable {
	return golang.MajorAvailable{
		ModuleDir:      ".",
		Path:           "github.com/example/lib",
		CurrentVersion: "v1.2.3",
		NextPath:       "github.com/example/lib/v3",
		NextVersion:    "v3.0.1",
	}
}

// migrationCheckoutFixture is the tree every flow test starts from.
func migrationCheckoutFixture() map[string]string {
	return map[string]string{
		"go.mod":       migrationGoModFixture,
		"main.go":      migrationMainFixture,
		"CHANGELOG.md": migrationChangelogFixture,
	}
}

// stubGoRunner stands in for the Go toolchain: it records what the preparation
// asked of it and answers the way a toolchain that succeeded would, so the flow
// is exercised without one being installed.
type stubGoRunner struct {
	calls  []stubGoCall
	failOn string
	output string
}

// stubGoCall is one command the preparation ran.
type stubGoCall struct {
	name string
	args []string
	dir  string
}

func (r *stubGoRunner) Run(
	_ context.Context, name string, args []string, opts cmdrunner.RunOptions,
) (*cmdrunner.RunResult, error) {
	r.calls = append(r.calls, stubGoCall{name: name, args: args, dir: opts.Dir})

	if r.failOn != "" && slices.Contains(args, r.failOn) {
		return &cmdrunner.RunResult{Output: r.output, ExitCode: 1},
			fmt.Errorf("command %s exited with code 1", name)
	}

	return &cmdrunner.RunResult{}, nil
}

// commands returns the commands the runner was asked for, in the shape the
// assertions read: "get github.com/example/lib/v3@v3.0.1".
func (r *stubGoRunner) commands() []string {
	commands := make([]string, 0, len(r.calls))
	for _, call := range r.calls {
		commands = append(commands, strings.Join(call.args, " "))
	}
	return commands
}

func TestMigrationsForRunPreparesABranch(t *testing.T) {
	t.Parallel()

	// given
	repoDir := newMigrationCheckout(t, migrationCheckoutFixture())
	toolingDir := t.TempDir()
	provider := &repositorydoubles.SpyProviderRepository{ProviderName: "github"}
	runner := &stubGoRunner{}

	// when
	migrations := golang.MigrationsForRun(context.Background(), golang.MigrationParams{
		RepoDir:      repoDir,
		ToolingDir:   toolingDir,
		BaseBranch:   "main",
		ProviderName: "github",
		Provider:     provider,
		Repo:         entities.Repository{Organization: "example", Name: "app", DefaultBranch: "main"},
		GoBinary:     "go",
		Majors:       []golang.MajorAvailable{libMigration()},
		Runner:       runner,
	}, true)

	// then
	require.Len(t, migrations, 1)
	prepared := migrations[0]
	assert.True(t, prepared.Prepared)
	assert.True(t, prepared.Compiles)
	assert.Equal(t, "chore/migrate-example-lib-v3", prepared.Branch)
	// The link is what the reader clicks, and it has to be the provider's own
	// "open a pull request" page for the branch that was just pushed.
	assert.Equal(t,
		"https://github.com/example/app/compare/main...chore/migrate-example-lib-v3?expand=1",
		prepared.URL)
	assert.Empty(t, prepared.Note)
	// The lookup comes first, so a run that repeats an earlier one costs a
	// lookup rather than a clone, a rewrite and a push.
	assert.Equal(t, []string{"chore/migrate-example-lib-v3"}, provider.PRExistsBranches)

	// What the provider was handed is the branch: imports rewritten, module
	// moved, and proposed against the branch the notice's pull request targets.
	require.Len(t, provider.BranchInputs, 1)
	input := provider.BranchInputs[0]
	assert.Equal(t, "chore/migrate-example-lib-v3", input.BranchName)
	assert.Equal(t, "refs/heads/main", input.BaseBranch)
	assert.Contains(
		t,
		input.CommitMessage,
		"prepare the migration of github.com/example/lib to github.com/example/lib/v3",
	)

	contents := map[string]string{}
	changeTypes := map[string]string{}
	for _, change := range input.Changes {
		contents[change.Path] = change.Content
		changeTypes[change.Path] = change.ChangeType
	}
	assert.Contains(t, contents["main.go"], `"github.com/example/lib/v3"`)
	assert.Contains(t, contents["main.go"], `"github.com/example/lib/v3/pkg"`)
	assert.Equal(t, "edit", changeTypes["main.go"])
	assert.Contains(t, contents["CHANGELOG.md"], "changed the imports and the module requirement")

	// The go commands ran in a worktree of the base branch rather than in the
	// checkout the run is assembling its own upgrade in.
	assert.Equal(t, []string{
		"get github.com/example/lib/v3@v3.0.1",
		"mod tidy",
		"vet ./...",
	}, runner.commands())
	for _, call := range runner.calls {
		assert.True(t,
			strings.HasPrefix(call.dir, toolingDir),
			"the module commands run in the worktree, not in %s", repoDir)
	}

	// A worktree is a registration in the checkout as well as a directory, so
	// leaving either behind costs the next run.
	entries, err := os.ReadDir(toolingDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "the worktree is removed once the branch is pushed")
}

func TestMigrationsForRunReportsWhatItCouldNotDo(t *testing.T) {
	t.Parallel()

	t.Run("should report a branch that is already there without claiming to have built it", func(t *testing.T) {
		t.Parallel()

		// given: the provider refuses a branch that exists, which is what a
		// second run of the day runs into.
		repoDir := newMigrationCheckout(t, migrationCheckoutFixture())
		provider := &repositorydoubles.SpyProviderRepository{
			ProviderName:    "github",
			CreateBranchErr: errors.New("reference already exists"),
		}

		// when
		migrations := golang.MigrationsForRun(context.Background(), golang.MigrationParams{
			RepoDir: repoDir, BaseBranch: "main", ProviderName: "github",
			Provider: provider, Repo: entities.Repository{Organization: "example", Name: "app"},
			GoBinary: "go", Majors: []golang.MajorAvailable{libMigration()},
			Runner: &stubGoRunner{},
		}, true)

		// then
		require.Len(t, migrations, 1)
		assert.True(t, migrations[0].Prepared, "the migration exists; this run just did not make it")
		assert.False(t, migrations[0].Compiles, "and saying it compiles would claim a build nobody ran")
		assert.Contains(t, migrations[0].Note, "the branch already existed")
	})

	t.Run("should report a migration that does not build", func(t *testing.T) {
		t.Parallel()

		// given: a toolchain that builds nothing. The API changes of a new
		// major are the normal reason, and they are not the bot's to make.
		repoDir := newMigrationCheckout(t, migrationCheckoutFixture())
		provider := &repositorydoubles.SpyProviderRepository{ProviderName: "github"}
		runner := &stubGoRunner{
			failOn: "vet",
			output: "main.go:9:2: undefined: lib.NewName\n",
		}

		// when
		migrations := golang.MigrationsForRun(context.Background(), golang.MigrationParams{
			RepoDir: repoDir, BaseBranch: "main", ProviderName: "github",
			Provider: provider, Repo: entities.Repository{Organization: "example", Name: "app"},
			GoBinary: "go", Majors: []golang.MajorAvailable{libMigration()}, Runner: runner,
		}, true)

		// then
		require.Len(t, migrations, 1)
		assert.True(t, migrations[0].Prepared, "the branch is worth having even when it needs work")
		assert.False(t, migrations[0].Compiles)
		assert.Contains(t, migrations[0].Note, "it does not build yet")
		assert.Contains(t, migrations[0].Note, "undefined: lib.NewName",
			"the reader is told what the new major's API changed")
		assert.Contains(t, provider.BranchInputs[0].CommitMessage, "It does not build yet.")
	})

	t.Run("should decline a module the repository redirects elsewhere", func(t *testing.T) {
		t.Parallel()

		// given: a go.mod that replaces the dependency with a fork or a local
		// checkout. Rewriting its imports would point the build at the
		// published module the repository deliberately does not use.
		files := migrationCheckoutFixture()
		files["go.mod"] = migrationGoModFixture +
			"\nreplace github.com/example/lib => github.com/example/fork v1.2.3\n"

		repoDir := newMigrationCheckout(t, files)
		provider := &repositorydoubles.SpyProviderRepository{ProviderName: "github"}

		// when
		migrations := golang.MigrationsForRun(context.Background(), golang.MigrationParams{
			RepoDir: repoDir, BaseBranch: "main", ProviderName: "github",
			Provider: provider, Repo: entities.Repository{Organization: "example", Name: "app"},
			GoBinary: "go", Majors: []golang.MajorAvailable{libMigration()},
			Runner: &stubGoRunner{},
		}, true)

		// then
		require.Len(t, migrations, 1)
		assert.False(t, migrations[0].Prepared)
		assert.Contains(t, migrations[0].Note, "go.mod")
		assert.Contains(t, migrations[0].Note, "redirects it elsewhere")
		assert.Empty(t, provider.BranchInputs, "nothing is pushed in that case")
	})

	t.Run("should report a dependency nothing in the repository uses", func(t *testing.T) {
		t.Parallel()

		// given: a module that requires neither the dependency nor anything
		// importing it, which is what a stale finding looks like.
		repoDir := newMigrationCheckout(t, map[string]string{
			"go.mod":  "module example.com/app\n\ngo 1.25\n",
			"main.go": "package main\n\nfunc main() {}\n",
		})
		provider := &repositorydoubles.SpyProviderRepository{ProviderName: "github"}

		// when
		migrations := golang.MigrationsForRun(context.Background(), golang.MigrationParams{
			RepoDir: repoDir, BaseBranch: "main", ProviderName: "github",
			Provider: provider, Repo: entities.Repository{Organization: "example", Name: "app"},
			GoBinary: "go", Majors: []golang.MajorAvailable{libMigration()},
			Runner: &stubGoRunner{},
		}, true)

		// then
		require.Len(t, migrations, 1)
		assert.False(t, migrations[0].Prepared)
		assert.Contains(t, migrations[0].Note, "no module in this repository requires or imports it")
	})

	t.Run("should not look at a repository whose pull request is already open", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := newMigrationCheckout(t, migrationCheckoutFixture())
		provider := &repositorydoubles.SpyProviderRepository{
			ProviderName:   "github",
			PRExistsResult: true,
		}
		runner := &stubGoRunner{}

		// when
		migrations := golang.MigrationsForRun(context.Background(), golang.MigrationParams{
			RepoDir: repoDir, BaseBranch: "main", ProviderName: "github",
			Provider: provider, Repo: entities.Repository{Organization: "example", Name: "app"},
			GoBinary: "go", Majors: []golang.MajorAvailable{libMigration()}, Runner: runner,
		}, true)

		// then
		require.Len(t, migrations, 1)
		assert.True(t, migrations[0].Prepared)
		assert.Contains(t, migrations[0].Note, "already open")
		assert.Empty(t, runner.calls, "the work is already done; this run does none of it")
		assert.Empty(t, provider.BranchInputs)
	})

	t.Run("should prepare nothing on a rehearsal", func(t *testing.T) {
		t.Parallel()

		// given: the notice is part of what a dry run would produce, but a
		// branch on the remote is not a rehearsal.
		repoDir := newMigrationCheckout(t, migrationCheckoutFixture())
		provider := &repositorydoubles.SpyProviderRepository{ProviderName: "github"}
		runner := &stubGoRunner{}

		// when
		migrations := golang.MigrationsForRun(context.Background(), golang.MigrationParams{
			RepoDir: repoDir, BaseBranch: "main", ProviderName: "github",
			Provider: provider, Repo: entities.Repository{Organization: "example", Name: "app"},
			GoBinary: "go", Majors: []golang.MajorAvailable{libMigration()},
			Runner: runner, DryRun: true,
		}, true)

		// then
		require.Len(t, migrations, 1)
		assert.False(t, migrations[0].Prepared)
		assert.Empty(t, runner.calls)
		assert.Empty(t, provider.PRExistsBranches)
		assert.Empty(t, provider.BranchInputs)
	})

	t.Run("should report the findings without a migration when the preparation is off", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := newMigrationCheckout(t, migrationCheckoutFixture())
		provider := &repositorydoubles.SpyProviderRepository{ProviderName: "github"}

		// when
		migrations := golang.MigrationsForRun(context.Background(), golang.MigrationParams{
			RepoDir: repoDir, BaseBranch: "main", ProviderName: "github",
			Provider: provider, Repo: entities.Repository{Organization: "example", Name: "app"},
			GoBinary: "go", Majors: []golang.MajorAvailable{libMigration()},
			Runner: &stubGoRunner{},
		}, false)

		// then
		require.Len(t, migrations, 1, "the notice still travels; only the branch is absent")
		assert.False(t, migrations[0].Prepared)
		assert.Empty(t, provider.PRExistsBranches)
	})

	t.Run("should report the findings without a migration when there is no provider API", func(t *testing.T) {
		t.Parallel()

		// given: the flow that cannot open pull requests has nothing to push a
		// branch through.
		repoDir := newMigrationCheckout(t, migrationCheckoutFixture())

		// when
		migrations := golang.MigrationsForRun(context.Background(), golang.MigrationParams{
			RepoDir: repoDir, BaseBranch: "main", ProviderName: "github",
			Repo:     entities.Repository{Organization: "example", Name: "app"},
			GoBinary: "go", Majors: []golang.MajorAvailable{libMigration()},
			Runner: &stubGoRunner{},
		}, true)

		// then
		require.Len(t, migrations, 1)
		assert.False(t, migrations[0].Prepared)
	})
}

func TestPrepareLocalMigrations(t *testing.T) {
	t.Parallel()

	// given: the output of a local upgrade, whose script reports the newer
	// majors the same way the batch one does.
	repoDir := newMigrationCheckout(t, migrationCheckoutFixture())
	provider := &repositorydoubles.SpyProviderRepository{ProviderName: "github"}
	runner := &stubGoRunner{}
	defer golang.SetLocalCmdRunner(runner)()

	output := "Checking for newer majors published under a different path...\n" +
		"GO_MAJOR_AVAILABLE=.|github.com/example/lib|v1.2.3|github.com/example/lib/v3|v3.0.1\n"

	// when
	migrations := golang.PrepareLocalMigrations(context.Background(), repoDir, golang.LocalUpgradeOptions{
		ProviderName: "github",
		Repository:   entities.Repository{Organization: "example", Name: "app", DefaultBranch: "main"},
		Provider:     provider,

		PrepareMajorMigrations: true,
	}, output, "main")

	// then
	require.Len(t, migrations, 1)
	assert.True(t, migrations[0].Prepared)
	assert.Equal(t, "chore/migrate-example-lib-v3", migrations[0].Branch)
	require.Len(t, provider.BranchInputs, 1)
	assert.Contains(t, provider.BranchInputs[0].CommitMessage, "prepare the migration of github.com/example/lib")
	assert.Equal(t, []string{"get github.com/example/lib/v3@v3.0.1", "mod tidy", "vet ./..."},
		runner.commands())
}

func TestMigrationPreparationEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("should report empty findings without touching the provider", func(t *testing.T) {
		t.Parallel()

		// given
		provider := &repositorydoubles.SpyProviderRepository{ProviderName: "github"}

		// when
		migrations := golang.MigrationsForRun(t.Context(), golang.MigrationParams{
			Provider: provider,
		}, true)

		// then
		assert.Empty(t, migrations)
	})

	t.Run("should report missing runner and provider without preparing", func(t *testing.T) {
		t.Parallel()

		// given
		major := []golang.MajorAvailable{libMigration()}

		// when
		withoutProvider := golang.MigrationsForRun(t.Context(), golang.MigrationParams{Majors: major}, true)
		withoutRunner := golang.MigrationsForRun(t.Context(), golang.MigrationParams{
			Provider: &repositorydoubles.SpyProviderRepository{ProviderName: "github"},
			Majors:   major,
		}, true)

		// then
		assert.False(t, withoutProvider[0].Prepared)
		assert.False(t, withoutRunner[0].Prepared)
	})

	t.Run("should report a provider branch creation failure", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := newMigrationCheckout(t, migrationCheckoutFixture())
		provider := &repositorydoubles.SpyProviderRepository{
			ProviderName:    "github",
			CreateBranchErr: errors.New("provider unavailable"),
		}

		// when
		migrations := golang.MigrationsForRun(t.Context(), golang.MigrationParams{
			RepoDir: repoDir, BaseBranch: "main", ProviderName: "github",
			Provider: provider, Repo: entities.Repository{Organization: "example", Name: "app"},
			GoBinary: "go", Majors: []golang.MajorAvailable{libMigration()}, Runner: &stubGoRunner{},
		}, true)

		// then
		assert.False(t, migrations[0].Prepared)
		assert.Contains(t, migrations[0].Note, "provider unavailable")
	})

	t.Run("should report a migration command failure", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := newMigrationCheckout(t, migrationCheckoutFixture())
		provider := &repositorydoubles.SpyProviderRepository{ProviderName: "github"}
		runner := &stubGoRunner{failOn: "get", output: "token leaked in output"}

		// when
		migrations := golang.MigrationsForRun(t.Context(), golang.MigrationParams{
			RepoDir: repoDir, BaseBranch: "main", ProviderName: "github",
			Provider: provider, Repo: entities.Repository{Organization: "example", Name: "app"},
			AuthToken: "token", GoBinary: "go", Majors: []golang.MajorAvailable{libMigration()}, Runner: runner,
		}, true)

		// then
		assert.False(t, migrations[0].Prepared)
		assert.Contains(t, migrations[0].Note, "go get")
		assert.NotContains(t, migrations[0].Note, "token")
	})
}

func TestMigrationWorktreeAndRunnerEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("should use the origin remote ref when the local base ref is absent", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := newMigrationCheckout(t, migrationCheckoutFixture())
		runGit(t, repoDir, "branch", "-m", "main", "trunk")
		runGit(t, repoDir, "update-ref", "refs/remotes/origin/main", "HEAD")

		// when
		worktree, remove, err := golang.MigrationWorktree(t.Context(), golang.MigrationParams{
			RepoDir: repoDir, ToolingDir: t.TempDir(), BaseBranch: "main",
		})

		// then
		require.NoError(t, err)
		assert.DirExists(t, worktree)
		remove()
	})

	t.Run("should quote config paths and report command errors", func(t *testing.T) {
		t.Parallel()

		// given
		runner := &stubGoRunner{failOn: "vet", output: "secret output\n"}
		params := golang.MigrationParams{GoBinary: "go", AuthToken: "secret", Runner: runner}

		// when
		err := golang.MigrationRun(t.Context(), params, t.TempDir(), "vet", "./...")

		// then
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "secret")
		assert.Equal(t, "'path'\\''with'", golang.ShellQuote("path'with"))
	})

	t.Run("should ignore invalid edit ranges", func(t *testing.T) {
		t.Parallel()

		// given / when
		result := golang.ApplyEdits([]byte("abc"), []golang.SourceEdit{
			{Start: -1, End: 1, Text: "x"},
			{Start: 0, End: 9, Text: "y"},
		})

		// then
		assert.Equal(t, "abc", string(result))
	})
}

func TestMigrationWorktreePreparationDetails(t *testing.T) {
	t.Parallel()

	t.Run("should report an unavailable worktree base", func(t *testing.T) {
		t.Parallel()

		// given
		params := golang.MigrationParams{RepoDir: t.TempDir(), ToolingDir: t.TempDir(), BaseBranch: "main"}

		// when
		_, _, err := golang.MigrationWorktree(t.Context(), params)

		// then
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to check")
	})

	t.Run("should report a missing Go runner before changing the worktree", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := newMigrationCheckout(t, migrationCheckoutFixture())
		worktree, remove, err := golang.MigrationWorktree(t.Context(), golang.MigrationParams{
			RepoDir: repoDir, ToolingDir: t.TempDir(), BaseBranch: "main",
		})
		require.NoError(t, err)
		defer remove()

		// when
		build, prepareErr := golang.PrepareMigrationInWorktree(t.Context(), golang.MigrationParams{
			GoBinary: "go", Runner: nil,
		}, libMigration(), worktree, nil)

		// then
		assert.False(t, build.Compiles)
		assert.EqualError(t, prepareErr, "go command runner is not configured")
	})

	t.Run("should source config.sh and vendor a touched module", func(t *testing.T) {
		t.Parallel()

		// given
		files := migrationCheckoutFixture()
		files["config.sh"] = "export GOPRIVATE=example.com\n"
		files["vendor/.keep"] = ""
		repoDir := newMigrationCheckout(t, files)
		worktree, remove, err := golang.MigrationWorktree(t.Context(), golang.MigrationParams{
			RepoDir: repoDir, ToolingDir: t.TempDir(), BaseBranch: "main",
		})
		require.NoError(t, err)
		defer remove()
		runner := &stubGoRunner{}

		// when
		build, prepareErr := golang.PrepareMigrationInWorktree(t.Context(), golang.MigrationParams{
			GoBinary: "go", Runner: runner, HasConfigSH: true,
		}, libMigration(), worktree, nil)

		// then
		require.NoError(t, prepareErr)
		assert.True(t, build.Compiles)
		assert.Equal(t, "bash", runner.calls[0].name)
		assert.Contains(t, strings.Join(runner.calls[0].args, " "), "source")
		assert.Contains(t, strings.Join(runner.commands(), " | "), "go mod vendor")
	})

	t.Run("should ignore an unreadable nested module and a non-matching module", func(t *testing.T) {
		t.Parallel()

		// given
		files := migrationCheckoutFixture()
		files["tools/go.mod"] = "not a go module"
		files["other/go.mod"] = "module example.com/other\n\ngo 1.25\n"
		repoDir := newMigrationCheckout(t, files)
		worktree, remove, err := golang.MigrationWorktree(t.Context(), golang.MigrationParams{
			RepoDir: repoDir, ToolingDir: t.TempDir(), BaseBranch: "main",
		})
		require.NoError(t, err)
		defer remove()

		// when
		build, prepareErr := golang.PrepareMigrationInWorktree(t.Context(), golang.MigrationParams{
			GoBinary: "go", Runner: &stubGoRunner{},
		}, libMigration(), worktree, nil)

		// then
		require.NoError(t, prepareErr)
		assert.True(t, build.Compiles)
	})

	t.Run("should run commands only in the nearest nested module", func(t *testing.T) {
		t.Parallel()

		// given
		files := migrationCheckoutFixture()
		files["go.mod"] = "module example.com/app\n\ngo 1.25\n"
		files["main.go"] = "package main\n\nfunc main() {}\n"
		files["tools/go.mod"] = migrationGoModFixture
		files["tools/main.go"] = migrationMainFixture
		repoDir := newMigrationCheckout(t, files)
		worktree, remove, err := golang.MigrationWorktree(t.Context(), golang.MigrationParams{
			RepoDir: repoDir, ToolingDir: t.TempDir(), BaseBranch: "main",
		})
		require.NoError(t, err)
		defer remove()
		runner := &stubGoRunner{}

		// when
		_, prepareErr := golang.PrepareMigrationInWorktree(t.Context(), golang.MigrationParams{
			GoBinary: "go", Runner: runner,
		}, libMigration(), worktree, nil)

		// then
		require.NoError(t, prepareErr)
		for _, call := range runner.calls {
			assert.Contains(t, call.dir, worktree,
				"the command runs in the migration worktree")
		}
		assert.NotEmpty(t, runner.calls)
		assert.Contains(t, runner.calls[0].dir, "tools",
			"commands should be scoped to the owning nested module")
	})
}

func TestMigrationCleanupAndCollectionErrors(t *testing.T) {
	t.Parallel()

	// given
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "leftover"), []byte("x"), 0o600)

	// when
	golang.RemoveMigrationWorktree(t.Context(), filepath.Join(dir, "not-a-repo"), dir)
	_, _, collectErr := golang.CollectMigrationChanges(t.Context(), t.TempDir())

	// then
	assert.Error(t, collectErr, "a non-git worktree cannot provide migration changes")
}
