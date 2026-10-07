package java_test

import (
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	javaUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/java"
	"github.com/rios0rios0/autoupdate/internal/support"
	"github.com/rios0rios0/autoupdate/test/infrastructure/scriptrunner"
)

// argumentEcho stands in for a build tool's wrapper script: it prints every
// argument it was given inside brackets, so a test can tell one argument holding
// a space from two arguments, and an empty argument from none.
const argumentEcho = "#!/bin/sh\nfor arg in \"$@\"; do printf '[%s]' \"$arg\"; done\nprintf '\\n'\n"

// runJavaUpgrade runs the Java upgrade commands for buildSystem against a
// repository holding the given files, and returns what the wrappers printed.
func runJavaUpgrade(t *testing.T, buildSystem string, files map[string]string, env map[string]string) string {
	t.Helper()

	repoDir := t.TempDir()
	for name, content := range files {
		scriptrunner.WriteFile(t, repoDir, name, content)
	}

	var sb strings.Builder
	javaUpdater.WriteJavaUpgradeCommands(&sb, javaUpdater.UpgradeParamsExported{
		BuildSystem:       buildSystem,
		AllowMajorUpdates: true,
	})

	runEnv := map[string]string{"JAVA_VERSION": "21.0.5", "BUILD_SYSTEM": buildSystem}
	maps.Copy(runEnv, env)
	return scriptrunner.Run(t, repoDir, sb.String(), scriptrunner.Options{Env: runEnv})
}

func TestJavaUpgradeCommandsLeaveNothingBehind(t *testing.T) {
	t.Parallel()

	t.Run("should run every Gradle command without a daemon", func(t *testing.T) {
		t.Parallel()

		// given -- a wrapper and a dependency lock, so both Gradle commands run

		// when
		output := runJavaUpgrade(t, "gradle", map[string]string{
			"gradlew":         argumentEcho,
			"gradle.lockfile": "empty=\n",
		}, nil)

		// then
		assert.Contains(t, output, "[--no-daemon][wrapper]")
		assert.Contains(t, output, "[--no-daemon][dependencies][--write-locks]")
	})

	t.Run("should keep Maven's downloads in the local repository the run names", func(t *testing.T) {
		t.Parallel()

		// given -- a temporary directory with a space in its path, which MAVEN_OPTS
		// would have split in two
		repository := filepath.Join(t.TempDir(), "with space", "maven")

		// when
		output := runJavaUpgrade(t, "maven", map[string]string{"mvnw": argumentEcho},
			map[string]string{support.MavenRepositoryVariable: repository})

		// then
		assert.Equal(t, 2, strings.Count(output, "[-Dmaven.repo.local="+repository+"]"),
			"both versions-maven-plugin goals should use it:\n%s", output)
	})

	t.Run("should leave Maven on its own local repository when the run names none", func(t *testing.T) {
		t.Parallel()

		// given / when
		output := runJavaUpgrade(t, "maven", map[string]string{"mvnw": argumentEcho}, nil)

		// then
		assert.Contains(t, output, "[versions:update-properties]")
		assert.NotContains(t, output, "maven.repo.local")
		assert.NotContains(t, output, "[]", "an unset variable must add no argument, not an empty one")
	})
}
