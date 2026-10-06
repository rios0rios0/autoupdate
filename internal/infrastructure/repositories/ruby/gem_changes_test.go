package ruby_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	rbUpdater "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/ruby"
	"github.com/rios0rios0/autoupdate/test/infrastructure/gitrepo"
)

// gemfileLock is a Gemfile.lock in the shape bundler writes: gems from
// RubyGems and from git, a native gem built for two platforms, and the gems
// the Gemfile names under DEPENDENCIES.
const gemfileLock = `GIT
  remote: https://github.com/org/internal_gem.git
  revision: abc123
  specs:
    internal_gem (0.4.0)

GEM
  remote: https://rubygems.org/
  specs:
    actionpack (7.1.0)
      rack (>= 2.2.4)
    nokogiri (1.16.0-arm64-darwin)
      racc (~> 1.4)
    nokogiri (1.16.0-x86_64-linux)
      racc (~> 1.4)
    rack (3.0.8)
    rails (7.1.0)
      actionpack (= 7.1.0)

PLATFORMS
  arm64-darwin
  x86_64-linux

DEPENDENCIES
  internal_gem!
  nokogiri
  rails (~> 7.1)

BUNDLED WITH
   2.5.4
`

func TestLockedGemVersions(t *testing.T) {
	t.Parallel()

	t.Run("should read the gems the Gemfile names, without their platform", func(t *testing.T) {
		t.Parallel()

		// given / when
		versions, err := rbUpdater.LockedGemVersions([]byte(gemfileLock))

		// then: actionpack and rack are resolved only because rails needs them
		require.NoError(t, err)
		assert.Equal(t, map[string]string{
			"internal_gem": "0.4.0",
			"nokogiri":     "1.16.0",
			"rails":        "7.1.0",
		}, versions)
	})

	t.Run("should fail on a lock whose gems it cannot read", func(t *testing.T) {
		t.Parallel()

		// given
		lock := "GEM\n  specs:\n    rails 7.1.0\n\nDEPENDENCIES\n  rails\n"

		// when
		versions, err := rbUpdater.LockedGemVersions([]byte(lock))

		// then
		require.Error(t, err)
		assert.Nil(t, versions)
	})
}

func TestObserveGemChanges(t *testing.T) {
	t.Parallel()

	t.Run("should report the gems and the Ruby version a run moved", func(t *testing.T) {
		t.Parallel()

		// given
		root := gitrepo.New(t, map[string]string{"Gemfile.lock": gemfileLock, ".ruby-version": "3.3.0\n"})
		gitrepo.Write(t, root, map[string]string{
			"Gemfile.lock": strings.NewReplacer(
				"rails (7.1.0)", "rails (7.2.1)",
				"rack (3.0.8)", "rack (3.1.7)",
			).Replace(gemfileLock),
			".ruby-version": "3.3.5\n",
		})

		// when
		changes, err := rbUpdater.ObserveGemChanges(t.Context(), root)

		// then
		require.NoError(t, err)
		assert.ElementsMatch(t, []entities.DependencyChange{
			{Subject: entities.SubjectRubyGem, Name: "rails", From: "7.1.0", To: "7.2.1"},
			{Subject: entities.SubjectRubyVersion, From: "3.3.0", To: "3.3.5"},
		}, changes)
	})
}
