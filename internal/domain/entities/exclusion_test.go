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
			Organization: "ContosoSecurity",
			Project:      "frontend",
			Name:         "opensearch-dashboards",
		}

		// when
		key := entities.RepoKey(repo)

		// then
		assert.Equal(t, "contososecurity/frontend/opensearch-dashboards", key)
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
		Organization: "ContosoSecurity",
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
		patterns := []string{"ContosoSecurity/frontend/opensearch-dashboards"}

		// when
		matched, pattern := entities.MatchesExcludePattern(adoRepo, patterns)

		// then
		assert.True(t, matched)
		assert.Equal(t, "ContosoSecurity/frontend/opensearch-dashboards", pattern)
	})

	t.Run("should support glob wildcards on the org segment", func(t *testing.T) {
		t.Parallel()

		// given
		patterns := []string{"*/oui"}
		repo := entities.Repository{Organization: "ContosoSecurity", Name: "oui"}

		// when
		matched, pattern := entities.MatchesExcludePattern(repo, patterns)

		// then
		assert.True(t, matched)
		assert.Equal(t, "*/oui", pattern)
	})

	t.Run("should support glob wildcards spanning project and repo", func(t *testing.T) {
		t.Parallel()

		// given
		patterns := []string{"contososecurity/frontend/*"}

		// when
		matched, pattern := entities.MatchesExcludePattern(adoRepo, patterns)

		// then
		assert.True(t, matched)
		assert.Equal(t, "contososecurity/frontend/*", pattern)
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

func TestExcludesSelf(t *testing.T) {
	t.Parallel()

	// filterRepositories runs these same predicates organization-wide, before any
	// repository's own file has been read. This is the second pass the project layer
	// earns -- and the only place a repository's own exclude_* keys can mean anything.
	t.Run("should exclude a fork when the settings say so", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{ExcludeForks: true}
		repo := entities.Repository{Organization: "org", Name: "repo", IsFork: true}

		// when
		excluded, rule := entities.ExcludesSelf(settings, repo)

		// then
		assert.True(t, excluded)
		assert.Equal(t, "exclude_forks", rule)
	})

	t.Run("should exclude an archived repository when the settings say so", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{ExcludeArchived: true}
		repo := entities.Repository{Organization: "org", Name: "repo", IsArchived: true}

		// when
		excluded, rule := entities.ExcludesSelf(settings, repo)

		// then
		assert.True(t, excluded)
		assert.Equal(t, "exclude_archived", rule)
	})

	t.Run("should name the pattern that matched", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{ExcludeRepos: []string{"*/sandbox"}}
		repo := entities.Repository{Organization: "org", Name: "sandbox"}

		// when
		excluded, rule := entities.ExcludesSelf(settings, repo)

		// then
		assert.True(t, excluded)
		assert.Contains(t, rule, "*/sandbox")
	})

	t.Run("should not exclude a repository nothing matches", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{ExcludeForks: true, ExcludeArchived: true}
		repo := entities.Repository{Organization: "org", Name: "repo"}

		// when
		excluded, rule := entities.ExcludesSelf(settings, repo)

		// then
		assert.False(t, excluded)
		assert.Empty(t, rule)
	})

	t.Run("should tolerate nil settings", func(t *testing.T) {
		t.Parallel()

		// when
		excluded, rule := entities.ExcludesSelf(nil, entities.Repository{Name: "repo"})

		// then
		assert.False(t, excluded)
		assert.Empty(t, rule)
	})
}
