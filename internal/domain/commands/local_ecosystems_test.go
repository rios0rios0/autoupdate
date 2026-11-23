package commands_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	domainRepos "github.com/rios0rios0/autoupdate/internal/domain/repositories"
	infraRepos "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlocal"
	"github.com/rios0rios0/autoupdate/test/infrastructure/gitrepo"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
	langEntities "github.com/rios0rios0/langforge/pkg/domain/entities"
)

// ecosystemOrigin is the origin of the checkouts in these tests. It is fetched over SSH, so no
// token reaches git, and pushed to a bare repository on disk (see newEcosystemCheckout).
const ecosystemOrigin = "git@github.com:example-org/infra-repo.git"

func TestLocalUpdatersFor(t *testing.T) {
	t.Parallel()

	t.Run("should find every ecosystem the checkout holds on disk, below the root too", func(t *testing.T) {
		t.Parallel()

		// given -- Terraform kept in a stack directory and a Go module of tests beside it,
		// neither of them at the root, the way an infrastructure repository lays them out
		repoDir := gitrepo.New(t, map[string]string{
			"stacks/network/main.tf": "module \"vpc\" {}\n",
			"tests/terratest/go.mod": "module example.com/terratest\n\ngo 1.22\n",
		})
		cmd := newEcosystemCommand(infraRepos.NewDefaultUpdaterRegistry(), nil)

		// when
		updaters := commands.LocalUpdatersFor(
			cmd, t.Context(), repoDir, ecosystemRepository(), commands.LocalOptions{}, "",
		)

		// then
		assert.Equal(t, []string{"golang", "terraform"}, updaterNames(updaters))
	})

	t.Run("should leave out the updater a dedicated upgrade covers", func(t *testing.T) {
		t.Parallel()

		// given -- a Go repository that also carries Terraform
		repoDir := gitrepo.New(t, map[string]string{
			"go.mod":                 "module example.com/app\n\ngo 1.22\n",
			"stacks/network/main.tf": "module \"vpc\" {}\n",
		})
		cmd := newEcosystemCommand(infraRepos.NewDefaultUpdaterRegistry(), nil)

		// when
		updaters := commands.LocalUpdatersFor(
			cmd, t.Context(), repoDir, ecosystemRepository(), commands.LocalOptions{}, "golang",
		)

		// then
		assert.Equal(t, []string{"terraform"}, updaterNames(updaters))
	})

	t.Run("should leave out an updater the configuration turns off", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := gitrepo.New(t, map[string]string{
			"stacks/network/main.tf": "module \"vpc\" {}\n",
			"Dockerfile":             "FROM example-image:1.0.0\n",
		})
		disabled := false
		settings := &entities.Settings{Updaters: map[string]entities.UpdaterConfig{
			"terraform": {Enabled: &disabled},
		}}
		cmd := newEcosystemCommand(infraRepos.NewDefaultUpdaterRegistry(), nil)

		// when
		updaters := commands.LocalUpdatersFor(
			cmd, t.Context(), repoDir, ecosystemRepository(), commands.LocalOptions{Settings: settings}, "",
		)

		// then
		assert.Equal(t, []string{"dockerfile"}, updaterNames(updaters))
	})

	t.Run("should leave out an updater without a clone-based upgrade", func(t *testing.T) {
		t.Parallel()

		// given -- an updater that only knows the legacy CreateUpdatePRs flow
		registry := infraRepos.NewUpdaterRegistry()
		registry.Register(
			repositorydoubles.NewSpyUpdaterRepositoryBuilder().
				WithUpdaterName("legacy").
				WithDetectResult(true).
				BuildSpy(),
		)
		cmd := newEcosystemCommand(registry, nil)

		// when
		updaters := commands.LocalUpdatersFor(
			cmd, t.Context(), t.TempDir(), ecosystemRepository(), commands.LocalOptions{}, "",
		)

		// then
		assert.Empty(t, updaters)
	})
}

func TestLocalCommandUpgradesEveryEcosystem(t *testing.T) {
	t.Parallel()

	t.Run("should upgrade a Terraform checkout on today's branch and open one pull request", func(t *testing.T) {
		t.Parallel()

		// given -- a Terraform checkout, which langforge reads as Terraform and no dedicated
		// upgrade covers, and two updaters that each change a file
		repoDir, remoteDir := newEcosystemCheckout(t, map[string]string{"main.tf": "module \"vpc\" {}\n"})
		registry := infraRepos.NewUpdaterRegistry()
		registry.Register(writingUpdater("terraform", "main.tf", "module \"vpc\" { version = \"2\" }\n"))
		registry.Register(writingUpdater("dockerfile", "Dockerfile", "FROM example-image:2.0.0\n"))
		api := newPullRequestSpy()
		cmd := newEcosystemCommand(registry, api)

		// when
		err := cmd.Execute(t.Context(), commands.LocalOptions{RepoDir: repoDir})

		// then -- one commit on today's branch, carrying both changes, and one pull request
		require.NoError(t, err)
		branch := todaysBranch()
		require.Len(t, api.PRInputs, 1)
		assert.Equal(t, "refs/heads/"+branch, api.PRInputs[0].SourceBranch)
		assert.Equal(t, "refs/heads/main", api.PRInputs[0].TargetBranch)
		assert.Equal(t, "chore(deps): updated dependencies (dockerfile, terraform)", api.PRInputs[0].Title)
		assert.Equal(
			t,
			[]string{"chore(deps): bumped dependencies via autoupdate"},
			commitSubjects(t, remoteDir, branch),
		)
		assert.Equal(t, "FROM example-image:2.0.0\n", fileAt(t, remoteDir, branch, "Dockerfile"))
		assert.Equal(t, "module \"vpc\" { version = \"2\" }\n", fileAt(t, remoteDir, branch, "main.tf"))
		assert.Equal(t, branch, currentBranch(t, repoDir))
	})

	t.Run("should keep the other updaters' changes when one of them fails", func(t *testing.T) {
		t.Parallel()

		// given -- an updater that writes half its change and then fails
		repoDir, remoteDir := newEcosystemCheckout(t, map[string]string{"main.tf": "module \"vpc\" {}\n"})
		registry := infraRepos.NewUpdaterRegistry()
		registry.Register(failingUpdater("dockerfile", "Dockerfile", "FROM example-image:broken\n"))
		registry.Register(writingUpdater("terraform", "main.tf", "module \"vpc\" { version = \"2\" }\n"))
		api := newPullRequestSpy()
		cmd := newEcosystemCommand(registry, api)

		// when
		err := cmd.Execute(t.Context(), commands.LocalOptions{RepoDir: repoDir})

		// then -- the failure costs its own change and nothing else
		require.NoError(t, err)
		branch := todaysBranch()
		require.Len(t, api.PRInputs, 1)
		assert.Equal(t, "chore(deps): upgraded the terraform dependencies", api.PRInputs[0].Title)
		assert.Equal(t, "module \"vpc\" { version = \"2\" }\n", fileAt(t, remoteDir, branch, "main.tf"))
		assert.False(t, hasFileAt(t, remoteDir, branch, "Dockerfile"))
	})

	t.Run("should leave the files git ignores alone when an updater fails", func(t *testing.T) {
		t.Parallel()

		// given -- a checkout with a module cache and a secret, both ignored, and an
		// updater whose failure is rolled back
		repoDir, _ := newEcosystemCheckout(t, map[string]string{
			"main.tf":    "module \"vpc\" {}\n",
			".gitignore": ".terraform/\n.env\n",
		})
		gitrepo.Write(t, repoDir, map[string]string{
			".terraform/modules/modules.json": "{}",
			".env":                            "TOKEN=fixture-token-placeholder",
		})
		registry := infraRepos.NewUpdaterRegistry()
		registry.Register(failingUpdater("dockerfile", "Dockerfile", "FROM example-image:broken\n"))
		registry.Register(writingUpdater("terraform", "main.tf", "module \"vpc\" { version = \"2\" }\n"))
		cmd := newEcosystemCommand(registry, newPullRequestSpy())

		// when
		err := cmd.Execute(t.Context(), commands.LocalOptions{RepoDir: repoDir})

		// then
		require.NoError(t, err)
		assert.Equal(t, "{}", readFile(t, filepath.Join(repoDir, ".terraform", "modules", "modules.json")))
		assert.Equal(t, "TOKEN=fixture-token-placeholder", readFile(t, filepath.Join(repoDir, ".env")))
		assert.NoFileExists(t, filepath.Join(repoDir, "Dockerfile"))
	})

	t.Run("should put the operator's uncommitted work back on the branch the run started from", func(t *testing.T) {
		t.Parallel()

		// given -- a tracked file edited and an untracked one written, neither committed
		repoDir, remoteDir := newEcosystemCheckout(
			t,
			map[string]string{"main.tf": "module \"vpc\" {}\n", "notes.md": "v1\n"},
		)
		gitrepo.Write(t, repoDir, map[string]string{"notes.md": "work in progress\n", "scratch.txt": "draft\n"})
		registry := infraRepos.NewUpdaterRegistry()
		registry.Register(writingUpdater("terraform", "main.tf", "module \"vpc\" { version = \"2\" }\n"))
		api := newPullRequestSpy()
		cmd := newEcosystemCommand(registry, api)

		// when
		err := cmd.Execute(t.Context(), commands.LocalOptions{RepoDir: repoDir})

		// then -- the operator's work never reached the branch, and it is back where it was
		require.NoError(t, err)
		branch := todaysBranch()
		assert.Equal(t, "v1\n", fileAt(t, remoteDir, branch, "notes.md"))
		assert.False(t, hasFileAt(t, remoteDir, branch, "scratch.txt"))
		assert.Equal(t, "main", currentBranch(t, repoDir))
		assert.Equal(t, "work in progress\n", readFile(t, filepath.Join(repoDir, "notes.md")))
		assert.Equal(t, "draft\n", readFile(t, filepath.Join(repoDir, "scratch.txt")))
	})

	t.Run("should change nothing in a rehearsal", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir, remoteDir := newEcosystemCheckout(t, map[string]string{"main.tf": "module \"vpc\" {}\n"})
		updater := writingUpdater("terraform", "main.tf", "module \"vpc\" { version = \"2\" }\n")
		registry := infraRepos.NewUpdaterRegistry()
		registry.Register(updater)
		api := newPullRequestSpy()
		cmd := newEcosystemCommand(registry, api)

		// when
		err := cmd.Execute(t.Context(), commands.LocalOptions{RepoDir: repoDir, DryRun: true})

		// then
		require.NoError(t, err)
		assert.Zero(t, updater.ApplyCallCount)
		assert.Empty(t, api.PRInputs)
		assert.Equal(t, "main", currentBranch(t, repoDir))
		assert.False(t, hasBranch(t, remoteDir, todaysBranch()))
	})

	t.Run("should skip a run whose pull request is already open today", func(t *testing.T) {
		t.Parallel()

		// given -- today's aggregate pull request exists, as after an earlier run
		repoDir, remoteDir := newEcosystemCheckout(t, map[string]string{"main.tf": "module \"vpc\" {}\n"})
		updater := writingUpdater("terraform", "main.tf", "module \"vpc\" { version = \"2\" }\n")
		registry := infraRepos.NewUpdaterRegistry()
		registry.Register(updater)
		api := newPullRequestSpy()
		api.PRExistsResult = true
		cmd := newEcosystemCommand(registry, api)

		// when
		err := cmd.Execute(t.Context(), commands.LocalOptions{RepoDir: repoDir})

		// then
		require.NoError(t, err)
		assert.Zero(t, updater.ApplyCallCount)
		assert.Empty(t, api.PRInputs)
		assert.False(t, hasBranch(t, remoteDir, todaysBranch()))
	})

	t.Run("should put the dedicated upgrade and the others on one branch and in one pull request", func(t *testing.T) {
		t.Parallel()

		// given -- a Go repository with Terraform beside it: the dedicated Go upgrade
		// (standing in here for the real one, which needs the network) commits first
		repoDir, remoteDir := newEcosystemCheckout(t, map[string]string{
			"go.mod":                 "module example.com/app\n\ngo 1.22\n",
			"stacks/network/main.tf": "module \"vpc\" {}\n",
		})
		api := newPullRequestSpy()
		cmd := newEcosystemCommand(infraRepos.NewUpdaterRegistry(), api)
		terraform := writingUpdater("terraform", "stacks/network/main.tf", "module \"vpc\" { version = \"2\" }\n")
		others := []commands.ApplicableUpdater{
			commands.NewApplicableUpdaterForTest(terraform, entities.UpdateOptions{}),
		}
		remote, err := commands.ParseRemoteURL(ecosystemOrigin)
		require.NoError(t, err)
		repo := ecosystemRepository()
		repo.DefaultBranch = "main"

		// when
		err = commands.UpgradeEveryEcosystemForTest(
			t.Context(), cmd, repoDir, remote, commands.NewLocalTokensForTest("", "fixture-api-token-placeholder"),
			repo, commands.LocalOptions{RepoDir: repoDir}, "golang", fakeGoUpgrade, others,
		)

		// then -- two commits, the dedicated one first, and one pull request naming both
		require.NoError(t, err)
		branch := todaysBranch()
		assert.Equal(t, []string{
			"chore(deps): upgraded the terraform dependencies",
			"chore(deps): updated Go module dependencies",
		}, commitSubjects(t, remoteDir, branch))
		require.Len(t, api.PRInputs, 1)
		assert.Equal(t, "refs/heads/"+branch, api.PRInputs[0].SourceBranch)
		assert.Equal(t, "chore(deps): updated dependencies (golang, terraform)", api.PRInputs[0].Title)
		assert.Contains(t, api.PRInputs[0].Description, "## golang")
		assert.Contains(t, api.PRInputs[0].Description, "## terraform")
	})
}

// --- fixtures ---

// ecosystemRepository is the repository ecosystemOrigin names.
func ecosystemRepository() entities.Repository {
	return entities.Repository{ID: "infra-repo", Name: "infra-repo", Organization: "example-org"}
}

// newEcosystemCommand builds a LocalCommand over updaters and a provider API that answers
// with api, signed in through the gh CLI and with no token exported for git.
func newEcosystemCommand(
	updaters *infraRepos.UpdaterRegistry, api *repositorydoubles.SpyProviderRepository,
) *commands.LocalCommand {
	providers := infraRepos.NewProviderRegistry()
	if api != nil {
		providers.Register("github", func(_ string) domainRepos.ProviderRepository { return api })
	}
	return commands.NewLocalCommand(
		providers, updaters, cliCredentials(), noGitToken(),
		&gitlocal.OriginRepository{SSHHostName: func(string) string { return "" }},
	)
}

// newPullRequestSpy is the provider API: no pull request open yet, and one created on request.
func newPullRequestSpy() *repositorydoubles.SpyProviderRepository {
	return repositorydoubles.NewSpyProviderRepositoryBuilder().
		WithProviderName("github").
		WithToken(cliTokenFixture).
		WithCreatedPR(&entities.PullRequest{ID: 7, URL: "https://example.com/pull/7"}).
		BuildSpy()
}

// newEcosystemCheckout commits files into a checkout on main whose origin is fetched over
// SSH and pushed to a bare repository on disk, which it returns beside the checkout. The
// checkout's own configuration turns signing off, so the developer's cannot reach the test.
func newEcosystemCheckout(t *testing.T, files map[string]string) (string, string) {
	t.Helper()

	remoteDir := t.TempDir()
	runGit(t, remoteDir, "init", "--quiet", "--bare", "--initial-branch=main")

	repoDir := gitrepo.New(t, files)
	runGit(t, repoDir, "config", "commit.gpgsign", "false")
	runGit(t, repoDir, "config", "user.name", "autoupdate-test")
	runGit(t, repoDir, "config", "user.email", "autoupdate-test@example.com")
	runGit(t, repoDir, "config", "core.hooksPath", "/dev/null")
	runGit(t, repoDir, "remote", "add", "origin", ecosystemOrigin)
	runGit(t, repoDir, "remote", "set-url", "--push", "origin", remoteDir)
	runGit(t, repoDir, "push", "--quiet", remoteDir, "main")

	return repoDir, remoteDir
}

// writingUpdater is an updater that applies to every checkout and writes content to path.
func writingUpdater(name, path, content string) *repositorydoubles.SpyLocalUpdaterRepository {
	return repositorydoubles.NewSpyLocalUpdaterRepositoryBuilder().
		WithUpdaterName(name).
		WithDetectResult(true).
		WithApplyUpdateFn(func(repoDir string, _ entities.UpdateOptions) (*domainRepos.LocalUpdateResult, error) {
			if err := writeRepoFile(repoDir, path, content); err != nil {
				return nil, err
			}
			message := "chore(deps): upgraded the " + name + " dependencies"
			return &domainRepos.LocalUpdateResult{
				CommitMessage: message,
				PRTitle:       message,
				PRDescription: name + " body",
			}, nil
		}).
		BuildSpy()
}

// failingUpdater is an updater that writes content to path and then fails.
func failingUpdater(name, path, content string) *repositorydoubles.SpyLocalUpdaterRepository {
	return repositorydoubles.NewSpyLocalUpdaterRepositoryBuilder().
		WithUpdaterName(name).
		WithDetectResult(true).
		WithApplyUpdateFn(func(repoDir string, _ entities.UpdateOptions) (*domainRepos.LocalUpdateResult, error) {
			if err := writeRepoFile(repoDir, path, content); err != nil {
				return nil, err
			}
			return nil, errors.New("the registry did not answer")
		}).
		BuildSpy()
}

// fakeGoUpgrade stands in for the dedicated Go upgrade: it makes its branch the way the real
// one does, changes go.mod, and commits and pushes.
func fakeGoUpgrade(
	_ context.Context,
	repoDir, _ string,
	tokens commands.LocalTokens,
	_ entities.Repository,
	opts commands.LocalOptions,
	registry *infraRepos.ProviderRegistry,
) (*commands.LocalPRInfoForTest, error) {
	branch := commands.BranchNameOf(opts)
	gitCtx, restore, err := gitlocal.PrepareBranch(repoDir, branch, registry)
	if err != nil {
		return nil, err
	}
	defer restore()

	if writeErr := writeRepoFile(repoDir, "go.mod", "module example.com/app\n\ngo 1.23\n"); writeErr != nil {
		return nil, writeErr
	}
	pushed, err := gitCtx.StageCommitAndPush(branch, "chore(deps): updated Go module dependencies", tokens.Git)
	if err != nil {
		return nil, err
	}

	return &commands.LocalPRInfoForTest{
		BranchName:    branch,
		LatestVersion: "1.23",
		ProjectType:   langEntities.LanguageGo,
		HasChanges:    pushed,
	}, nil
}

// --- helpers ---

func updaterNames(updaters []commands.ApplicableUpdater) []string {
	names := make([]string, 0, len(updaters))
	for _, au := range updaters {
		names = append(names, commands.UpdaterOf(au).Name())
	}
	return names
}

func todaysBranch() string {
	return commands.BuildAggregateBranchName(entities.DefaultAggregateBranchPrefix, time.Now())
}

func writeRepoFile(repoDir, path, content string) error {
	full := filepath.Join(repoDir, filepath.FromSlash(path))
	// A directory needs the owner search bit, so 0o700 is the least-privilege mode here.
	// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o600)
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// gitOutput runs git in dir and returns its trimmed output.
func gitOutput(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	return strings.TrimSpace(string(output)), err
}

func currentBranch(t *testing.T, repoDir string) string {
	t.Helper()

	branch, err := gitOutput(t, repoDir, "rev-parse", "--abbrev-ref", "HEAD")
	require.NoError(t, err)
	return branch
}

func hasBranch(t *testing.T, remoteDir, branch string) bool {
	t.Helper()

	_, err := gitOutput(t, remoteDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// commitSubjects lists, newest first, the subjects of the commits branch adds to main.
func commitSubjects(t *testing.T, remoteDir, branch string) []string {
	t.Helper()

	output, err := gitOutput(t, remoteDir, "log", "--format=%s", "main.."+branch)
	require.NoError(t, err)
	return strings.Split(output, "\n")
}

func fileAt(t *testing.T, remoteDir, branch, path string) string {
	t.Helper()

	cmd := exec.Command("git", "show", branch+":"+path)
	cmd.Dir = remoteDir
	output, err := cmd.Output()
	require.NoError(t, err)
	return string(output)
}

func hasFileAt(t *testing.T, remoteDir, branch, path string) bool {
	t.Helper()

	_, err := gitOutput(t, remoteDir, "cat-file", "-e", branch+":"+path)
	return err == nil
}
