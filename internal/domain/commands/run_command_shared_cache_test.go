package commands_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	domainRepos "github.com/rios0rios0/autoupdate/internal/domain/repositories"
	infraRepos "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
	globalEntities "github.com/rios0rios0/gitforge/v4/pkg/global/domain/entities"
)

// localForgeOfMany serves several repositories from the local filesystem, the way
// localForge serves one.
type localForgeOfMany struct {
	*repositorydoubles.SpyProviderRepository

	remoteDirs []string
}

func (f *localForgeOfMany) MatchesURL(url string) bool                     { return slices.Contains(f.remoteDirs, url) }
func (f *localForgeOfMany) GetServiceType() globalEntities.ServiceType     { return globalEntities.GITHUB }
func (f *localForgeOfMany) PrepareCloneURL(url string) string              { return url }
func (f *localForgeOfMany) ConfigureTransport()                            {}
func (f *localForgeOfMany) GetAuthMethods(_ string) []transport.AuthMethod { return localAuthMethods() }

// TestRunCommandSharesTheGoCaches is not parallel: it points TMPDIR at a directory
// of its own with t.Setenv, so that the shared cache can be found, and shown to be
// gone, by reading that directory -- and HOME at an empty one, for the reason
// TestRunCommandLeavesNothingBehind gives.
func TestRunCommandSharesTheGoCaches(t *testing.T) {
	t.Run("should give every repository the same Go caches and remove them when the run ends", func(t *testing.T) {
		// given two repositories, each upgraded by an updater that fills the Go caches
		remoteDirs := []string{newBareRemoteWithBranches(t), newBareRemoteWithBranches(t)}
		tmpDir := t.TempDir()
		t.Setenv("TMPDIR", tmpDir)
		t.Setenv("HOME", t.TempDir())

		var (
			mu          sync.Mutex
			sharedDirs  []string
			toolingDirs []string
		)
		updater := repositorydoubles.NewSpyLocalUpdaterRepositoryBuilder().
			WithUpdaterName("golang").
			WithDetectResult(true).
			WithApplyUpdateFn(func(_ string, opts entities.UpdateOptions) (*domainRepos.LocalUpdateResult, error) {
				mu.Lock()
				defer mu.Unlock()
				sharedDirs = append(sharedDirs, opts.SharedToolingDir)
				toolingDirs = append(toolingDirs, opts.ToolingDir)
				modCache := filepath.Join(opts.SharedToolingDir, "go", "mod")
				// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
				if err := os.MkdirAll(modCache, 0o700); err != nil {
					return nil, err
				}
				marker := filepath.Join(modCache, "downloaded-by-"+filepath.Base(opts.ToolingDir))
				if err := os.WriteFile(marker, nil, 0o600); err != nil {
					return nil, err
				}
				return nil, domainRepos.ErrNoUpdatesNeeded
			}).
			BuildSpy()
		cmd := newLocalRunCommandOfMany(remoteDirs, updater)

		// when
		err := cmd.Execute(context.Background(), localRunSettings(), commands.RunOptions{})

		// then both repositories used one shared directory beside their own workspaces,
		// and nothing of either is left once the run is over
		require.NoError(t, err)
		require.Len(t, sharedDirs, 2, "the updater should have run against both clones")
		assert.NotEmpty(t, sharedDirs[0])
		assert.Equal(t, sharedDirs[0], sharedDirs[1], "the repositories should share the Go caches")
		assert.True(t, strings.HasPrefix(sharedDirs[0], tmpDir+string(os.PathSeparator)),
			"the shared cache should live in the temporary directory, got %s", sharedDirs[0])
		assert.NotEqual(t, toolingDirs[0], toolingDirs[1], "every repository should still have its own workspace")
		assertEmptyDir(t, tmpDir)
	})
}

// newLocalRunCommandOfMany builds a run command over one repository per remote
// directory, all in the organization localRunSettings configures.
func newLocalRunCommandOfMany(
	remoteDirs []string, updater domainRepos.UpdaterRepository,
) *commands.RunCommand {
	repositories := make([]entities.Repository, 0, len(remoteDirs))
	for index, remoteDir := range remoteDirs {
		name := "repo-" + string(rune('a'+index))
		repositories = append(repositories, entities.Repository{
			ID:            name,
			Name:          name,
			Organization:  "org",
			DefaultBranch: "refs/heads/main",
			RemoteURL:     remoteDir,
		})
	}
	spy := repositorydoubles.NewSpyProviderRepositoryBuilder().
		WithProviderName("github").
		WithToken("test-token").
		WithRepositories(repositories).
		BuildSpy()
	forge := &localForgeOfMany{SpyProviderRepository: spy, remoteDirs: remoteDirs}

	providerRegistry := infraRepos.NewProviderRegistry()
	providerRegistry.Register("github", func(_ string) domainRepos.ProviderRepository { return forge })
	providerRegistry.RegisterAdapter(forge)

	updaterRegistry := infraRepos.NewUpdaterRegistry()
	updaterRegistry.Register(updater)

	return commands.NewRunCommand(providerRegistry, updaterRegistry)
}
