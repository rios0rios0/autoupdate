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
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	infraRepos "github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/gitlocal"
	configHelpers "github.com/rios0rios0/gitforge/v4/pkg/config/domain/helpers"
	credentialEntities "github.com/rios0rios0/gitforge/v4/pkg/credentials/domain/entities"
	credentialInfra "github.com/rios0rios0/gitforge/v4/pkg/credentials/infrastructure"
	globalEntities "github.com/rios0rios0/gitforge/v4/pkg/global/domain/entities"
	"github.com/rios0rios0/gitforge/v4/pkg/providers/infrastructure/github"
	gitforgeDoubles "github.com/rios0rios0/gitforge/v4/test/doubles"
)

const (
	githubSSHRemote   = "git@github.com:rios0rios0/autoupdate.git"
	githubHTTPSRemote = "https://github.com/rios0rios0/autoupdate.git"
	azureDevOpsRemote = "git@ssh.dev.azure.com:v3/ContosoSecurity/frontend/opensearch-dashboards"
	pushURLFixture    = "https://push.example.invalid/rios0rios0/autoupdate.git"

	flagTokenFixture      = "fixture-flag-token-placeholder"
	cliTokenFixture       = "fixture-cli-token-placeholder"
	refreshedTokenFixture = "fixture-refreshed-token-placeholder"
	envTokenFixture       = "fixture-env-token-placeholder"
)

func TestResolveLocalTokens(t *testing.T) {
	t.Parallel()

	t.Run("should give the provider API and git the --token flag without asking either resolver", func(t *testing.T) {
		t.Parallel()

		// given
		credentials, gitCredentials := cliCredentials(), exportedGitToken()
		cmd := newLocalCommand(credentials, gitCredentials)

		// when
		tokens, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{Token: flagTokenFixture}, remoteFor(t, githubHTTPSRemote),
		)

		// then
		require.NoError(t, err)
		assert.Equal(t, flagTokenFixture, tokens.Git)
		assert.Equal(t, flagTokenFixture, apiToken(t, tokens))
		assert.Empty(t, credentials.Requests())
		assert.Empty(t, gitCredentials.Requests())
	})

	t.Run(
		"should take the provider API's token from the resolver and none for git when --token is empty",
		func(t *testing.T) {
			t.Parallel()

			// given -- nothing exported, signed in to gh
			cmd := newLocalCommand(cliCredentials(), noGitToken())

			// when
			tokens, err := commands.ResolveLocalTokens(
				cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubSSHRemote),
			)

			// then -- the push goes over SSH, with the SSH agent
			require.NoError(t, err)
			assert.Equal(t, cliTokenFixture, apiToken(t, tokens))
			assert.Empty(t, tokens.Git, "a token the CLI issued must never reach git")
		},
	)

	t.Run("should give git the token exported in the environment", func(t *testing.T) {
		t.Parallel()

		// given
		credentials := &gitforgeDoubles.CredentialResolverStub{Credential: credentialEntities.Credential{
			Token: envTokenFixture, Source: "GITHUB_TOKEN environment variable",
		}}
		cmd := newLocalCommand(credentials, exportedGitToken())

		// when
		tokens, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubHTTPSRemote),
		)

		// then
		require.NoError(t, err)
		assert.Equal(t, envTokenFixture, tokens.Git)
		assert.Equal(t, envTokenFixture, apiToken(t, tokens))
	})

	t.Run("should ask for the provider API's token again each time it is used", func(t *testing.T) {
		t.Parallel()

		// given -- an az token that expired during the upgrade, so az issues another
		credentials := cliCredentials()
		cmd := newLocalCommand(credentials, noGitToken())
		tokens, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubSSHRemote),
		)
		require.NoError(t, err)
		credentials.Credential.Token = refreshedTokenFixture

		// when
		token := apiToken(t, tokens)

		// then
		assert.Equal(t, refreshedTokenFixture, token)
		assert.Len(t, credentials.Requests(), 2, "once to fail fast, once when the token is used")
	})

	t.Run("should build the migration provider with the token current when it is asked for", func(t *testing.T) {
		t.Parallel()

		// given
		credentials := cliCredentials()
		cmd := newLocalCommand(credentials, noGitToken())
		tokens, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubSSHRemote),
		)
		require.NoError(t, err)
		credentials.Credential.Token = refreshedTokenFixture

		// when -- after the push, when a newer major is waiting
		provider := commands.LocalMigrationProvider(
			t.Context(), githubRegistry(), "github", tokens, commands.LocalOptions{},
		)

		// then
		require.NotNil(t, provider)
		assert.Equal(t, refreshedTokenFixture, provider.AuthToken())
	})

	t.Run("should name the Azure DevOps organization in the credential request", func(t *testing.T) {
		t.Parallel()

		// given -- the Azure CLI then issues the token in the organization's Entra tenant
		credentials := &gitforgeDoubles.CredentialResolverStub{
			Credential: credentialEntities.Credential{Token: cliTokenFixture, Source: "az CLI"},
		}
		cmd := newLocalCommand(credentials, noGitToken())

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
		cmd := newLocalCommand(credentials, noGitToken())

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
			environmentOf(nil),
			&credentialInfra.CLICredentialResolver{Runner: &gitforgeDoubles.CLIRunnerStub{}},
		)
		_, chainErr := chain.Resolve(t.Context(), credentialEntities.CredentialRequest{
			ServiceType: globalEntities.GITHUB,
		})
		require.Error(t, chainErr)
		cmd := newLocalCommand(chain, environmentOf(nil))

		// when
		_, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubSSHRemote),
		)

		// then
		require.ErrorIs(t, err, credentialEntities.ErrNoCredential)
		require.EqualError(t, err, "cannot authenticate to GitHub without --token: "+chainErr.Error())
		require.ErrorContains(t, err, "GITHUB_TOKEN")
		require.ErrorContains(t, err, "GH_TOKEN")
	})

	for name, token := range map[string]string{"empty": "", "whitespace-only": "   "} {
		t.Run("should take an answer with an "+name+" token for no credential", func(t *testing.T) {
			t.Parallel()

			// given
			credentials := &gitforgeDoubles.CredentialResolverStub{
				Credential: credentialEntities.Credential{Token: token, Source: "gh CLI"},
			}
			cmd := newLocalCommand(credentials, noGitToken())

			// when
			_, err := commands.ResolveLocalTokens(
				cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubSSHRemote),
			)

			// then
			require.ErrorIs(t, err, credentialEntities.ErrNoCredential)
			require.ErrorContains(t, err, "the gh CLI returned an empty token")
		})
	}
}

func TestResolveLocalTokensForAnHTTPSPush(t *testing.T) {
	t.Parallel()

	t.Run("should refuse an HTTPS origin without asking the CLI when git has no token", func(t *testing.T) {
		t.Parallel()

		// given -- git could push to it with nothing but a token the CLI issued
		credentials := cliCredentials()
		cmd := newLocalCommand(credentials, noGitToken())

		// when
		tokens, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubHTTPSRemote),
		)

		// then
		require.ErrorIs(t, err, commands.ErrNoPushToken)
		require.ErrorContains(t, err, "--token")
		require.ErrorContains(t, err, configHelpers.TokenEnvHint(globalEntities.GITHUB))
		assert.Empty(t, tokens.Git)
		assert.Empty(t, credentials.Requests(), "the CLI's token could not have been used")
	})

	for name, pushURLs := range map[string][]string{
		"after the SSH one":  {githubSSHRemote, pushURLFixture},
		"before the SSH one": {pushURLFixture, githubSSHRemote},
	} {
		t.Run("should refuse an origin with an HTTPS URL "+name+" when git has no token", func(t *testing.T) {
			t.Parallel()

			// given -- go-git pushes to the last URL and picks its forge from the first
			credentials := cliCredentials()
			cmd := newLocalCommand(credentials, noGitToken())

			// when
			_, err := commands.ResolveLocalTokens(
				cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubSSHRemote, pushURLs...),
			)

			// then
			require.ErrorIs(t, err, commands.ErrNoPushToken)
			assert.Empty(t, credentials.Requests())
		})
	}

	t.Run("should not take a variable holding only whitespace for git's token", func(t *testing.T) {
		t.Parallel()

		// given -- read the way gitforge's chain reads the variables
		cmd := newLocalCommand(cliCredentials(), environmentOf(map[string]string{"GITHUB_TOKEN": "   "}))

		// when
		_, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubHTTPSRemote),
		)

		// then -- refused now, instead of passing the check and failing at the push
		require.ErrorIs(t, err, commands.ErrNoPushToken)
	})

	t.Run("should take the next variable when the first holds only whitespace", func(t *testing.T) {
		t.Parallel()

		// given
		cmd := newLocalCommand(cliCredentials(), environmentOf(map[string]string{
			"GITHUB_TOKEN": "   ", "GH_TOKEN": " " + envTokenFixture + " ",
		}))

		// when
		tokens, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubHTTPSRemote),
		)

		// then
		require.NoError(t, err)
		assert.Equal(t, envTokenFixture, tokens.Git)
	})
}

func TestResolveLocalTokensForGitLab(t *testing.T) {
	t.Parallel()

	selfManaged := "git@gitlab.example.com:platform/service.git"

	t.Run("should refuse an origin on another GitLab server without asking for a token", func(t *testing.T) {
		t.Parallel()

		// given -- gitforge reads any SSH host containing "gitlab" as GitLab
		credentials, gitCredentials := cliCredentials(), exportedGitToken()
		cmd := newLocalCommand(credentials, gitCredentials)

		// when
		_, err := commands.ResolveLocalTokens(cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, selfManaged))

		// then
		require.ErrorIs(t, err, commands.ErrSelfManagedGitLab)
		require.ErrorContains(t, err, "gitlab.example.com")
		assert.Empty(t, credentials.Requests())
		assert.Empty(t, gitCredentials.Requests())
	})

	t.Run("should refuse an origin on another GitLab server even with --token", func(t *testing.T) {
		t.Parallel()

		// given -- the provider would send it to gitlab.com all the same
		cmd := newLocalCommand(cliCredentials(), exportedGitToken())

		// when
		_, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{Token: flagTokenFixture}, remoteFor(t, selfManaged),
		)

		// then
		require.ErrorIs(t, err, commands.ErrSelfManagedGitLab)
	})

	t.Run("should accept an origin on gitlab.com", func(t *testing.T) {
		t.Parallel()

		// given
		cmd := newLocalCommand(cliCredentials(), noGitToken())

		// when
		_, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, "git@gitlab.com:group/repo.git"),
		)

		// then
		require.NoError(t, err)
	})

	t.Run("should accept an SSH alias the SSH configuration resolves to gitlab.com", func(t *testing.T) {
		t.Parallel()

		// given
		origins := &gitlocal.OriginRepository{SSHHostName: func(alias string) string {
			if alias == "gitlab.com-work" {
				return "gitlab.com"
			}
			return ""
		}}
		cmd := commands.NewLocalCommand(infraRepos.NewProviderRegistry(), cliCredentials(), noGitToken(), origins)

		// when
		_, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, "git@gitlab.com-work:group/repo.git"),
		)

		// then
		require.NoError(t, err)
	})
}

// TestResolveLocalTokensLogging is not parallel: it swaps the standard logger's hooks, which
// belong to the whole process.
func TestResolveLocalTokensLogging(t *testing.T) { //nolint:paralleltest // swaps the standard logger's hooks
	t.Run("should log where the credential came from and never the token", func(t *testing.T) {
		// given
		logs := captureLogs(t)
		cmd := newLocalCommand(cliCredentials(), noGitToken())

		// when
		tokens, err := commands.ResolveLocalTokens(
			cmd, t.Context(), commands.LocalOptions{}, remoteFor(t, githubSSHRemote),
		)
		_ = apiToken(t, tokens)

		// then -- once, although the token was asked for twice
		require.NoError(t, err)
		assert.Equal(t, []string{"Authenticating to GitHub with the gh CLI"}, messagesAt(logs, logger.InfoLevel))
		for _, entry := range logs.AllEntries() {
			assert.NotContains(t, entry.Message, cliTokenFixture)
		}
	})
}

// TestLocalCommandUnderAGlobalInsteadOfRule is not parallel: every subtest hands git a global
// configuration with t.Setenv.
func TestLocalCommandUnderAGlobalInsteadOfRule(t *testing.T) {
	t.Run("should read origin's push URLs without the global rule git prints it with", func(t *testing.T) {
		// given -- the rule people use to fetch private Go modules over SSH
		t.Setenv("GIT_CONFIG_GLOBAL", writeGlobalGitConfig(t, sshInsteadOfHTTPSRule))
		repoDir := initRepoWithoutCommits(t, githubHTTPSRemote)

		// when
		remote, err := commands.ParseGitRemote(t.Context(), repoDir)
		require.NoError(t, err)
		pushURLs, pushErr := gitlocal.OriginPushURLs(repoDir)

		// then -- git applies the global rule, while the push reads origin without it
		require.NoError(t, pushErr)
		assert.Equal(t, "git@github.com:rios0rios0/autoupdate.git", remote.RemoteURL)
		assert.Equal(t, []string{githubHTTPSRemote}, pushURLs)
	})

	t.Run("should refuse an HTTPS origin that only a global insteadOf rule shows as SSH", func(t *testing.T) {
		// given
		t.Setenv("GIT_CONFIG_GLOBAL", writeGlobalGitConfig(t, sshInsteadOfHTTPSRule))
		repoDir := initRepoWithoutCommits(t, githubHTTPSRemote)
		credentials := cliCredentials()
		cmd := newLocalCommand(credentials, noGitToken())

		// when
		err := cmd.Execute(t.Context(), commands.LocalOptions{RepoDir: repoDir})

		// then -- refused up front, rather than failing at the push after the upgrade
		require.ErrorIs(t, err, commands.ErrNoPushToken)
		assert.Empty(t, credentials.Requests())
	})
}

func TestLocalCommandExecuteCredentials(t *testing.T) {
	t.Parallel()

	t.Run("should stop before the upgrade when no credential is found", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := initRepoWithoutCommits(t, githubSSHRemote)
		credentials := &gitforgeDoubles.CredentialResolverStub{Err: credentialEntities.ErrNoCredential}
		cmd := newLocalCommand(credentials, noGitToken())

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
		cmd := newLocalCommand(credentials, noGitToken())

		// when
		err := cmd.Execute(t.Context(), commands.LocalOptions{RepoDir: repoDir, DryRun: true})

		// then -- it went on to the next step, which is where this repository stops
		require.ErrorContains(t, err, "failed to detect current branch")
		require.NotErrorIs(t, err, credentialEntities.ErrNoCredential)
		assert.Len(t, credentials.Requests(), 1)
	})

	t.Run("should not ask the resolvers when --token is given", func(t *testing.T) {
		t.Parallel()

		// given
		repoDir := initRepoWithoutCommits(t, githubHTTPSRemote)
		credentials, gitCredentials := unusedCredentials(), unusedCredentials()
		cmd := newLocalCommand(credentials, gitCredentials)

		// when
		err := cmd.Execute(t.Context(), commands.LocalOptions{RepoDir: repoDir, Token: flagTokenFixture})

		// then
		require.ErrorContains(t, err, "failed to detect current branch")
		assert.Empty(t, credentials.Requests())
		assert.Empty(t, gitCredentials.Requests())
	})

	for name, configure := range map[string][]string{
		"a pushurl":    {"remote", "set-url", "--push", "origin", pushURLFixture},
		"a second url": {"config", "--add", "remote.origin.url", pushURLFixture},
	} {
		t.Run("should refuse an SSH origin with "+name+" over HTTPS before the upgrade", func(t *testing.T) {
			t.Parallel()

			// given -- fetched over SSH, pushed over HTTPS, signed in to gh only
			repoDir := initRepoWithoutCommits(t, githubSSHRemote)
			runGit(t, repoDir, configure...)
			credentials := cliCredentials()
			cmd := newLocalCommand(credentials, noGitToken())

			// when
			err := cmd.Execute(t.Context(), commands.LocalOptions{RepoDir: repoDir})

			// then -- before any branch or commit, not at the push after them
			require.ErrorIs(t, err, commands.ErrNoPushToken)
			assert.Empty(t, credentials.Requests())
		})
	}

	for transport, remoteURL := range map[string]string{"SSH": githubSSHRemote, "HTTPS": githubHTTPSRemote} {
		t.Run(
			"should not ask for a token for a project type it has no upgrade for, over "+transport,
			func(t *testing.T) {
				t.Parallel()

				// given -- a Terraform repository, which only `autoupdate run` updates
				repoDir := t.TempDir()
				runGit(t, repoDir, "init", "-b", "main")
				runGit(t, repoDir, "remote", "add", "origin", remoteURL)
				require.NoError(t, os.WriteFile(filepath.Join(repoDir, "main.tf"), []byte("terraform {}\n"), 0o600))
				credentials, gitCredentials := unusedCredentials(), unusedCredentials()
				cmd := newLocalCommand(credentials, gitCredentials)

				// when
				err := cmd.Execute(t.Context(), commands.LocalOptions{RepoDir: repoDir})

				// then
				require.EqualError(t, err, "unsupported project type: terraform")
				assert.Empty(t, credentials.Requests())
				assert.Empty(t, gitCredentials.Requests())
			},
		)
	}

	t.Run("should refuse a checkout whose origin go-git cannot read", func(t *testing.T) {
		t.Parallel()

		// given -- a module in a subdirectory: git finds the repository from there, go-git
		// does not, and the push opens it with go-git too
		repoDir := initRepoWithoutCommits(t, githubSSHRemote)
		moduleDir := filepath.Join(repoDir, "module")
		// A directory needs the owner search bit, so 0o700 is the least-privilege mode here.
		// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
		require.NoError(t, os.MkdirAll(moduleDir, 0o700))
		require.NoError(t, os.WriteFile(
			filepath.Join(moduleDir, "go.mod"), []byte("module example.com/module\n\ngo 1.22\n"), 0o600,
		))
		credentials := cliCredentials()
		cmd := newLocalCommand(credentials, noGitToken())

		// when
		err := cmd.Execute(t.Context(), commands.LocalOptions{RepoDir: moduleDir})

		// then -- not guessed from what git printed
		require.ErrorContains(t, err, "failed to read the URLs a push to origin dials")
		assert.Empty(t, credentials.Requests())
	})
}

// --- credential test helpers ---

// newLocalCommand builds a LocalCommand the way the container wires it, over the given
// resolvers and with an SSH client configuration that names no alias.
func newLocalCommand(
	credentials repositories.CredentialResolver, gitCredentials repositories.GitCredentialResolver,
) *commands.LocalCommand {
	return commands.NewLocalCommand(
		infraRepos.NewProviderRegistry(), credentials, gitCredentials,
		&gitlocal.OriginRepository{SSHHostName: func(string) string { return "" }},
	)
}

// cliCredentials answers every request the way gitforge's chain does for an operator who
// exported no token and is signed in to the gh CLI.
func cliCredentials() *gitforgeDoubles.CredentialResolverStub {
	return &gitforgeDoubles.CredentialResolverStub{
		Credential: credentialEntities.Credential{Token: cliTokenFixture, Source: "gh CLI"},
	}
}

// exportedGitToken answers git's resolver for an operator who exported GITHUB_TOKEN.
func exportedGitToken() *gitforgeDoubles.CredentialResolverStub {
	return &gitforgeDoubles.CredentialResolverStub{Credential: credentialEntities.Credential{
		Token: envTokenFixture, Source: "GITHUB_TOKEN environment variable",
	}}
}

// noGitToken answers git's resolver for an operator who exported no token.
func noGitToken() *gitforgeDoubles.CredentialResolverStub {
	return &gitforgeDoubles.CredentialResolverStub{Err: credentialEntities.ErrNoCredential}
}

// unusedCredentials is a resolver a test expects never to be asked: it declines, so a run that
// asked it would stop, and the test checks its requests are empty.
func unusedCredentials() *gitforgeDoubles.CredentialResolverStub {
	return &gitforgeDoubles.CredentialResolverStub{Err: credentialEntities.ErrNoCredential}
}

// environmentOf is gitforge's EnvCredentialResolver over the given variables instead of the
// process environment, so a test can run in parallel.
func environmentOf(variables map[string]string) *credentialInfra.EnvCredentialResolver {
	return &credentialInfra.EnvCredentialResolver{Lookup: func(name string) string { return variables[name] }}
}

// githubRegistry knows the GitHub provider, which carries the token it is built with.
func githubRegistry() *infraRepos.ProviderRegistry {
	registry := infraRepos.NewProviderRegistry()
	registry.RegisterFactory("github", github.NewProvider)
	return registry
}

// apiToken asks tokens for the provider API's token and fails the test when it cannot.
func apiToken(t *testing.T, tokens commands.LocalTokens) string {
	t.Helper()

	token, err := tokens.API(t.Context())
	require.NoError(t, err)

	return token
}

// remoteFor parses a remote URL the way Execute parses origin's, with the URLs a push to it
// dials: pushURLs, or the URL itself when none are given.
func remoteFor(t *testing.T, url string, pushURLs ...string) *commands.RemoteInfo {
	t.Helper()

	remote, err := commands.ParseRemoteURL(url)
	require.NoError(t, err)

	remote.PushURLs = pushURLs
	if len(pushURLs) == 0 {
		remote.PushURLs = []string{url}
	}

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

// sshInsteadOfHTTPSRule makes git reach GitHub over SSH wherever an HTTPS URL is written.
const sshInsteadOfHTTPSRule = "[url \"git@github.com:\"]\n\tinsteadOf = https://github.com/\n"

// writeGlobalGitConfig writes a gitconfig for GIT_CONFIG_GLOBAL to point at.
func writeGlobalGitConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
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
