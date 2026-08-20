//go:build unit

package ruby_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	rbUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/ruby"
	"github.com/rios0rios0/autoupdate/test/infrastructure/repositorydoubles"
	"github.com/rios0rios0/autoupdate/test/infrastructure/scriptrunner"
)

// downgradeRepo is the repository every case in this file resolves against.
var downgradeRepo = entities.Repository{ //nolint:gochecknoglobals // shared test fixture
	Organization: "org",
	Name:         "repo",
}

func runRubyVersionBlock(t *testing.T, repoDir, rubyVersion string) string {
	t.Helper()

	var sb strings.Builder
	rbUpdater.WriteRubyUpgradeCommands(&sb)

	return scriptrunner.Run(t, repoDir, sb.String(), scriptrunner.Options{
		Env:   map[string]string{"TARGET_RUBY_VERSION": rubyVersion},
		Stubs: []string{"gem", "bundle", "bundler", "ruby"},
	})
}

func TestRubyVersionPinIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	t.Run("should keep the pin when the repository runs a newer Ruby than the latest stable", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, ".ruby-version", "3.5.0\n")

		// when
		output := runRubyVersionBlock(t, repoDir, "3.4.1")

		// then
		assert.Equal(t, "3.5.0", scriptrunner.Trimmed(t, repoDir, ".ruby-version"))
		assert.Contains(t, output, "RUBY_VERSION_UPDATED=false")
	})

	t.Run("should raise the pin when the repository is behind the latest stable", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, ".ruby-version", "3.2.2\n")

		// when
		output := runRubyVersionBlock(t, repoDir, "3.4.1")

		// then
		assert.Equal(t, "3.4.1", scriptrunner.Trimmed(t, repoDir, ".ruby-version"))
		assert.Contains(t, output, "RUBY_VERSION_UPDATED=true")
	})

	t.Run("should keep a pin naming an implementation other than MRI", func(t *testing.T) {
		t.Parallel()

		// given — an MRI version number is not a replacement for a JRuby pin
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, ".ruby-version", "jruby-9.4.9.0\n")

		// when
		output := runRubyVersionBlock(t, repoDir, "3.4.1")

		// then
		assert.Equal(t, "jruby-9.4.9.0", scriptrunner.Trimmed(t, repoDir, ".ruby-version"))
		assert.Contains(t, output, "RUBY_VERSION_UPDATED=false")
	})
}

func TestRubyDockerfileTagIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	runDockerfileBlock := func(t *testing.T, repoDir, rubyVersion string) {
		t.Helper()

		var sb strings.Builder
		rbUpdater.WriteDockerfileUpdate(&sb)
		scriptrunner.Run(t, repoDir, sb.String(), scriptrunner.Options{
			Env: map[string]string{
				"TARGET_RUBY_VERSION":  rubyVersion,
				"RUBY_VERSION_CHANGED": "true",
			},
		})
	}

	t.Run("should keep a base image newer than the version being rolled out", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, "Dockerfile", "FROM ruby:3.5.0-slim\n")

		// when
		runDockerfileBlock(t, repoDir, "3.4.1")

		// then
		assert.Contains(t, scriptrunner.ReadFile(t, repoDir, "Dockerfile"), "FROM ruby:3.5.0-slim")
	})

	t.Run("should upgrade a base image older than the version being rolled out", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := t.TempDir()
		scriptrunner.WriteFile(t, repoDir, "Dockerfile", "FROM ruby:3.2.2-slim\n")

		// when
		runDockerfileBlock(t, repoDir, "3.4.1")

		// then
		assert.Contains(t, scriptrunner.ReadFile(t, repoDir, "Dockerfile"), "FROM ruby:3.4.1-slim")
	})
}

func TestRubyVersionContextIsNeverADowngrade(t *testing.T) {
	t.Parallel()

	t.Run("should report no version upgrade when the pin is ahead of the latest stable", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".ruby-version": true}).
			WithFileContents(map[string]string{".ruby-version": "3.5.0\n"}).
			BuildSpy()

		// when
		vCtx := rbUpdater.ResolveVersionContext(t.Context(), provider, downgradeRepo, "3.4.1")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-ruby-deps", vCtx.BranchName)
	})

	t.Run("should report a version upgrade when the pin is behind the latest stable", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.NewSpyProviderRepositoryBuilder().
			WithExistingFiles(map[string]bool{".ruby-version": true}).
			WithFileContents(map[string]string{".ruby-version": "3.2.2\n"}).
			BuildSpy()

		// when
		vCtx := rbUpdater.ResolveVersionContext(t.Context(), provider, downgradeRepo, "3.4.1")

		// then
		assert.True(t, vCtx.NeedsVersionUpgrade)
	})
}
