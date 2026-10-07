package golang_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	goUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
	"github.com/rios0rios0/autoupdate/test/infrastructure/gitrepo"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestApplyUpdatesToolingDir(t *testing.T) {
	t.Parallel()

	t.Run("should keep the module and build caches in the tooling directory the run gives it", func(t *testing.T) {
		t.Parallel()

		// given
		root := gitrepo.New(t, map[string]string{"go.mod": appGoMod})
		runner := repositorydoubles.NewFakeUpgradeRunner(nil, "")
		updater := goUpdater.NewUpdaterRepositoryForTest(
			&repositorydoubles.StubVersionFetcher{Version: "1.27.1"}, runner,
		)
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy()
		toolingDir := t.TempDir()

		// when
		_, err := updater.ApplyUpdates(t.Context(), root, provider,
			entities.Repository{Organization: "org", Name: "repo"},
			entities.UpdateOptions{ToolingDir: toolingDir})

		// then
		require.ErrorIs(t, err, repositories.ErrNoUpdatesNeeded)
		require.Len(t, runner.Calls, 1)
		assert.Contains(t, runner.Calls[0].Opts.Env, "GOMODCACHE="+filepath.Join(toolingDir, "go", "mod"))
		assert.Contains(t, runner.Calls[0].Opts.Env, "GOCACHE="+filepath.Join(toolingDir, "go", "build"))
		assert.Contains(t, runner.Calls[0].Opts.Env, "TMPDIR="+filepath.Join(toolingDir, "tmp"))
	})
}
