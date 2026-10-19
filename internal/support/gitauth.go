package support

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Git provider names, matching what ProviderRepository.Name() reports. The
// generated scripts key their credential rewrites off these.
const (
	ProviderAzureDevOps = "azuredevops"
	ProviderGitHub      = "github"
	ProviderGitLab      = "gitlab"
)

// gitAuthTokenPlaceholder is where a credential URL carries the token. It is a
// shell expansion because the generated script reads the real token from the
// environment at run time -- which is what keeps the secret out of the script
// file and out of any log line that echoes the script -- and it is replaced
// with the token directly when the same rule is written into a gitconfig file.
const gitAuthTokenPlaceholder = "${AUTH_TOKEN}" //nolint:gosec // this is a non-secret shell placeholder

// gitAuthRule is one provider's credential rewrite: the URL a remote is
// rewritten to, and the URL prefixes it is rewritten from.
type gitAuthRule struct {
	credential string
	insteadOf  []string
}

// gitAuthRules maps a provider to its rewrite rules. A lookup rather than a
// switch, so supporting another provider is one entry rather than an edit to
// every updater that builds a script.
//
// A secret scanner reads these as credentials in a URL. They are not: the
// ${AUTH_TOKEN} in each is a placeholder, and the real token only ever reaches
// the config through the environment or through the file this package writes.
// The entries are suppressed by path in .gitleaksignore.
//
// Azure DevOps gets two rules. The SSH one matters because a dependency
// declared against `git@ssh.dev.azure.com:v3/...` cannot authenticate with a
// token otherwise, and a checkout has no key material.
var gitAuthRules = map[string]gitAuthRule{ //nolint:gochecknoglobals // static lookup table
	ProviderAzureDevOps: {
		credential: "https://pat:" + gitAuthTokenPlaceholder + "@dev.azure.com/",
		insteadOf:  []string{"https://dev.azure.com/", "git@ssh.dev.azure.com:v3/"},
	},
	ProviderGitHub: {
		credential: "https://x-access-token:" + gitAuthTokenPlaceholder + "@github.com/",
		insteadOf:  []string{"https://github.com/"},
	},
	ProviderGitLab: {
		credential: "https://oauth2:" + gitAuthTokenPlaceholder + "@gitlab.com/",
		insteadOf:  []string{"https://gitlab.com/"},
	},
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

	rule, known := gitAuthRules[providerName]
	if known {
		for _, insteadOf := range rule.insteadOf {
			sb.WriteString(gitConfigEcho(rule.credential, insteadOf))
		}
	}

	sb.WriteString("export GIT_CONFIG_GLOBAL=\"$TEMP_GITCONFIG\"\n")
	sb.WriteString("trap 'rm -f \"$TEMP_GITCONFIG\"' EXIT\n\n")

	return sb.String()
}

// gitConfigEcho renders the two lines that append one rewrite rule to the
// throwaway config: the credential URL, split around the placeholder so bash
// expands exactly that piece, and the prefix it replaces.
func gitConfigEcho(credential, insteadOf string) string {
	prefix, suffix, _ := strings.Cut(credential, gitAuthTokenPlaceholder)

	return fmt.Sprintf(
		"printf '[url \"%%s%%s%%s\"]\\n' '%s' \"${AUTH_TOKEN}\" '%s' >> \"$TEMP_GITCONFIG\"\n"+
			"printf '    insteadOf = %s\\n' >> \"$TEMP_GITCONFIG\"\n",
		prefix, suffix, insteadOf,
	)
}

// GitAuthEnv returns the environment entries that hand git the run's
// credentials without touching the operator's own configuration: the token
// under both names the scripts and the tools read, and -- when a config file
// was written -- the path that replaces the global gitconfig for the process.
//
// GIT_TERMINAL_PROMPT=0 is unconditional. Every one of these processes runs
// unattended, and a git that stops to ask for a username hangs a batch run on a
// prompt nobody will ever see; failing instead is the answer that surfaces.
func GitAuthEnv(configPath, token string) []string {
	env := []string{"GIT_TERMINAL_PROMPT=0"}
	if token != "" {
		env = append(env, "AUTH_TOKEN="+token, "GIT_HTTPS_TOKEN="+token)
	}
	if configPath != "" {
		env = append(env, "GIT_CONFIG_GLOBAL="+configPath)
	}

	return env
}

// gitconfigFilePattern names the gitconfig files FileGitAuthConfig leaves in a
// directory, and is what lets a sweep recognise its own leftovers.
const gitconfigFilePattern = "autoupdate-gitconfig-*"

// FileGitAuthConfig writes the same rewrites GitAuthScript exports into a
// gitconfig file, and returns its path together with the function that removes
// it.
//
// It exists because not every consumer of a credential is a shell script. The
// migration preparation runs the go command as a subprocess, and the only way
// to hand that subprocess a credential is a file GIT_CONFIG_GLOBAL points at --
// which is also why the operator's own gitconfig is copied in first: replacing
// the global file must not silently drop the settings that were in it.
//
// dir is where the file is created. It is the repository's tooling directory
// during a batch run and the OS temporary directory in local mode, so a file
// that survives a killed process is removed by the sweep that already clears
// that directory's leftovers.
func FileGitAuthConfig(dir, providerName, token string) (string, func(), error) {
	file, err := os.CreateTemp(dir, gitconfigFilePattern)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create the gitconfig: %w", err)
	}

	cleanup := func() { _ = os.Remove(file.Name()) }

	if writeErr := writeGitAuthConfig(file, readOperatorGitConfig(), providerName, token); writeErr != nil {
		_ = file.Close()
		cleanup()
		return "", nil, writeErr
	}
	if closeErr := file.Close(); closeErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to write the gitconfig: %w", closeErr)
	}

	return file.Name(), cleanup, nil
}

// writeGitAuthConfig copies the operator's gitconfig into w and appends the
// provider's rewrite rules.
func writeGitAuthConfig(w io.Writer, operatorConfig []byte, providerName, token string) error {
	if len(operatorConfig) > 0 {
		if _, err := w.Write(operatorConfig); err != nil {
			return fmt.Errorf("failed to copy the operator's gitconfig: %w", err)
		}
		if operatorConfig[len(operatorConfig)-1] != '\n' {
			if _, err := io.WriteString(w, "\n"); err != nil {
				return fmt.Errorf("failed to copy the operator's gitconfig: %w", err)
			}
		}
	}

	rule, known := gitAuthRules[providerName]
	if !known || token == "" {
		return nil
	}

	credential := strings.ReplaceAll(rule.credential, gitAuthTokenPlaceholder, token)
	for _, insteadOf := range rule.insteadOf {
		_, err := fmt.Fprintf(
			w, "[url %s]\n\tinsteadOf = %s\n",
			quoteGitConfigValue(credential), insteadOf,
		)
		if err != nil {
			return fmt.Errorf("failed to write the gitconfig: %w", err)
		}
	}

	return nil
}

// readOperatorGitConfig returns the contents of the operator's global
// gitconfig, or nothing when there is not one. A file that cannot be read is
// not an error: the run then authenticates with the rewrites alone, which is
// exactly what it does on a machine that never had a gitconfig.
func readOperatorGitConfig() []byte {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}

	data, err := os.ReadFile(filepath.Join(home, ".gitconfig"))
	if err != nil {
		return nil
	}
	return data
}

// quoteGitConfigValue renders a git config value as git's own quoting does, so
// a token containing a quote or a backslash cannot end the value early and
// turn the rest of it into directives.
func quoteGitConfigValue(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\t", `\t`,
	)

	return `"` + replacer.Replace(value) + `"`
}
