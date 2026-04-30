//go:build unit

package entities_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

func TestRepoKey(t *testing.T) {
	t.Parallel()

	t.Run("should join organization and name for GitHub-style repos", func(t *testing.T) {
		t.Parallel()

		// given
		repo := entities.Repository{Organization: "rios0rios0", Name: "autoupdate"}

		// when
		key := entities.RepoKey(repo)

		// then
		assert.Equal(t, "rios0rios0/autoupdate", key)
	})

	t.Run("should include project segment for Azure DevOps repos", func(t *testing.T) {
		t.Parallel()

		// given
		repo := entities.Repository{
			Organization: "ZestSecurity",
			Project:      "frontend",
			Name:         "opensearch-dashboards",
		}

		// when
		key := entities.RepoKey(repo)

		// then
		assert.Equal(t, "zestsecurity/frontend/opensearch-dashboards", key)
	})

	t.Run("should lowercase every segment for case-insensitive matching", func(t *testing.T) {
		t.Parallel()

		// given
		repo := entities.Repository{Organization: "MyOrg", Name: "MyRepo"}

		// when
		key := entities.RepoKey(repo)

		// then
		assert.Equal(t, "myorg/myrepo", key)
	})
}

func TestMatchesExcludePattern(t *testing.T) {
	t.Parallel()

	githubRepo := entities.Repository{Organization: "rios0rios0", Name: "autoupdate"}
	adoRepo := entities.Repository{
		Organization: "ZestSecurity",
		Project:      "frontend",
		Name:         "opensearch-dashboards",
	}

	t.Run("should return false for empty pattern list", func(t *testing.T) {
		t.Parallel()

		// given
		patterns := []string{}

		// when
		matched, pattern := entities.MatchesExcludePattern(githubRepo, patterns)

		// then
		assert.False(t, matched)
		assert.Empty(t, pattern)
	})

	t.Run("should match exact org/repo patterns", func(t *testing.T) {
		t.Parallel()

		// given
		patterns := []string{"rios0rios0/autoupdate"}

		// when
		matched, pattern := entities.MatchesExcludePattern(githubRepo, patterns)

		// then
		assert.True(t, matched)
		assert.Equal(t, "rios0rios0/autoupdate", pattern)
	})

	t.Run("should match Azure DevOps three-segment paths", func(t *testing.T) {
		t.Parallel()

		// given
		patterns := []string{"ZestSecurity/frontend/opensearch-dashboards"}

		// when
		matched, pattern := entities.MatchesExcludePattern(adoRepo, patterns)

		// then
		assert.True(t, matched)
		assert.Equal(t, "ZestSecurity/frontend/opensearch-dashboards", pattern)
	})

	t.Run("should support glob wildcards on the org segment", func(t *testing.T) {
		t.Parallel()

		// given
		patterns := []string{"*/oui"}
		repo := entities.Repository{Organization: "ZestSecurity", Name: "oui"}

		// when
		matched, pattern := entities.MatchesExcludePattern(repo, patterns)

		// then
		assert.True(t, matched)
		assert.Equal(t, "*/oui", pattern)
	})

	t.Run("should support glob wildcards spanning project and repo", func(t *testing.T) {
		t.Parallel()

		// given
		patterns := []string{"zestsecurity/frontend/*"}

		// when
		matched, pattern := entities.MatchesExcludePattern(adoRepo, patterns)

		// then
		assert.True(t, matched)
		assert.Equal(t, "zestsecurity/frontend/*", pattern)
	})

	t.Run("should match a bare repo name against the trailing segment", func(t *testing.T) {
		t.Parallel()

		// given
		patterns := []string{"opensearch-dashboards"}

		// when
		matched, pattern := entities.MatchesExcludePattern(adoRepo, patterns)

		// then
		assert.True(t, matched)
		assert.Equal(t, "opensearch-dashboards", pattern)
	})

	t.Run("should not match a partial repo name (right-anchored match only)", func(t *testing.T) {
		t.Parallel()

		// given
		patterns := []string{"dashboards"}

		// when
		matched, _ := entities.MatchesExcludePattern(adoRepo, patterns)

		// then
		assert.False(t, matched, "use *dashboards or opensearch-dashboards instead")
	})

	t.Run("should be case-insensitive across patterns and keys", func(t *testing.T) {
		t.Parallel()

		// given
		patterns := []string{"RIOS0RIOS0/AUTOUPDATE"}

		// when
		matched, _ := entities.MatchesExcludePattern(githubRepo, patterns)

		// then
		assert.True(t, matched)
	})

	t.Run("should ignore blank entries in the pattern list", func(t *testing.T) {
		t.Parallel()

		// given
		patterns := []string{"", "   ", "rios0rios0/autoupdate"}

		// when
		matched, pattern := entities.MatchesExcludePattern(githubRepo, patterns)

		// then
		assert.True(t, matched)
		assert.Equal(t, "rios0rios0/autoupdate", pattern)
	})

	t.Run("should return first matching pattern when multiple apply", func(t *testing.T) {
		t.Parallel()

		// given
		patterns := []string{"*/autoupdate", "rios0rios0/autoupdate"}

		// when
		_, pattern := entities.MatchesExcludePattern(githubRepo, patterns)

		// then
		assert.Equal(t, "*/autoupdate", pattern)
	})

	t.Run("should not match unrelated repo", func(t *testing.T) {
		t.Parallel()

		// given
		patterns := []string{"someorg/somerepo"}

		// when
		matched, _ := entities.MatchesExcludePattern(githubRepo, patterns)

		// then
		assert.False(t, matched)
	})
}

func TestSettingsIsRepoExcluded(t *testing.T) {
	t.Parallel()

	t.Run("should return false on nil receiver", func(t *testing.T) {
		t.Parallel()

		// given
		var settings *entities.Settings
		repo := entities.Repository{Organization: "x", Name: "y"}

		// when
		matched, _ := settings.IsRepoExcluded(repo)

		// then
		assert.False(t, matched)
	})

	t.Run("should return false when ExcludeRepos is empty", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{}
		repo := entities.Repository{Organization: "x", Name: "y"}

		// when
		matched, _ := settings.IsRepoExcluded(repo)

		// then
		assert.False(t, matched)
	})

	t.Run("should match against ExcludeRepos list", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{ExcludeRepos: []string{"x/y"}}
		repo := entities.Repository{Organization: "x", Name: "y"}

		// when
		matched, pattern := settings.IsRepoExcluded(repo)

		// then
		assert.True(t, matched)
		assert.Equal(t, "x/y", pattern)
	})
}
