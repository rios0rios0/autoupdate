package support

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	logger "github.com/sirupsen/logrus"
)

// MavenRepositoryVariable names the local Maven repository a batch run gives each
// repository. Maven has no environment variable of its own for it, so the Java
// upgrade script passes it as `-Dmaven.repo.local` whenever it is set -- an
// argument rather than MAVEN_OPTS, which the `mvn` launcher word-splits, so a
// temporary directory with a space in its path cannot break it.
const MavenRepositoryVariable = "AUTOUPDATE_MAVEN_REPOSITORY"

// toolingTempDir is the TMPDIR every script runs with, and the one directory
// prepareToolingDir creates up front: `mktemp` fails in a directory that does not
// exist, while every cache below is created by its own tool on first use.
const toolingTempDir = "tmp"

// toolingDirMode keeps the tooling directory owner-only, like the [os.MkdirTemp]
// root it lives in.
const toolingDirMode = 0o700

// toolingRedirect points one package-manager variable at a directory under the
// repository's tooling directory.
type toolingRedirect struct {
	variable string
	dir      string
}

// toolingRedirects moves everything a package manager run by an updater writes
// outside the repository -- caches, downloaded toolchains, installed packages and
// temporary files -- into the repository's tooling directory, where it is removed
// together with the clone once the repository is done.
//
// Without it, every repository a batch run touches leaves its dependencies behind
// in the operator's home, and the run grows the disk by the union of what an
// entire organization depends on: the Go module and build caches (the compile
// guard's `go vet` fills the latter on every Go repository), every Gradle
// distribution a wrapper names, every npm, pip, pub, NuGet and gem package. A
// batch over a hundred repositories fills a CI agent long before it finishes.
//
// Every entry moves a cache or a download, never configuration: `~/.npmrc`,
// `~/.m2/settings.xml`, `pip.conf`, `NuGet.Config`, `~/.bundle/config`, `.netrc`
// and `go env -w` are still read from where the operator put them, which is why
// HOME and XDG_CONFIG_HOME are left alone. GRADLE_USER_HOME is the one directory
// that mixes the two, so prepareToolingDir carries its configuration over.
//
// Each variable is set explicitly even where XDG_CACHE_HOME would already move the
// default, because a CI pipeline commonly presets PIP_CACHE_DIR or GOCACHE to a
// cache of its own, and a preset value would otherwise keep growing.
//
// Two variables are deliberately absent. YARN_CACHE_FOLDER would pull a Yarn
// Berry project's committed `.yarn/cache` out of the repository, so a zero-install
// project's pull request would update the lockfile without the archives its CI
// installs from; Classic's cache follows XDG_CACHE_HOME and Berry's follows
// YARN_GLOBAL_FOLDER instead. And npm_config_store_dir, pnpm's older spelling,
// makes npm warn about an unknown setting on every command; PNPM_HOME moves the
// same store on every pnpm version.
var toolingRedirects = []toolingRedirect{ //nolint:gochecknoglobals // read-only lookup table
	// Temporary files: the virtual environment the Python script builds with
	// `mktemp -d`, and the build directories npm, pip and Go clean up only when
	// they are not interrupted.
	{variable: "TMPDIR", dir: toolingTempDir},
	// Anything that follows the XDG cache directory without a variable of its own:
	// corepack's package managers, node-gyp's headers, and the browsers Puppeteer,
	// Playwright and Cypress download from an `npm install` postinstall script.
	{variable: "XDG_CACHE_HOME", dir: "cache"},

	// Go: the module cache, which also holds every toolchain GOTOOLCHAIN
	// downloads, and the build cache.
	{variable: "GOMODCACHE", dir: "go/mod"},
	{variable: "GOCACHE", dir: "go/build"},

	// JavaScript.
	{variable: "npm_config_cache", dir: "npm"},
	{variable: "PNPM_HOME", dir: "pnpm"},
	{variable: "YARN_GLOBAL_FOLDER", dir: "yarn"},
	{variable: "COREPACK_HOME", dir: "corepack"},

	// Python.
	{variable: "PIP_CACHE_DIR", dir: "pip"},
	{variable: "PDM_CACHE_DIR", dir: "pdm"},

	// Java.
	{variable: "GRADLE_USER_HOME", dir: gradleHomeDir},
	{variable: MavenRepositoryVariable, dir: "maven"},

	// Dart and Flutter.
	{variable: "PUB_CACHE", dir: "pub"},

	// .NET.
	{variable: "NUGET_PACKAGES", dir: "nuget/packages"},
	{variable: "NUGET_HTTP_CACHE_PATH", dir: "nuget/http-cache"},
	{variable: "NUGET_PLUGINS_CACHE_PATH", dir: "nuget/plugins-cache"},

	// Ruby: the gems `bundle update` installs, and bundler's own cache.
	{variable: "BUNDLE_PATH", dir: "bundle"},
	{variable: "BUNDLE_USER_CACHE", dir: "bundle-cache"},
}

// toolingSettings are fixed values every script runs with, so that no process a
// package manager starts outlives the repository it was started for. MSBuild
// otherwise keeps its worker nodes alive after `dotnet restore` and `dotnet add
// package` return, waiting for a next build that, in a batch run, never comes.
// The Gradle and Kotlin daemons are the same problem and are handled where Gradle
// is invoked, with `--no-daemon`.
var toolingSettings = []string{ //nolint:gochecknoglobals // read-only lookup table
	"MSBUILDDISABLENODEREUSE=1",
}

// gradleHomeDir is where GRADLE_USER_HOME points under the tooling directory.
const gradleHomeDir = "gradle"

// gradleConfigEntries are what the operator's Gradle home holds besides caches:
// the properties carrying the JDK, the proxy and repository credentials, the init
// scripts that point every build at a mirror, and the Develocity access keys.
// Redirecting GRADLE_USER_HOME without them would leave a build behind a
// corporate proxy unable to download anything.
var gradleConfigEntries = []string{ //nolint:gochecknoglobals // read-only lookup table
	"gradle.properties",
	"init.gradle",
	"init.gradle.kts",
	"init.d",
	"develocity",
	"enterprise",
}

// ScriptEnv returns the environment an upgrade script starts from: the process
// environment, with every package manager's caches, downloads and temporary
// files moved into toolingDir. The caller appends its own variables, which win
// over these because exec keeps the last value of a duplicated key.
//
// An empty toolingDir returns the process environment unchanged, so a caller that
// was given no workspace runs exactly as it did before there was one. Standalone
// mode never gets here: it upgrades the operator's own checkout once, through its
// own environment, and is better served by the caches already on their machine.
func ScriptEnv(toolingDir string) []string {
	env := os.Environ()
	if toolingDir == "" {
		return env
	}

	for _, redirect := range toolingRedirects {
		env = append(env, redirect.variable+"="+filepath.Join(toolingDir, redirect.dir))
	}
	return append(env, toolingSettings...)
}

// prepareToolingDir lays out what ScriptEnv points at and cannot be created on
// first use: the temporary directory, and the operator's Gradle configuration.
func prepareToolingDir(toolingDir string) error {
	// A directory needs the owner search bit, so 0o700 is the least-privilege mode
	// one can be created with; the rule compares it against 0600 regardless.
	// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
	if err := os.MkdirAll(filepath.Join(toolingDir, toolingTempDir), toolingDirMode); err != nil {
		return fmt.Errorf("failed to create the tooling directory: %w", err)
	}

	gradleHome := filepath.Join(toolingDir, gradleHomeDir)
	// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
	if err := os.MkdirAll(gradleHome, toolingDirMode); err != nil {
		return fmt.Errorf("failed to create the Gradle home: %w", err)
	}
	linkGradleConfiguration(operatorGradleHome(), gradleHome)
	return nil
}

// operatorGradleHome is the Gradle home the operator's own builds use: the one
// GRADLE_USER_HOME names, or `~/.gradle`. It is absolute, because a link resolves
// a relative target against the directory the link is in, not against this one.
func operatorGradleHome() string {
	if home := os.Getenv("GRADLE_USER_HOME"); home != "" {
		absolute, err := filepath.Abs(home)
		if err != nil {
			return ""
		}
		return absolute
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(userHome, ".gradle")
}

// linkGradleConfiguration makes the operator's Gradle configuration visible in the
// repository's Gradle home. The entries are linked rather than copied, so no
// credential is duplicated into a temporary directory, and the removal deletes the
// links without following them. A link that cannot be made is reported and
// skipped: the build then runs as it would on a machine with no configuration.
func linkGradleConfiguration(source, target string) {
	if source == "" {
		return
	}
	for _, name := range gradleConfigEntries {
		original := filepath.Join(source, name)
		if _, err := os.Stat(original); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				logger.Warnf("Could not read the Gradle configuration %s: %v", original, err)
			}
			continue
		}
		if err := os.Symlink(original, filepath.Join(target, name)); err != nil {
			logger.Warnf("Could not carry the Gradle configuration %s over to the repository: %v", original, err)
		}
	}
}
