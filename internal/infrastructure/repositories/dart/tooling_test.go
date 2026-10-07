package dart_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	dartUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/dart"
	"github.com/rios0rios0/autoupdate/test/infrastructure/gitrepo"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

func TestApplyUpdatesToolingDir(t *testing.T) {
	t.Parallel()

	t.Run("should keep the pub cache in the tooling directory the run gives it", func(t *testing.T) {
		t.Parallel()

		// given
		root := gitrepo.New(t, map[string]string{"pubspec.yaml": "name: app\n"})
		runner := repositorydoubles.NewFakeUpgradeRunner(nil, "")
		updater := dartUpdater.NewUpdaterRepositoryForTest(
			&repositorydoubles.StubVersionFetcher{Version: "3.13.4"},
			&repositorydoubles.StubVersionFetcher{Version: "3.41.0"},
			runner,
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
		assert.Contains(t, runner.Calls[0].Opts.Env, "PUB_CACHE="+filepath.Join(toolingDir, "pub"))
		assert.Contains(t, runner.Calls[0].Opts.Env, "TMPDIR="+filepath.Join(toolingDir, "tmp"))
	})
}
