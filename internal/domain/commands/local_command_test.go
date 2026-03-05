//go:build unit

package commands_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	langEntities "github.com/rios0rios0/langforge/pkg/domain/entities"
)

func TestParseRemoteURL(t *testing.T) {
	t.Parallel()

	t.Run("should parse GitHub SSH URL", func(t *testing.T) {
		t.Parallel()

		// given
		url := "git@github.com:myorg/myrepo.git"

		// when
		info, err := commands.ParseRemoteURL(url)

		// then
		require.NoError(t, err)
		assert.Equal(t, "github", info.ProviderType)
		assert.Equal(t, "myorg", info.Org)
		assert.Equal(t, "myrepo", info.RepoName)
	})

	t.Run("should parse GitHub HTTPS URL", func(t *testing.T) {
		t.Parallel()

		// given
		url := "https://github.com/myorg/myrepo.git"

		// when
		info, err := commands.ParseRemoteURL(url)

		// then
		require.NoError(t, err)
		assert.Equal(t, "github", info.ProviderType)
		assert.Equal(t, "myorg", info.Org)
		assert.Equal(t, "myrepo", info.RepoName)
	})

	t.Run("should parse GitLab SSH URL", func(t *testing.T) {
		t.Parallel()

		// given
		url := "git@gitlab.com:group/project.git"

		// when
		info, err := commands.ParseRemoteURL(url)

		// then
		require.NoError(t, err)
		assert.Equal(t, "gitlab", info.ProviderType)
		assert.Equal(t, "group", info.Org)
		assert.Equal(t, "project", info.RepoName)
	})

	t.Run("should parse GitLab HTTPS URL", func(t *testing.T) {
		t.Parallel()

		// given
		url := "https://gitlab.com/group/project.git"

		// when
		info, err := commands.ParseRemoteURL(url)

		// then
		require.NoError(t, err)
		assert.Equal(t, "gitlab", info.ProviderType)
		assert.Equal(t, "group", info.Org)
		assert.Equal(t, "project", info.RepoName)
	})

	t.Run("should parse Azure DevOps SSH URL", func(t *testing.T) {
		t.Parallel()

		// given
		url := "git@ssh.dev.azure.com:v3/myorg/myproject/myrepo"

		// when
		info, err := commands.ParseRemoteURL(url)

		// then
		require.NoError(t, err)
		assert.Equal(t, "azuredevops", info.ProviderType)
		assert.Equal(t, "myorg", info.Org)
		assert.Equal(t, "myproject", info.Project)
		assert.Equal(t, "myrepo", info.RepoName)
	})

	t.Run("should parse Azure DevOps HTTPS URL", func(t *testing.T) {
		t.Parallel()

		// given
		url := "https://dev.azure.com/myorg/myproject/_git/myrepo"

		// when
		info, err := commands.ParseRemoteURL(url)

		// then
		require.NoError(t, err)
		assert.Equal(t, "azuredevops", info.ProviderType)
		assert.Equal(t, "myorg", info.Org)
		assert.Equal(t, "myproject", info.Project)
		assert.Equal(t, "myrepo", info.RepoName)
	})

	t.Run("should return error for unsupported URL", func(t *testing.T) {
		t.Parallel()

		// given
		url := "https://custom-git.example.com/repo.git"

		// when
		info, err := commands.ParseRemoteURL(url)

		// then
		require.Error(t, err)
		assert.Nil(t, info)
		assert.Contains(t, err.Error(), "unsupported git remote URL")
	})

	t.Run("should return error for invalid Azure DevOps SSH URL", func(t *testing.T) {
		t.Parallel()

		// given
		url := "git@ssh.dev.azure.com:v3/incomplete"

		// when
		info, err := commands.ParseRemoteURL(url)

		// then
		require.Error(t, err)
		assert.Nil(t, info)
		assert.Contains(t, err.Error(), "unsupported remote URL format")
	})
}

func TestResolveTokenFromEnv(t *testing.T) {
	t.Parallel()

	t.Run("should return empty string for unknown provider", func(t *testing.T) {
		t.Parallel()

		// given
		provider := "unknown"

		// when
		result := commands.ResolveTokenFromEnv(provider)

		// then
		assert.Empty(t, result)
	})
}

func TestTokenEnvHint(t *testing.T) {
	t.Parallel()

	t.Run("should return GitHub env hint", func(t *testing.T) {
		t.Parallel()

		// given / when
		hint := commands.TokenEnvHint("github")

		// then
		assert.Contains(t, hint, "GITHUB_TOKEN")
	})

	t.Run("should return Azure DevOps env hint", func(t *testing.T) {
		t.Parallel()

		// given / when
		hint := commands.TokenEnvHint("azuredevops")

		// then
		assert.Contains(t, hint, "AZURE_DEVOPS_EXT_PAT")
	})

	t.Run("should return GitLab env hint", func(t *testing.T) {
		t.Parallel()

		// given / when
		hint := commands.TokenEnvHint("gitlab")

		// then
		assert.Contains(t, hint, "GITLAB_TOKEN")
	})

	t.Run("should return unknown for unrecognized provider", func(t *testing.T) {
		t.Parallel()

		// given / when
		hint := commands.TokenEnvHint("bitbucket")

		// then
		assert.Contains(t, hint, "unknown")
	})
}

func TestGeneratePRContent(t *testing.T) {
	t.Parallel()

	t.Run("should generate Go PR content without version update", func(t *testing.T) {
		t.Parallel()

		// given
		info := &commands.LocalPRInfoForTest{
			BranchName:     "chore/deps-update",
			LatestVersion:  "1.22.0",
			VersionUpdated: false,
			ProjectType:    langEntities.LanguageGo,
		}

		// when
		title, desc := commands.GeneratePRContent(info)

		// then
		assert.Equal(t, "chore(deps): update Go module dependencies", title)
		assert.NotEmpty(t, desc)
	})

	t.Run("should generate Go PR content with version update", func(t *testing.T) {
		t.Parallel()

		// given
		info := &commands.LocalPRInfoForTest{
			BranchName:     "chore/deps-update",
			LatestVersion:  "1.26.0",
			VersionUpdated: true,
			ProjectType:    langEntities.LanguageGo,
		}

		// when
		title, _ := commands.GeneratePRContent(info)

		// then
		assert.Contains(t, title, "1.26.0")
		assert.Contains(t, title, "upgraded Go version")
	})

	t.Run("should generate Python PR content", func(t *testing.T) {
		t.Parallel()

		// given
		info := &commands.LocalPRInfoForTest{
			BranchName:     "chore/deps-update",
			LatestVersion:  "3.12.0",
			VersionUpdated: true,
			ProjectType:    langEntities.LanguagePython,
		}

		// when
		title, _ := commands.GeneratePRContent(info)

		// then
		assert.Contains(t, title, "Python")
		assert.Contains(t, title, "3.12.0")
	})

	t.Run("should generate JavaScript PR content", func(t *testing.T) {
		t.Parallel()

		// given
		info := &commands.LocalPRInfoForTest{
			BranchName:     "chore/deps-update",
			LatestVersion:  "20.0.0",
			VersionUpdated: true,
			ProjectType:    langEntities.LanguageNode,
		}

		// when
		title, _ := commands.GeneratePRContent(info)

		// then
		assert.Contains(t, title, "Node.js")
		assert.Contains(t, title, "20.0.0")
	})

	t.Run("should generate default PR content for unknown project type", func(t *testing.T) {
		t.Parallel()

		// given
		info := &commands.LocalPRInfoForTest{
			BranchName:  "chore/deps-update",
			ProjectType: langEntities.LanguageUnknown,
		}

		// when
		title, desc := commands.GeneratePRContent(info)

		// then
		assert.Equal(t, "chore(deps): updated dependencies", title)
		assert.Equal(t, "Automated dependency update.", desc)
	})
}
