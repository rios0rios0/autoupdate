package commands_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	domainRepos "github.com/rios0rios0/autoupdate/internal/domain/repositories"
	infraRepos "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
	globalEntities "github.com/rios0rios0/gitforge/v4/pkg/global/domain/entities"
)

// localForge is a provider whose repository lives on the local filesystem. It
// answers the registry's URL and service-type lookups for that path, so a batch
// run clones, commits, pushes and opens its pull request exactly as it would
// against a hosted remote; the pull request itself is recorded by the spy.
type localForge struct {
	*repositorydoubles.SpyProviderRepository

	remoteDir string
}

func (f *localForge) MatchesURL(url string) bool                     { return url == f.remoteDir }
func (f *localForge) GetServiceType() globalEntities.ServiceType     { return globalEntities.GITHUB }
func (f *localForge) PrepareCloneURL(url string) string              { return url }
func (f *localForge) ConfigureTransport()                            {}
func (f *localForge) GetAuthMethods(_ string) []transport.AuthMethod { return localAuthMethods() }

// fillModuleCache writes into the tooling directory what `go get` leaves there: a
// module inside directories nobody may write to, which a plain [os.RemoveAll]
// cannot delete.
func fillModuleCache(toolingDir string) error {
	module := filepath.Join(toolingDir, "go", "mod", "example.com", "dependency@v1.0.0")
	// A directory needs the owner search bit, so 0o700 is the least-privilege mode.
	// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
	if err := os.MkdirAll(module, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(
		filepath.Join(module, "go.mod"),
		[]byte("module example.com/dependency\n"),
		0o400,
	); err != nil {
		return err
	}
	for dir := module; dir != toolingDir; dir = filepath.Dir(dir) {
		if err := os.Chmod(dir, 0o555); err != nil {
			return err
		}
	}
	return nil
}

// upgradeFile changes the clone the way an upgrade would.
func upgradeFile(repoDir string) error {
	return os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test\n\nupgraded\n"), 0o600)
}

// TestRunCommandLeavesNothingBehind is not parallel: it points TMPDIR at a
// directory of its own with t.Setenv, so that "nothing is left behind" is checked
// by reading that directory, and HOME at an empty one, so the developer's global
// git configuration cannot ask for the aggregate commit to be signed.
func TestRunCommandLeavesNothingBehind(t *testing.T) {
	outcomes := []struct {
		scenario     string
		apply        func(repoDir string) (*domainRepos.LocalUpdateResult, error)
		pullRequests int
	}{
		{
			scenario: "a pull request is opened",
			apply: func(repoDir string) (*domainRepos.LocalUpdateResult, error) {
				return &domainRepos.LocalUpdateResult{
					CommitMessage: "chore(deps): updated all dependencies",
					PRTitle:       "chore(deps): updated all dependencies",
				}, upgradeFile(repoDir)
			},
			pullRequests: 1,
		},
		{
			scenario: "nothing needs upgrading",
			apply: func(_ string) (*domainRepos.LocalUpdateResult, error) {
				return nil, domainRepos.ErrNoUpdatesNeeded
			},
		},
		{
			scenario: "the updater fails halfway through",
			apply: func(repoDir string) (*domainRepos.LocalUpdateResult, error) {
				return nil, errors.Join(upgradeFile(repoDir), errors.New("the package manager crashed"))
			},
		},
	}

	for _, outcome := range outcomes {
		t.Run("should remove the clone and every cache when "+outcome.scenario, func(t *testing.T) {
			// given
			remoteDir := newBareRemoteWithBranches(t)
			tmpDir := t.TempDir()
			t.Setenv("TMPDIR", tmpDir)
			t.Setenv("HOME", t.TempDir())

			var toolingDirs []string
			updater := repositorydoubles.NewSpyLocalUpdaterRepositoryBuilder().
				WithUpdaterName("golang").
				WithDetectResult(true).
				WithApplyUpdateFn(func(repoDir string, opts entities.UpdateOptions) (*domainRepos.LocalUpdateResult, error) {
					toolingDirs = append(toolingDirs, opts.ToolingDir)
					if err := fillModuleCache(opts.ToolingDir); err != nil {
						return nil, err
					}
					return outcome.apply(repoDir)
				}).
				BuildSpy()
			cmd, spy := newLocalRunCommand(remoteDir, updater)

			// when
			err := cmd.Execute(context.Background(), localRunSettings(), commands.RunOptions{})

			// then
			require.NoError(t, err)
			require.Len(t, toolingDirs, 1, "the updater should have run against the clone")
			assert.True(t, strings.HasPrefix(toolingDirs[0], tmpDir+string(os.PathSeparator)),
				"the caches should have been kept in the repository's workspace, got %s", toolingDirs[0])
			assert.Len(t, spy.PRInputs, outcome.pullRequests)
			assertEmptyDir(t, tmpDir)
		})
	}

	t.Run("should remove the workspace when the clone itself fails", func(t *testing.T) {
		// given
		missingRemote := filepath.Join(t.TempDir(), "no-such-remote")
		tmpDir := t.TempDir()
		t.Setenv("TMPDIR", tmpDir)
		t.Setenv("HOME", t.TempDir())
		updater := repositorydoubles.NewSpyLocalUpdaterRepositoryBuilder().
			WithUpdaterName("golang").
			WithDetectResult(true).
			BuildSpy()
		cmd, _ := newLocalRunCommand(missingRemote, updater)

		// when
		err := cmd.Execute(context.Background(), localRunSettings(), commands.RunOptions{})

		// then
		require.NoError(t, err)
		assert.Zero(t, updater.ApplyCallCount)
		assertEmptyDir(t, tmpDir)
	})
}

// newLocalRunCommand builds a run command over one repository served from
// remoteDir and one local updater, and returns the spy recording its pull
// requests.
func newLocalRunCommand(
	remoteDir string, updater domainRepos.UpdaterRepository,
) (*commands.RunCommand, *repositorydoubles.SpyProviderRepository) {
	spy := repositorydoubles.NewSpyProviderRepositoryBuilder().
		WithProviderName("github").
		WithToken("test-token").
		WithRepositories([]entities.Repository{{
			ID:            "repo-1",
			Name:          "repo",
			Organization:  "org",
			DefaultBranch: "refs/heads/main",
			RemoteURL:     remoteDir,
		}}).
		BuildSpy()
	forge := &localForge{SpyProviderRepository: spy, remoteDir: remoteDir}

	providerRegistry := infraRepos.NewProviderRegistry()
	providerRegistry.Register("github", func(_ string) domainRepos.ProviderRepository { return forge })
	providerRegistry.RegisterAdapter(forge)

	updaterRegistry := infraRepos.NewUpdaterRegistry()
	updaterRegistry.Register(updater)

	return commands.NewRunCommand(providerRegistry, updaterRegistry), spy
}

// localRunSettings configures a run over the single organization newLocalRunCommand serves.
func localRunSettings() *entities.Settings {
	return &entities.Settings{
		Providers: []entities.ProviderConfig{{
			Type:          "github",
			Token:         "test-token",
			Organizations: []string{"org"},
		}},
	}
}

// assertEmptyDir fails the test with whatever dir still holds.
func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	assert.Empty(t, names, "the run left these behind in %s", dir)
}
