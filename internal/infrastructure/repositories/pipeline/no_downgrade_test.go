package pipeline_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pipelineUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/pipeline"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

// nodeToolPipeline renders an Azure DevOps pipeline pinning Node.js to version.
func nodeToolPipeline(version string) string {
	return "steps:\n" +
		"  - task: NodeTool@0\n" +
		"    inputs:\n" +
		"      versionSpec: '" + version + "'\n"
}

// pythonPipeline renders an Azure DevOps pipeline pinning Python to version.
func pythonPipeline(version string) string {
	return "steps:\n" +
		"  - task: UsePythonVersion@0\n" +
		"    inputs:\n" +
		"      versionSpec: '" + version + "'\n"
}

// scanPipeline runs the local scan against a repository holding one pipeline
// file and returns the upgrades it planned.
func scanPipeline(t *testing.T, pipeline string, latest map[string]string) []pipelineUpdater.UpgradeTask {
	t.Helper()

	repoDir := t.TempDir()
	path := filepath.Join(repoDir, "azure-pipelines.yml")
	require.NoError(t, os.WriteFile(path, []byte(pipeline), 0o600))
	provider := repositorydoubles.NewSpyProviderRepositoryBuilder().BuildSpy()

	upgrades, _ := pipelineUpdater.LocalScanAndDetermineUpgrades(t.Context(), repoDir, provider, latest, true)

	return upgrades
}

func TestPipelineVersionPinIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	t.Run("should plan an upgrade when the pipeline pins an older release", func(t *testing.T) {
		t.Parallel()

		// given
		pipeline := nodeToolPipeline("20")

		// when
		upgrades := scanPipeline(t, pipeline, map[string]string{"nodejs": "24.19.0"})

		// then
		require.Len(t, upgrades, 1)
		assert.Equal(t, "24", pipelineUpdater.UpgradeTaskNewVersion(upgrades[0]))
	})

	t.Run("should plan no upgrade when the pipeline pins a release newer than the LTS", func(t *testing.T) {
		t.Parallel()

		// given — the pipeline deliberately tracks a current Node.js release
		pipeline := nodeToolPipeline("26")

		// when
		upgrades := scanPipeline(t, pipeline, map[string]string{"nodejs": "24.19.0"})

		// then
		assert.Empty(t, upgrades)
	})

	t.Run("should plan no upgrade when the pipeline already names the latest release", func(t *testing.T) {
		t.Parallel()

		// given
		pipeline := pythonPipeline("3.13")

		// when
		upgrades := scanPipeline(t, pipeline, map[string]string{"python": "3.13.2"})

		// then
		assert.Empty(t, upgrades)
	})

	t.Run("should plan no upgrade when the pipeline pins a newer Python series", func(t *testing.T) {
		t.Parallel()

		// given
		pipeline := pythonPipeline("3.14")

		// when
		upgrades := scanPipeline(t, pipeline, map[string]string{"python": "3.13.2"})

		// then
		assert.Empty(t, upgrades)
	})
}
