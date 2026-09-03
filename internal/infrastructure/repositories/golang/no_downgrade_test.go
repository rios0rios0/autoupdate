package golang_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	goUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
	"github.com/rios0rios0/autoupdate/test/infrastructure/scriptrunner"
)

func goMod(version string) string {
	return "module example.com/demo\n\ngo " + version + "\n"
}

// runGoUpgradeBlock executes the emitted module-upgrade block with the Go
// toolchain stubbed out, so only the directive rewrite is exercised.
func runGoUpgradeBlock(t *testing.T, repoDir, goVersion string) string {
	t.Helper()

	var sb strings.Builder
	goUpdater.WriteGoUpgradeCommands(&sb, false)

	return scriptrunner.Run(t, repoDir, sb.String(), scriptrunner.Options{
		Env:   map[string]string{"GO_VERSION": goVersion, "GO_BINARY": "go"},
		Stubs: []string{"go"},
	})
}

func TestGoDirectiveIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		declared string
		want     string
		marker   string
	}{
		{
			name:     "keep the directive when the module targets a newer Go than the latest stable",
			declared: "1.27.0", want: "go 1.27.0", marker: "GO_VERSION_UPDATED=false",
		},
		{
			name:     "raise the directive when the module is behind the latest stable",
			declared: "1.24.0", want: "go 1.26.6", marker: "GO_VERSION_UPDATED=true",
		},
		{
			// A dependency requiring a newer Go makes `go mod tidy` raise the
			// directive; the re-apply step used to write the older target back.
			name:     "not re-apply the target over a directive tidy left ahead of it",
			declared: "1.27.0", want: "go 1.27.0", marker: "GO_VERSION_UPDATED=false",
		},
	}

	for _, testCase := range cases {
		t.Run("should "+testCase.name, func(t *testing.T) {
			t.Parallel()

			// given
			repoDir := t.TempDir()
			scriptrunner.WriteFile(t, repoDir, "go.mod", goMod(testCase.declared))

			// when
			output := runGoUpgradeBlock(t, repoDir, "1.26.6")

			// then
			assert.Contains(t, scriptrunner.ReadFile(t, repoDir, "go.mod"), testCase.want)
			assert.Contains(t, output, testCase.marker)
		})
	}

	t.Run("should decide per module rather than from the first one read", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, "go.mod", goMod("1.27.0"))
		scriptrunner.WriteFile(t, repoDir, "tools/go.mod", goMod("1.24.0"))

		// when
		runGoUpgradeBlock(t, repoDir, "1.26.6")

		// then
		assert.Contains(t, scriptrunner.ReadFile(t, repoDir, "go.mod"), "go 1.27.0")
		assert.Contains(t, scriptrunner.ReadFile(t, repoDir, "tools/go.mod"), "go 1.26.6")
	})
}

func TestGoVersionContextIsNeverADowngrade(t *testing.T) {
	t.Parallel()

	t.Run("should report no version upgrade when every module is ahead of the release", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, "go.mod", goMod("1.27.0"))

		// when
		vCtx := goUpdater.LocalResolveVersionContext(repoDir, "1.26.6")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-go-deps", vCtx.BranchName)
	})

	t.Run("should report a version upgrade when a nested module is behind the release", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, "go.mod", goMod("1.26.6"))
		scriptrunner.WriteFile(t, repoDir, "tools/go.mod", goMod("1.24.0"))

		// when
		vCtx := goUpdater.LocalResolveVersionContext(repoDir, "1.26.6")

		// then
		assert.True(t, vCtx.NeedsVersionUpgrade)
	})
}
