package dart_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	dartUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/dart"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
)

// flutterPubspec is the shape of a real Flutter manifest: SDK-sourced packages
// and a top-level flutter section, either of which identifies the toolchain.
const flutterPubspec = `name: medhub
publish_to: 'none'
version: 0.1.0

environment:
  sdk: ^3.13.0

dependencies:
  flutter:
    sdk: flutter
  go_router: ^17.0.0

flutter:
  uses-material-design: true
`

// dartPubspec is a plain Dart package: no SDK-sourced package, no flutter section.
const dartPubspec = `name: cli
version: 1.0.0

environment:
  sdk: ^3.13.0

dependencies:
  args: ^2.7.0
`

func TestName(t *testing.T) {
	t.Parallel()

	t.Run("should return dart as updater name", func(t *testing.T) {
		t.Parallel()

		// given
		updater := dartUpdater.NewUpdaterRepository()

		// when
		name := updater.Name()

		// then
		assert.Equal(t, "dart", name)
	})
}

func TestDetect(t *testing.T) {
	t.Parallel()

	t.Run("should return true when pubspec.yaml exists", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{"pubspec.yaml": true}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := dartUpdater.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.True(t, detected)
	})

	t.Run("should return false when no Dart files exist", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{}).
			BuildSpy()
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		detected := dartUpdater.NewUpdaterRepository().Detect(t.Context(), provider, repo)

		// then
		assert.False(t, detected)
	})
}

func TestResolveVersionContext(t *testing.T) {
	t.Parallel()

	repo := entities.Repository{Organization: "org", Name: "repo"}

	// The manifest is the only input that decides the toolchain, and the
	// toolchain in turn decides which release channel supplies the version —
	// so both assertions belong to the same scenario.
	t.Run("should pick the toolchain the manifest calls for", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name              string
			pubspec           string
			expectedToolchain string
			expectedVersion   string
		}{
			{"Flutter manifest", flutterPubspec, "flutter", "3.47.0"},
			{"plain Dart package", dartPubspec, "dart", "3.13.0"},
		}

		for _, testCase := range testCases {
			t.Run(testCase.name, func(t *testing.T) {
				t.Parallel()

				// given
				provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
					WithFileContents(map[string]string{"pubspec.yaml": testCase.pubspec}).
					WithExistingFiles(map[string]bool{"pubspec.yaml": true}).
					BuildSpy()
				updater := dartUpdater.NewUpdaterRepositoryForTest(
					&repositorydoubles.StubVersionFetcher{Version: "3.13.0"},
					&repositorydoubles.StubVersionFetcher{Version: "3.47.0"},
				)

				// when
				vCtx := updater.ResolveVersionContextForTest(t.Context(), provider, repo)

				// then
				assert.Equal(t, testCase.expectedToolchain, vCtx.Toolchain)
				assert.Equal(t, testCase.expectedVersion, vCtx.LatestVersion)
			})
		}
	})

	// .fvmrc pins a Flutter SDK. A plain Dart package that carries one — a CLI
	// living beside a Flutter app, say — must not have it rewritten with a
	// version from the Dart release channel.
	t.Run("should ignore an .fvmrc pin when the manifest is a plain Dart package", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFileContents(map[string]string{
				"pubspec.yaml": dartPubspec,
				".fvmrc":       `{"flutter": "3.38.0"}`,
			}).
			WithExistingFiles(map[string]bool{"pubspec.yaml": true, ".fvmrc": true}).
			BuildSpy()
		updater := dartUpdater.NewUpdaterRepositoryForTest(
			&repositorydoubles.StubVersionFetcher{Version: "3.13.0"},
			&repositorydoubles.StubVersionFetcher{Version: "3.47.0"},
		)

		// when
		vCtx := updater.ResolveVersionContextForTest(t.Context(), provider, repo)

		// then
		assert.Equal(t, "dart", vCtx.Toolchain)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-dart-deps", vCtx.BranchName)
	})

	t.Run("should upgrade the .fvmrc pin when the manifest is a Flutter one", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFileContents(map[string]string{
				"pubspec.yaml": flutterPubspec,
				".fvmrc":       `{"flutter": "3.38.0"}`,
			}).
			WithExistingFiles(map[string]bool{"pubspec.yaml": true, ".fvmrc": true}).
			BuildSpy()
		updater := dartUpdater.NewUpdaterRepositoryForTest(
			&repositorydoubles.StubVersionFetcher{Version: "3.13.0"},
			&repositorydoubles.StubVersionFetcher{Version: "3.47.0"},
		)

		// when
		vCtx := updater.ResolveVersionContextForTest(t.Context(), provider, repo)

		// then
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "3.47.0", vCtx.LatestVersion)
		assert.Equal(t, "chore/upgrade-flutter-3.47.0", vCtx.BranchName)
	})

	t.Run("should still upgrade dependencies when the release channel is unreachable", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithFileContents(map[string]string{"pubspec.yaml": dartPubspec}).
			BuildSpy()
		updater := dartUpdater.NewUpdaterRepositoryForTest(
			&repositorydoubles.StubVersionFetcher{Err: assert.AnError},
			&repositorydoubles.StubVersionFetcher{Err: assert.AnError},
		)

		// when
		vCtx := updater.ResolveVersionContextForTest(t.Context(), provider, repo)

		// then
		assert.Empty(t, vCtx.LatestVersion)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-dart-deps", vCtx.BranchName)
	})
}

// writeDartRepo lays out a repository on disk with a manifest and, optionally,
// an .fvmrc pin.
func writeDartRepo(t *testing.T, pubspec, fvmrc string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pubspec.yaml"), []byte(pubspec), 0o600))
	if fvmrc != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".fvmrc"), []byte(fvmrc), 0o600))
	}
	return dir
}

func TestResolveLocalVersionContext(t *testing.T) {
	t.Parallel()

	// The local counterpart of the remote guard: an .fvmrc found next to a
	// plain Dart manifest names a Flutter SDK the Dart channel knows nothing
	// about, so it is left alone.
	t.Run("should ignore an .fvmrc pin when the manifest is a plain Dart package", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := writeDartRepo(t, dartPubspec, `{"flutter": "3.38.0"}`)
		updater := dartUpdater.NewUpdaterRepositoryForTest(
			&repositorydoubles.StubVersionFetcher{Version: "3.13.0"},
			&repositorydoubles.StubVersionFetcher{Version: "3.47.0"},
		)

		// when
		vCtx := updater.ResolveLocalVersionContextForTest(t.Context(), repoDir)

		// then
		assert.Equal(t, "dart", vCtx.Toolchain)
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-dart-deps", vCtx.BranchName)
	})

	t.Run("should upgrade the .fvmrc pin when the manifest is a Flutter one", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := writeDartRepo(t, flutterPubspec, `{"flutter": "3.38.0"}`)
		updater := dartUpdater.NewUpdaterRepositoryForTest(
			&repositorydoubles.StubVersionFetcher{Version: "3.13.0"},
			&repositorydoubles.StubVersionFetcher{Version: "3.47.0"},
		)

		// when
		vCtx := updater.ResolveLocalVersionContextForTest(t.Context(), repoDir)

		// then
		assert.Equal(t, "flutter", vCtx.Toolchain)
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-flutter-3.47.0", vCtx.BranchName)
	})
}

func TestNewVersionContext(t *testing.T) {
	t.Parallel()

	t.Run("should request an SDK upgrade when the pin is behind", func(t *testing.T) {
		t.Parallel()

		// given / when
		vCtx := dartUpdater.NewVersionContext("flutter", "3.47.0", "3.38.0")

		// then
		assert.True(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-flutter-3.47.0", vCtx.BranchName)
	})

	t.Run("should not request an SDK upgrade when the pin is current", func(t *testing.T) {
		t.Parallel()

		// given / when
		vCtx := dartUpdater.NewVersionContext("flutter", "3.47.0", "3.47.0")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-dart-deps", vCtx.BranchName)
	})

	t.Run("should not request an SDK upgrade when the repository pins none", func(t *testing.T) {
		t.Parallel()

		// given — no .fvmrc, so there is nothing to bump and nothing to break
		vCtx := dartUpdater.NewVersionContext("dart", "3.13.0", "")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-dart-deps", vCtx.BranchName)
	})
}

func TestChangelogEntries(t *testing.T) {
	t.Parallel()

	t.Run("should describe the SDK upgrade when the pin moved", func(t *testing.T) {
		t.Parallel()

		// given
		vCtx := dartUpdater.NewVersionContext("flutter", "3.47.0", "3.38.0")

		// when
		entries := dartUpdater.ChangelogEntries(vCtx, true)

		// then
		require.Len(t, entries, 1)
		assert.Contains(t, entries[0], "3.47.0")
		assert.Contains(t, entries[0], "pub dependencies")
	})

	t.Run("should describe a dependency-only upgrade otherwise", func(t *testing.T) {
		t.Parallel()

		// given
		vCtx := dartUpdater.NewVersionContext("dart", "3.13.0", "")

		// when
		entries := dartUpdater.ChangelogEntries(vCtx, false)

		// then
		require.Len(t, entries, 1)
		assert.Equal(t, "- changed the Dart pub dependencies to their latest versions", entries[0])
	})
}

func TestBuildBatchDartScript(t *testing.T) {
	t.Parallel()

	t.Run("should produce a valid bash script running pub through the chosen toolchain", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := dartUpdater.BuildBatchDartScript(true)

		// then
		assert.True(t, strings.HasPrefix(script, "#!/bin/bash\n"))
		assert.Contains(t, script, "set -euo pipefail")
		assert.Contains(t, script, `PUB="${PUB_EXECUTABLE:-dart}"`)
		assert.Contains(t, script, "pub upgrade --major-versions")
		assert.Contains(t, script, "pub get")
	})

	t.Run("should route pub through fvm when the repository pins an SDK", func(t *testing.T) {
		t.Parallel()

		// given / when — fvm installs the SDK per project, so a pinned repository
		// must not be driven by whatever toolchain happens to be on PATH
		script := dartUpdater.BuildBatchDartScript(true)

		// then
		assert.Contains(t, script, `if [ -f ".fvmrc" ] && command -v fvm > /dev/null 2>&1; then`)
		assert.Contains(t, script, `PUB="fvm $PUB"`)
	})

	t.Run("should keep going when pub reports errors", func(t *testing.T) {
		t.Parallel()

		// given / when — one unresolvable package must not abort the whole run
		script := dartUpdater.BuildBatchDartScript(true)

		// then
		assert.Contains(t, script, `|| echo "WARNING: pub upgrade had some errors (continuing anyway)"`)
		assert.Contains(t, script, `|| echo "WARNING: pub get had some errors (continuing anyway)"`)
	})
}

func TestBuildUpgradeScript(t *testing.T) {
	t.Parallel()

	t.Run("should clone, branch, upgrade and push", func(t *testing.T) {
		t.Parallel()

		// given
		params := dartUpdater.UpgradeParamsExported{
			CloneURL:      "https://github.com/org/repo.git",
			DefaultBranch: "main",
			BranchName:    "chore/upgrade-dart-deps",
			ProviderName:  "github",
			Toolchain:     "flutter",
			// The production default; without it the params zero value refuses
			// majors and pub is asked for a plain re-resolution instead.
			AllowMajorUpdates: true,
		}

		// when
		script := dartUpdater.BuildUpgradeScript(params)

		// then
		assert.Contains(t, script, "git clone --depth=1")
		assert.Contains(t, script, `git checkout -b "$BRANCH_NAME"`)
		assert.Contains(t, script, "pub upgrade --major-versions")
		assert.Contains(t, script, "CHANGES_PUSHED=true")
	})
}

func TestWriteGitAuth(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ provider, expected string }{
		{"github", "x-access-token"},
		{"gitlab", "oauth2"},
		{"azuredevops", "dev.azure.com"},
	} {
		t.Run("should configure credentials for "+tc.provider, func(t *testing.T) {
			t.Parallel()

			// given
			var sb strings.Builder

			// when
			dartUpdater.WriteGitAuth(&sb, dartUpdater.UpgradeParamsExported{ProviderName: tc.provider})

			// then
			assert.Contains(t, sb.String(), tc.expected)
			assert.Contains(t, sb.String(), "GIT_CONFIG_GLOBAL")
		})
	}
}

func TestBuildEnv(t *testing.T) {
	t.Parallel()

	t.Run("should pass the toolchain to the script", func(t *testing.T) {
		t.Parallel()

		// given
		params := dartUpdater.UpgradeParamsExported{BranchName: "b", Toolchain: "flutter"}

		// when
		env := dartUpdater.BuildEnv(params, "/tmp/repo")

		// then
		assert.Contains(t, env, "PUB_EXECUTABLE=flutter")
		assert.Contains(t, env, "REPO_DIR=/tmp/repo")
	})

	t.Run("should omit the SDK target when no pin needs bumping", func(t *testing.T) {
		t.Parallel()

		// given
		params := dartUpdater.UpgradeParamsExported{Toolchain: "dart"}

		// when
		env := dartUpdater.BuildEnv(params, "/tmp/repo")

		// then
		for _, entry := range env {
			assert.False(t, strings.HasPrefix(entry, "TARGET_SDK_VERSION="))
		}
	})
}

func TestGeneratePRDescription(t *testing.T) {
	t.Parallel()

	t.Run("should name the SDK bump when one happened", func(t *testing.T) {
		t.Parallel()

		// given / when
		desc := dartUpdater.GeneratePRDescription("3.47.0", "flutter", true, true)

		// then
		assert.Contains(t, desc, "3.47.0")
		assert.Contains(t, desc, ".fvmrc")
		assert.Contains(t, desc, "flutter pub upgrade --major-versions")
	})

	t.Run("should describe a dependency-only upgrade otherwise", func(t *testing.T) {
		t.Parallel()

		// given / when
		desc := dartUpdater.GeneratePRDescription("", "dart", false, true)

		// then
		assert.NotContains(t, desc, ".fvmrc")
		assert.Contains(t, desc, "dart pub upgrade --major-versions")
		assert.Contains(t, desc, "Review Checklist")
	})
}

// TestDartMajorMode covers what `allow_major_updates` changes for Dart, which is
// the one ecosystem where the key previously did the opposite of what it said:
// `--major-versions` is precisely a request to raise constraints across majors,
// and it was passed unconditionally.
func TestDartMajorMode(t *testing.T) {
	t.Parallel()

	t.Run("should ask pub for major versions when allowed", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := dartUpdater.BuildBatchDartScript(true)

		// then
		assert.Contains(t, script, "pub upgrade --major-versions")
	})

	t.Run("should re-resolve within constraints when refused", func(t *testing.T) {
		t.Parallel()

		// given / when
		script := dartUpdater.BuildBatchDartScript(false)

		// then -- a plain `pub upgrade` rewrites pubspec.lock inside the declared
		// constraints, which is what holding the current major line means here
		assert.Contains(t, script, "pub upgrade")
		assert.NotContains(t, script, "--major-versions")
	})
}
