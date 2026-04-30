//go:build unit

package commands_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	infraRepos "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
	globalEntities "github.com/rios0rios0/gitforge/pkg/global/domain/entities"
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

func TestLocalUpgradeHandlers(t *testing.T) {
	t.Parallel()

	t.Run("should return a map containing Go, Node, and Python handlers", func(t *testing.T) {
		t.Parallel()

		// given / when
		handlers := commands.LocalUpgradeHandlers()

		// then
		assert.NotNil(t, handlers[langEntities.LanguageGo], "Go handler should be registered")
		assert.NotNil(t, handlers[langEntities.LanguageNode], "Node handler should be registered")
		assert.NotNil(t, handlers[langEntities.LanguagePython], "Python handler should be registered")
	})

	t.Run("should return nil handlers for unsupported languages", func(t *testing.T) {
		t.Parallel()

		// given / when
		handlers := commands.LocalUpgradeHandlers()

		// then
		assert.Nil(t, handlers[langEntities.LanguageJava], "Java handler should be nil")
		assert.Nil(t, handlers[langEntities.LanguageTerraform], "Terraform handler should be nil")
		assert.Nil(t, handlers[langEntities.LanguageCSharp], "CSharp handler should be nil")
		assert.Nil(t, handlers[langEntities.LanguageDockerfile], "Dockerfile handler should be nil")
		assert.Nil(t, handlers[langEntities.LanguageUnknown], "Unknown handler should be nil")
	})

	t.Run("should contain all langforge Language keys", func(t *testing.T) {
		t.Parallel()

		// given
		expectedLanguages := []langEntities.Language{
			langEntities.LanguageGo,
			langEntities.LanguageNode,
			langEntities.LanguagePython,
			langEntities.LanguageJava,
			langEntities.LanguageJavaGradle,
			langEntities.LanguageJavaMaven,
			langEntities.LanguageCSharp,
			langEntities.LanguageTerraform,
			langEntities.LanguageYAML,
			langEntities.LanguagePipeline,
			langEntities.LanguageDockerfile,
			langEntities.LanguageUnknown,
		}

		// when
		handlers := commands.LocalUpgradeHandlers()

		// then
		for _, lang := range expectedLanguages {
			_, exists := handlers[lang]
			assert.True(t, exists, "handler map should contain key for %s", lang)
		}
	})
}

func TestServiceTypeToProvider(t *testing.T) {
	t.Parallel()

	t.Run("should map GITHUB to github provider", func(t *testing.T) {
		t.Parallel()

		// given / when
		mapping := commands.ServiceTypeToProvider()

		// then
		assert.Equal(t, "github", mapping[globalEntities.GITHUB])
	})

	t.Run("should map GITLAB to gitlab provider", func(t *testing.T) {
		t.Parallel()

		// given / when
		mapping := commands.ServiceTypeToProvider()

		// then
		assert.Equal(t, "gitlab", mapping[globalEntities.GITLAB])
	})

	t.Run("should map AZUREDEVOPS to azuredevops provider", func(t *testing.T) {
		t.Parallel()

		// given / when
		mapping := commands.ServiceTypeToProvider()

		// then
		assert.Equal(t, "azuredevops", mapping[globalEntities.AZUREDEVOPS])
	})

	t.Run("should map UNKNOWN to empty string", func(t *testing.T) {
		t.Parallel()

		// given / when
		mapping := commands.ServiceTypeToProvider()

		// then
		assert.Equal(t, "", mapping[globalEntities.UNKNOWN])
	})

	t.Run("should map BITBUCKET to empty string", func(t *testing.T) {
		t.Parallel()

		// given / when
		mapping := commands.ServiceTypeToProvider()

		// then
		assert.Equal(t, "", mapping[globalEntities.BITBUCKET])
	})

	t.Run("should map CODECOMMIT to empty string", func(t *testing.T) {
		t.Parallel()

		// given / when
		mapping := commands.ServiceTypeToProvider()

		// then
		assert.Equal(t, "", mapping[globalEntities.CODECOMMIT])
	})
}

func TestDetectDefaultBranch(t *testing.T) {
	t.Parallel()

	t.Run("should return main for a freshly initialized repository", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := initTestGitRepo(t, "main")

		// when
		branch, err := commands.DetectDefaultBranch(context.Background(), repoDir)

		// then
		require.NoError(t, err)
		assert.Equal(t, "main", branch)
	})

	t.Run("should return the current branch name when on a feature branch", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := initTestGitRepo(t, "main")
		runGit(t, repoDir, "checkout", "-b", "feat/test-branch")

		// when
		branch, err := commands.DetectDefaultBranch(context.Background(), repoDir)

		// then
		require.NoError(t, err)
		assert.Equal(t, "feat/test-branch", branch)
	})

	t.Run("should return error for non-git directory", func(t *testing.T) {
		t.Parallel()

		// given
		tmpDir := t.TempDir()

		// when
		_, err := commands.DetectDefaultBranch(context.Background(), tmpDir)

		// then
		require.Error(t, err)
	})
}

func TestParseGitRemote(t *testing.T) {
	t.Parallel()

	t.Run("should parse origin remote URL from a real git repository", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := initTestGitRepo(t, "main")
		runGit(t, repoDir, "remote", "add", "origin", "git@github.com:testorg/testrepo.git")

		// when
		info, err := commands.ParseGitRemote(context.Background(), repoDir)

		// then
		require.NoError(t, err)
		assert.Equal(t, "github", info.ProviderType)
		assert.Equal(t, "testorg", info.Org)
		assert.Equal(t, "testrepo", info.RepoName)
	})

	t.Run("should return error when no origin remote exists", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := initTestGitRepo(t, "main")

		// when
		_, err := commands.ParseGitRemote(context.Background(), repoDir)

		// then
		require.Error(t, err)
	})

	t.Run("should parse HTTPS origin remote", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := initTestGitRepo(t, "main")
		runGit(t, repoDir, "remote", "add", "origin", "https://github.com/anotherorg/anotherrepo.git")

		// when
		info, err := commands.ParseGitRemote(context.Background(), repoDir)

		// then
		require.NoError(t, err)
		assert.Equal(t, "github", info.ProviderType)
		assert.Equal(t, "anotherorg", info.Org)
		assert.Equal(t, "anotherrepo", info.RepoName)
	})

	t.Run("should parse Azure DevOps SSH origin remote", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := initTestGitRepo(t, "main")
		runGit(t, repoDir, "remote", "add", "origin", "git@ssh.dev.azure.com:v3/myorg/myproject/myrepo")

		// when
		info, err := commands.ParseGitRemote(context.Background(), repoDir)

		// then
		require.NoError(t, err)
		assert.Equal(t, "azuredevops", info.ProviderType)
		assert.Equal(t, "myorg", info.Org)
		assert.Equal(t, "myproject", info.Project)
		assert.Equal(t, "myrepo", info.RepoName)
	})
}

func TestCheckLocalRepoConfigSkip(t *testing.T) {
	t.Parallel()

	t.Run("should return false when .autoupdate.yaml does not exist", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()

		// when
		skipped, err := commands.CheckLocalRepoConfigSkip(dir)

		// then
		require.NoError(t, err)
		assert.False(t, skipped)
	})

	t.Run("should return true when .autoupdate.yaml has skip: true", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, ".autoupdate.yaml"),
			[]byte("skip: true\nreason: \"fork\"\n"),
			0o600,
		))

		// when
		skipped, err := commands.CheckLocalRepoConfigSkip(dir)

		// then
		require.NoError(t, err)
		assert.True(t, skipped)
	})

	t.Run("should return false when .autoupdate.yaml has skip: false", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, ".autoupdate.yaml"),
			[]byte("skip: false\n"),
			0o600,
		))

		// when
		skipped, err := commands.CheckLocalRepoConfigSkip(dir)

		// then
		require.NoError(t, err)
		assert.False(t, skipped)
	})

	t.Run("should propagate parse errors from malformed YAML", func(t *testing.T) {
		t.Parallel()

		// given
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, ".autoupdate.yaml"),
			[]byte("skip: : not-yaml"),
			0o600,
		))

		// when
		_, err := commands.CheckLocalRepoConfigSkip(dir)

		// then
		require.Error(t, err)
	})
}

func TestIsExcludedByGlobalList(t *testing.T) {
	t.Parallel()

	github := &commands.RemoteInfo{Org: "rios0rios0", RepoName: "autoupdate"}
	ado := &commands.RemoteInfo{Org: "ZestSecurity", Project: "frontend", RepoName: "opensearch-dashboards"}

	t.Run("should return false when settings is nil", func(t *testing.T) {
		t.Parallel()

		// given/when
		excluded := commands.IsExcludedByGlobalList(nil, github)

		// then
		assert.False(t, excluded)
	})

	t.Run("should return false when ExcludeRepos is empty", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{}

		// when
		excluded := commands.IsExcludedByGlobalList(settings, github)

		// then
		assert.False(t, excluded)
	})

	t.Run("should return true for matching org/repo pattern", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{ExcludeRepos: []string{"rios0rios0/autoupdate"}}

		// when
		excluded := commands.IsExcludedByGlobalList(settings, github)

		// then
		assert.True(t, excluded)
	})

	t.Run("should match Azure DevOps three-segment paths", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{ExcludeRepos: []string{
			"ZestSecurity/frontend/opensearch-dashboards",
		}}

		// when
		excluded := commands.IsExcludedByGlobalList(settings, ado)

		// then
		assert.True(t, excluded)
	})

	t.Run("should match plain repo names via substring fallback", func(t *testing.T) {
		t.Parallel()

		// given
		settings := &entities.Settings{ExcludeRepos: []string{"opensearch-dashboards"}}

		// when
		excluded := commands.IsExcludedByGlobalList(settings, ado)

		// then
		assert.True(t, excluded)
	})
}

func TestLocalCommandExecute(t *testing.T) {
	t.Parallel()

	t.Run("should short-circuit when .autoupdate.yaml requests skip", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := initTestGitRepo(t, "main")
		runGit(t, repoDir, "remote", "add", "origin", "git@github.com:rios0rios0/autoupdate.git")
		require.NoError(t, os.WriteFile(
			filepath.Join(repoDir, ".autoupdate.yaml"),
			[]byte("skip: true\nreason: \"manually maintained\"\n"),
			0o600,
		))

		registry := infraRepos.NewProviderRegistry()
		cmd := commands.NewLocalCommand(registry)

		// when
		err := cmd.Execute(context.Background(), commands.LocalOptions{
			RepoDir: repoDir,
			DryRun:  true,
		})

		// then
		// No git provider was registered and no project files exist; the
		// only way Execute returns nil here is by short-circuiting via
		// the .autoupdate.yaml skip check before any of those steps run.
		require.NoError(t, err)
	})

	t.Run("should short-circuit when global exclude_repos matches", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := initTestGitRepo(t, "main")
		runGit(t, repoDir, "remote", "add", "origin", "git@github.com:rios0rios0/autoupdate.git")

		registry := infraRepos.NewProviderRegistry()
		cmd := commands.NewLocalCommand(registry)

		settings := &entities.Settings{ExcludeRepos: []string{"rios0rios0/autoupdate"}}

		// when
		err := cmd.Execute(context.Background(), commands.LocalOptions{
			RepoDir:  repoDir,
			DryRun:   true,
			Settings: settings,
		})

		// then
		// langforge would otherwise fail to detect a project type here;
		// returning nil proves the exclude_repos check fired before
		// language detection.
		require.NoError(t, err)
	})

	t.Run("should propagate parse errors from .autoupdate.yaml", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := initTestGitRepo(t, "main")
		require.NoError(t, os.WriteFile(
			filepath.Join(repoDir, ".autoupdate.yaml"),
			[]byte("skip: : not-yaml"),
			0o600,
		))

		registry := infraRepos.NewProviderRegistry()
		cmd := commands.NewLocalCommand(registry)

		// when
		err := cmd.Execute(context.Background(), commands.LocalOptions{
			RepoDir: repoDir,
			DryRun:  true,
		})

		// then
		require.Error(t, err)
	})
}

// --- test helpers ---

// initTestGitRepo creates a temporary git repo with an initial commit using exec.Command.
func initTestGitRepo(t *testing.T, branchName string) string {
	t.Helper()

	repoDir := t.TempDir()

	// git init with specified branch
	runGit(t, repoDir, "init", "-b", branchName)

	// configure user identity
	runGit(t, repoDir, "config", "user.name", "test")
	runGit(t, repoDir, "config", "user.email", "test@test.com")
	runGit(t, repoDir, "config", "commit.gpgsign", "false")

	// create a file and commit
	readmePath := filepath.Join(repoDir, "README.md")
	require.NoError(t, os.WriteFile(readmePath, []byte("# Test"), 0o600))
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "initial commit")

	return repoDir
}

// runGit executes a git command in the given directory and fails the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, string(output))
}
