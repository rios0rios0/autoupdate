package support

import "strings"

// Git provider names, matching what ProviderRepository.Name() reports. The
// generated scripts key their credential rewrites off these.
const (
	ProviderAzureDevOps = "azuredevops"
	ProviderGitHub      = "github"
	ProviderGitLab      = "gitlab"
)

// gitAuthRewrites maps a provider to the git `insteadOf` rules that make an
// anonymous remote resolve through the run's auth token. A lookup rather than a
// switch, so supporting another provider is one entry rather than an edit to
// every updater that builds a script.
//
// The snippets are raw strings because they are bash: written as Go string
// literals every inner quote needs escaping, which is how the four variants
// drifted apart in the first place.
//
// A secret scanner reads these as credentials in a URL. They are not: the
// ${AUTH_TOKEN} in each is a shell variable bash expands when it runs the
// generated script, and the real token only ever reaches the script through the
// environment. The entries are suppressed by path in .gitleaksignore.
//
// Azure DevOps gets two rules. The SSH one matters because a dependency
// declared against `git@ssh.dev.azure.com:v3/...` cannot authenticate with a
// token otherwise, and a checkout has no key material.
var gitAuthRewrites = map[string]string{ //nolint:gochecknoglobals // static lookup table
	ProviderAzureDevOps: `echo '[url "https://pat:'"${AUTH_TOKEN}"'@dev.azure.com/"]' >> "$TEMP_GITCONFIG"
echo '    insteadOf = https://dev.azure.com/' >> "$TEMP_GITCONFIG"
echo '[url "https://pat:'"${AUTH_TOKEN}"'@dev.azure.com/"]' >> "$TEMP_GITCONFIG"
echo '    insteadOf = git@ssh.dev.azure.com:v3/' >> "$TEMP_GITCONFIG"
`,
	ProviderGitHub: `echo '[url "https://x-access-token:'"${AUTH_TOKEN}"'@github.com/"]' >> "$TEMP_GITCONFIG"
echo '    insteadOf = https://github.com/' >> "$TEMP_GITCONFIG"
`,
	ProviderGitLab: `echo '[url "https://oauth2:'"${AUTH_TOKEN}"'@gitlab.com/"]' >> "$TEMP_GITCONFIG"
echo '    insteadOf = https://gitlab.com/' >> "$TEMP_GITCONFIG"
`,
}

// GitAuthScript returns the bash that points git at a throwaway config
// rewriting the provider's public URLs to token-authenticated ones, so the
// clone, the push, and any package manager reaching for a private dependency
// all authenticate without a credential ever touching the user's ~/.gitconfig.
// The config is removed on exit by the trap.
//
// The token itself is never interpolated here: the script reads it from
// $AUTH_TOKEN at run time, which is what keeps it out of the script file and
// out of any log line that echoes the script.
//
// An unknown provider yields the isolated config with no rewrite rules, which
// is what the caller wants — the script still runs, it just has no credential
// to offer.
func GitAuthScript(providerName string) string {
	var sb strings.Builder

	sb.WriteString("# Set up isolated git config for auth\n")
	sb.WriteString("TEMP_GITCONFIG=$(mktemp)\n")
	sb.WriteString("cp ~/.gitconfig \"$TEMP_GITCONFIG\" 2>/dev/null || true\n")
	sb.WriteString(gitAuthRewrites[providerName])
	sb.WriteString("export GIT_CONFIG_GLOBAL=\"$TEMP_GITCONFIG\"\n")
	sb.WriteString("trap 'rm -f \"$TEMP_GITCONFIG\"' EXIT\n\n")

	return sb.String()
}
