package cmd //nolint:testpackage // tests unexported functions

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRemoteURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		url          string
		providerType string
		org          string
		project      string
		repoName     string
		expectErr    bool
	}{
		// --- GitHub ---
		{
			name:         "GitHub HTTPS",
			url:          "https://github.com/rios0rios0/autoupdate.git",
			providerType: providerGitHub,
			org:          "rios0rios0",
			repoName:     "autoupdate",
		},
		{
			name:         "GitHub HTTPS without .git",
			url:          "https://github.com/rios0rios0/autoupdate",
			providerType: providerGitHub,
			org:          "rios0rios0",
			repoName:     "autoupdate",
		},
		{
			name:         "GitHub SSH",
			url:          "git@github.com:rios0rios0/autoupdate.git",
			providerType: providerGitHub,
			org:          "rios0rios0",
			repoName:     "autoupdate",
		},
		{
			name:         "GitHub SSH without .git",
			url:          "git@github.com:rios0rios0/autoupdate",
			providerType: providerGitHub,
			org:          "rios0rios0",
			repoName:     "autoupdate",
		},
		// --- Azure DevOps ---
		{
			name:         "Azure DevOps HTTPS",
			url:          "https://dev.azure.com/myorg/myproject/_git/myrepo",
			providerType: providerAzureDevOps,
			org:          "myorg",
			project:      "myproject",
			repoName:     "myrepo",
		},
		{
			name:         "Azure DevOps SSH",
			url:          "git@ssh.dev.azure.com:v3/myorg/myproject/myrepo",
			providerType: providerAzureDevOps,
			org:          "myorg",
			project:      "myproject",
			repoName:     "myrepo",
		},
		{
			name:         "Azure DevOps SSH with custom alias",
			url:          "git@dev.azure.com-arancia:v3/ZestSecurity/backend/catalog",
			providerType: providerAzureDevOps,
			org:          "ZestSecurity",
			project:      "backend",
			repoName:     "catalog",
		},
		// --- GitLab ---
		{
			name:         "GitLab HTTPS",
			url:          "https://gitlab.com/mygroup/myproject.git",
			providerType: providerGitLab,
			org:          "mygroup",
			repoName:     "myproject",
		},
		{
			name:         "GitLab SSH",
			url:          "git@gitlab.com:mygroup/myproject.git",
			providerType: providerGitLab,
			org:          "mygroup",
			repoName:     "myproject",
		},
		// --- Unsupported ---
		{
			name:      "unsupported provider",
			url:       "https://bitbucket.org/org/repo.git",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// when
			remote, err := parseRemoteURL(tt.url)

			// then
			if tt.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.providerType, remote.ProviderType)
			assert.Equal(t, tt.org, remote.Org)
			assert.Equal(t, tt.project, remote.Project)
			assert.Equal(t, tt.repoName, remote.RepoName)
		})
	}
}

func TestResolveTokenFromEnv(t *testing.T) {
	// Cannot use t.Parallel() at the suite level because subtests call
	// t.Setenv which modifies the process environment.

	t.Run("should read GITHUB_TOKEN for github provider", func(t *testing.T) {
		// given
		t.Setenv("GITHUB_TOKEN", "gh-test-token")

		// when
		token := resolveTokenFromEnv(providerGitHub)

		// then
		assert.Equal(t, "gh-test-token", token)
	})

	t.Run("should fall back to GH_TOKEN for github provider", func(t *testing.T) {
		// given
		t.Setenv("GH_TOKEN", "gh-alt-token")

		// when
		token := resolveTokenFromEnv(providerGitHub)

		// then
		assert.Equal(t, "gh-alt-token", token)
	})

	t.Run("should read AZURE_DEVOPS_EXT_PAT for azuredevops provider", func(t *testing.T) {
		// given
		t.Setenv("AZURE_DEVOPS_EXT_PAT", "ado-test-token")

		// when
		token := resolveTokenFromEnv(providerAzureDevOps)

		// then
		assert.Equal(t, "ado-test-token", token)
	})

	t.Run("should fall back to SYSTEM_ACCESSTOKEN for azuredevops provider", func(t *testing.T) {
		// given
		t.Setenv("SYSTEM_ACCESSTOKEN", "ado-system-token")

		// when
		token := resolveTokenFromEnv(providerAzureDevOps)

		// then
		assert.Equal(t, "ado-system-token", token)
	})

	t.Run("should read GITLAB_TOKEN for gitlab provider", func(t *testing.T) {
		// given
		t.Setenv("GITLAB_TOKEN", "gl-test-token")

		// when
		token := resolveTokenFromEnv(providerGitLab)

		// then
		assert.Equal(t, "gl-test-token", token)
	})

	t.Run("should return empty for unknown provider", func(t *testing.T) {
		// when
		token := resolveTokenFromEnv("unknown")

		// then
		assert.Empty(t, token)
	})
}

func TestTokenEnvHint(t *testing.T) {
	t.Parallel()

	assert.Contains(t, tokenEnvHint(providerGitHub), "GITHUB_TOKEN")
	assert.Contains(t, tokenEnvHint(providerAzureDevOps), "AZURE_DEVOPS_EXT_PAT")
	assert.Contains(t, tokenEnvHint(providerGitLab), "GITLAB_TOKEN")
	assert.Contains(t, tokenEnvHint("other"), "unknown")
}

func TestDetectDefaultBranch(t *testing.T) {
	t.Parallel()

	t.Run("should detect current branch in this repo", func(t *testing.T) {
		t.Parallel()

		// given — run against the project's own repo root
		repoDir, err := os.Getwd()
		require.NoError(t, err)
		// cmd/ is the package dir; go up one level
		repoDir += "/.."

		// when
		branch, branchErr := detectDefaultBranch(context.Background(), repoDir)

		// then
		require.NoError(t, branchErr)
		assert.NotEmpty(t, branch)
	})
}
