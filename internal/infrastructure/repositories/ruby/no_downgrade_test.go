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

func TestRubyVersionPinIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	cases := []scriptrunner.PinCase{
		{
			Name:   "keep the pin when the repository runs a newer Ruby than the latest stable",
			File:   ".ruby-version",
			Pinned: "3.5.0", Want: "3.5.0",
			Marker: "RUBY_VERSION_UPDATED=false",
		},
		{
			Name:   "raise the pin when the repository is behind the latest stable",
			File:   ".ruby-version",
			Pinned: "3.2.2", Want: "3.4.1",
			Marker: "RUBY_VERSION_UPDATED=true",
		},
		{
			// An MRI version number is not a replacement for a JRuby pin.
			Name:   "keep a pin naming an implementation other than MRI",
			File:   ".ruby-version",
			Pinned: "jruby-9.4.9.0", Want: "jruby-9.4.9.0",
			Marker: "RUBY_VERSION_UPDATED=false",
		},
	}

	var sb strings.Builder
	rbUpdater.WriteRubyUpgradeCommands(&sb)
	opts := scriptrunner.Options{
		Env:   map[string]string{"TARGET_RUBY_VERSION": "3.4.1"},
		Stubs: []string{"gem", "bundle", "bundler", "ruby"},
	}
	for _, testCase := range cases {
		t.Run("should "+testCase.Name, func(t *testing.T) {
			t.Parallel()

			scriptrunner.AssertVersionPin(t, sb.String(), opts, testCase)
		})
	}
}

func TestRubyDockerfileTagIsNeverDowngraded(t *testing.T) {
	t.Parallel()

	cases := []scriptrunner.ImageCase{
		{
			Name:       "keep a base image newer than the version being rolled out",
			Dockerfile: "FROM ruby:3.5.0-slim\n",
			Want:       []string{"FROM ruby:3.5.0-slim"},
		},
		{
			Name:       "upgrade a base image older than the version being rolled out",
			Dockerfile: "FROM ruby:3.2.2-slim\n",
			Want:       []string{"FROM ruby:3.4.1-slim"},
		},
	}

	var sb strings.Builder
	rbUpdater.WriteDockerfileUpdate(&sb)
	opts := scriptrunner.Options{
		Env: map[string]string{"TARGET_RUBY_VERSION": "3.4.1", "RUBY_VERSION_CHANGED": "true"},
	}
	for _, testCase := range cases {
		t.Run("should "+testCase.Name, func(t *testing.T) {
			t.Parallel()

			scriptrunner.AssertDockerfileTags(t, sb.String(), opts, testCase)
		})
	}
}

func TestRubyVersionContextIsNeverADowngrade(t *testing.T) {
	t.Parallel()

	t.Run("should report no version upgrade when the pin is ahead of the latest stable", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.SpyProviderWithFile(".ruby-version", "3.5.0\n")

		// when
		vCtx := rbUpdater.ResolveVersionContext(t.Context(), provider, downgradeRepo, "3.4.1")

		// then
		assert.False(t, vCtx.NeedsVersionUpgrade)
		assert.Equal(t, "chore/upgrade-ruby-deps", vCtx.BranchName)
	})

	t.Run("should report a version upgrade when the pin is behind the latest stable", func(t *testing.T) {
		t.Parallel()

		// given
		provider := repositorydoubles.SpyProviderWithFile(".ruby-version", "3.2.2\n")

		// when
		vCtx := rbUpdater.ResolveVersionContext(t.Context(), provider, downgradeRepo, "3.4.1")

		// then
		assert.True(t, vCtx.NeedsVersionUpgrade)
	})
}
