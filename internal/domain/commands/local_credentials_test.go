package commands_test

import (
	"os"
	"path/filepath"
	"testing"

	logger "github.com/sirupsen/logrus"
	logTest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	infraRepos "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
	configHelpers "github.com/rios0rios0/gitforge/v4/pkg/config/domain/helpers"
	credentialEntities "github.com/rios0rios0/gitforge/v4/pkg/credentials/domain/entities"
	credentialInfra "github.com/rios0rios0/gitforge/v4/pkg/credentials/infrastructure"
	globalEntities "github.com/rios0rios0/gitforge/v4/pkg/global/domain/entities"
	gitforgeDoubles "github.com/rios0rios0/gitforge/v4/test/doubles"
)

const (
	githubSSHRemote   = "git@github.com:rios0rios0/autoupdate.git"
	githubHTTPSRemote = "https://github.com/rios0rios0/autoupdate.git"
	azureDevOpsRemote = "git@ssh.dev.azure.com:v3/ContosoSecurity/frontend/opensearch-dashboards"

	flagTokenFixture = "fixture-flag-token-placeholder"
	cliTokenFixture  = "fixture-cli-token-placeholder"
	envTokenFixture  = "fixture-env-token-placeholder"
)

func TestResolveLocalTokens(t *testing.T) {
	t.Parallel()

	t.Run("should give the provider API and git the --token flag without asking the resolver", func(t *testing.T) {
		t.Parallel()

		// given
		credentials := cliCredentials()
		cmd := commands.NewLocalCommand(infraRepos.NewProviderRegistry(), credentials)

		// when
		tokens, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{Token: flagTokenFixture}, remoteFor(t, githubHTTPSRemote),
		)

		// then
		require.NoError(t, err)
		assert.Equal(t, commands.LocalTokens{API: flagTokenFixture, Git: flagTokenFixture}, tokens)
		assert.Empty(t, credentials.Requests())
	})

	t.Run("should take the provider API's token from the resolver when --token is empty", func(t *testing.T) {
		t.Parallel()

		// given
		credentials := cliCredentials()
		cmd := commands.NewLocalCommand(infraRepos.NewProviderRegistry(), credentials)

		// when
		tokens, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubSSHRemote),
		)

		// then
		require.NoError(t, err)
		assert.Equal(t, cliTokenFixture, tokens.API)
		assert.NotEqual(t, cliTokenFixture, tokens.Git, "a token the CLI issued must never reach git")
	})

	t.Run("should name the Azure DevOps organization in the credential request", func(t *testing.T) {
		t.Parallel()

		// given -- the Azure CLI then issues the token in the organization's Entra tenant
		credentials := &gitforgeDoubles.CredentialResolverStub{
			Credential: credentialEntities.Credential{Token: cliTokenFixture, Source: "az CLI"},
		}
		cmd := commands.NewLocalCommand(infraRepos.NewProviderRegistry(), credentials)

		// when
		_, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, azureDevOpsRemote),
		)

		// then
		require.NoError(t, err)
		assert.Equal(t, []credentialEntities.CredentialRequest{{
			ServiceType:  globalEntities.AZUREDEVOPS,
			Organization: "ContosoSecurity",
		}}, credentials.Requests())
	})

	t.Run("should name no organization when the forge is GitHub", func(t *testing.T) {
		t.Parallel()

		// given
		credentials := cliCredentials()
		cmd := commands.NewLocalCommand(infraRepos.NewProviderRegistry(), credentials)

		// when
		_, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubSSHRemote),
		)

		// then
		require.NoError(t, err)
		assert.Equal(t, []credentialEntities.CredentialRequest{{
			ServiceType: globalEntities.GITHUB,
		}}, credentials.Requests())
	})

	t.Run("should name the forge and every source tried when none has a credential", func(t *testing.T) {
		t.Parallel()

		// given -- gitforge's own chain, over an empty environment and a PATH with no gh on it
		chain := credentialInfra.NewChainCredentialResolver(
			&credentialInfra.EnvCredentialResolver{Lookup: func(string) string { return "" }},
			&credentialInfra.CLICredentialResolver{Runner: &gitforgeDoubles.CLIRunnerStub{}},
		)
		_, chainErr := chain.Resolve(t.Context(), credentialEntities.CredentialRequest{
			ServiceType: globalEntities.GITHUB,
		})
		require.Error(t, chainErr)
		cmd := commands.NewLocalCommand(infraRepos.NewProviderRegistry(), chain)

		// when
		_, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubSSHRemote),
		)

		// then
		require.ErrorIs(t, err, credentialEntities.ErrNoCredential)
		require.EqualError(t, err, "cannot authenticate to GitHub without --token: "+chainErr.Error())
		require.ErrorContains(t, err, "GH_TOKEN")
		require.ErrorContains(t, err, "GITHUB_TOKEN")
	})
}

// TestResolveLocalTokensForGit is not parallel: git's token is read from the forge's
// environment variables, which every subtest sets with t.Setenv.
func TestResolveLocalTokensForGit(t *testing.T) {
	t.Run("should give git the environment's token when the credential came from there", func(t *testing.T) {
		// given
		t.Setenv("GITHUB_TOKEN", envTokenFixture)
		t.Setenv("GH_TOKEN", "")
		credentials := &gitforgeDoubles.CredentialResolverStub{
			Credential: credentialEntities.Credential{
				Token: envTokenFixture, Source: "GITHUB_TOKEN environment variable",
			},
		}
		cmd := commands.NewLocalCommand(infraRepos.NewProviderRegistry(), credentials)

		// when
		tokens, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubHTTPSRemote),
		)

		// then
		require.NoError(t, err)
		assert.Equal(t, commands.LocalTokens{API: envTokenFixture, Git: envTokenFixture}, tokens)
	})

	t.Run("should refuse an HTTPS origin without asking the CLI when the environment has no token", func(t *testing.T) {
		// given -- git could push to it with nothing but a token the CLI issued
		t.Setenv("GH_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "")
		credentials := cliCredentials()
		cmd := commands.NewLocalCommand(infraRepos.NewProviderRegistry(), credentials)

		// when
		tokens, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubHTTPSRemote),
		)

		// then
		require.ErrorIs(t, err, commands.ErrNoPushToken)
		require.ErrorContains(t, err, "--token")
		require.ErrorContains(t, err, configHelpers.TokenEnvHint(globalEntities.GITHUB))
		assert.Equal(t, commands.LocalTokens{}, tokens)
		assert.Empty(t, credentials.Requests(), "the CLI's token could not have been used")
	})

	t.Run("should leave git without a token when only the CLI supplied one for an SSH origin", func(t *testing.T) {
		// given -- the push authenticates with the SSH agent, so the run needs no more than this
		t.Setenv("GH_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "")
		cmd := commands.NewLocalCommand(infraRepos.NewProviderRegistry(), cliCredentials())

		// when
		tokens, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubSSHRemote),
		)

		// then
		require.NoError(t, err)
		assert.Equal(t, commands.LocalTokens{API: cliTokenFixture}, tokens)
	})

	t.Run("should log where the credential came from and never the token", func(t *testing.T) {
		// given
		t.Setenv("GH_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "")
		logs := captureLogs(t)
		cmd := commands.NewLocalCommand(infraRepos.NewProviderRegistry(), cliCredentials())

		// when
		_, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubSSHRemote),
		)

		// then
		require.NoError(t, err)
		assert.Equal(t, []string{"Authenticating to GitHub with the gh CLI"}, messagesAt(logs, logger.InfoLevel))
		for _, entry := range logs.AllEntries() {
			assert.NotContains(t, entry.Message, cliTokenFixture)
		}
	})
}

func TestLocalCommandExecuteCredentials(t *testing.T) {
	t.Parallel()

	t.Run("should stop before the upgrade when no credential is found", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := initRepoWithoutCommits(t, githubSSHRemote)
		credentials := &gitforgeDoubles.CredentialResolverStub{Err: credentialEntities.ErrNoCredential}
		cmd := commands.NewLocalCommand(infraRepos.NewProviderRegistry(), credentials)

		// when
		err := cmd.Execute(t.Context(), commands.LocalOptions{RepoDir: repoDir})

		// then -- the credential's error, not that of detectDefaultBranch, which comes after it
		require.ErrorIs(t, err, credentialEntities.ErrNoCredential)
		require.ErrorContains(t, err, "cannot authenticate to GitHub without --token")
		assert.Len(t, credentials.Requests(), 1)
	})

	t.Run("should carry a dry run past a missing credential", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := initRepoWithoutCommits(t, githubSSHRemote)
		credentials := &gitforgeDoubles.CredentialResolverStub{Err: credentialEntities.ErrNoCredential}
		cmd := commands.NewLocalCommand(infraRepos.NewProviderRegistry(), credentials)

		// when
		err := cmd.Execute(t.Context(), commands.LocalOptions{RepoDir: repoDir, DryRun: true})

		// then -- it went on to the next step, which is where this repository stops
		require.ErrorContains(t, err, "failed to detect current branch")
		require.NotErrorIs(t, err, credentialEntities.ErrNoCredential)
		assert.Len(t, credentials.Requests(), 1)
	})

	t.Run("should not ask the resolver when --token is given", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := initRepoWithoutCommits(t, githubHTTPSRemote)
		credentials := &gitforgeDoubles.CredentialResolverStub{Err: credentialEntities.ErrNoCredential}
		cmd := commands.NewLocalCommand(infraRepos.NewProviderRegistry(), credentials)

		// when
		err := cmd.Execute(t.Context(), commands.LocalOptions{RepoDir: repoDir, Token: flagTokenFixture})

		// then
		require.ErrorContains(t, err, "failed to detect current branch")
		assert.Empty(t, credentials.Requests())
	})
}

// --- credential test helpers ---

// cliCredentials answers every request the way gitforge's chain does for an operator who
// exported no token and is signed in to the gh CLI.
func cliCredentials() *gitforgeDoubles.CredentialResolverStub {
	return &gitforgeDoubles.CredentialResolverStub{
		Credential: credentialEntities.Credential{Token: cliTokenFixture, Source: "gh CLI"},
	}
}

// remoteFor parses a remote URL the way Execute parses origin's.
func remoteFor(t *testing.T, url string) *commands.RemoteInfo {
	t.Helper()

	remote, err := commands.ParseRemoteURL(url)
	require.NoError(t, err)

	return remote
}

// initRepoWithoutCommits lays out a Go repository whose origin is remoteURL and which has no
// commit yet. Execute detects the project and resolves its credentials, then stops at
// detectDefaultBranch, because there is no HEAD to name: past the credentials, and before any
// upgrade could reach the network.
func initRepoWithoutCommits(t *testing.T, remoteURL string) string {
	t.Helper()

	repoDir := t.TempDir()
	runGit(t, repoDir, "init", "-b", "main")
	runGit(t, repoDir, "remote", "add", "origin", remoteURL)
	require.NoError(t, os.WriteFile(
		filepath.Join(repoDir, "go.mod"), []byte("module example.com/fixture\n\ngo 1.22\n"), 0o600,
	))

	return repoDir
}

// captureLogs records every entry the standard logger takes until the test ends. Only a test
// that is not parallel may use it, because the hooks it swaps are the whole process's.
func captureLogs(t *testing.T) *logTest.Hook {
	t.Helper()

	previous := logger.StandardLogger().ReplaceHooks(make(logger.LevelHooks))
	t.Cleanup(func() { logger.StandardLogger().ReplaceHooks(previous) })

	return logTest.NewLocal(logger.StandardLogger())
}

// messagesAt returns the messages of the captured entries logged at level, in order.
func messagesAt(logs *logTest.Hook, level logger.Level) []string {
	var messages []string
	for _, entry := range logs.AllEntries() {
		if entry.Level == level {
			messages = append(messages, entry.Message)
		}
	}

	return messages
}
