package support

import (
	"net/url"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// RepositoryWebURL returns the browser URL of a repository: what a reader
// clicks to see the code, and the base every link built on top of it hangs
// from.
//
// The clone URL is preferred when it is an HTTP one, because it already names
// the repository the way its host does -- project segment included, which the
// organization-and-name form cannot express for Azure DevOps. It is stripped of
// the credential a token-authenticated clone URL carries, of its query, and of
// the `.git` suffix, none of which belong in a page a person opens.
//
// An SSH remote is not parseable this way, so the URL is constructed from the
// provider instead. An unknown provider yields nothing rather than a guess: a
// link that 404s is worse than no link at all, since it looks like the tool
// knew where it was pointing.
func RepositoryWebURL(repo entities.Repository, providerName string) string {
	if webURL := webURLFromRemote(repo.RemoteURL); webURL != "" {
		return webURL
	}

	organization := strings.TrimSpace(repo.Organization)
	name := strings.TrimSpace(repo.Name)
	if organization == "" || name == "" {
		return ""
	}

	switch providerName {
	case ProviderGitHub:
		return "https://github.com/" + organization + "/" + name
	case ProviderGitLab:
		return "https://gitlab.com/" + organization + "/" + name
	case ProviderAzureDevOps:
		return azureDevOpsWebURL(organization, strings.TrimSpace(repo.Project), name)
	default:
		return ""
	}
}

// azureDevOpsWebURL builds an Azure DevOps repository URL from its parts. The
// project segment is omitted when it is unknown, which is the short form Azure
// DevOps itself resolves for a repository whose project carries its name.
func azureDevOpsWebURL(organization, project, name string) string {
	base := "https://dev.azure.com/" + organization + "/"
	if project != "" {
		base += project + "/"
	}
	return base + "_git/" + name
}

// webURLFromRemote turns an HTTP(S) clone URL into the address of the
// repository's web page, or returns nothing when the remote is not one.
func webURLFromRemote(remoteURL string) string {
	trimmed := strings.TrimSpace(remoteURL)
	if trimmed == "" {
		return ""
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return ""
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return ""
	}

	// Userinfo is where the clone URL carries its token; a page a person opens
	// must never have one in it, not even a scrubbed placeholder.
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimSuffix(strings.TrimSuffix(parsed.Path, "/"), ".git")

	return parsed.String()
}

// PullRequestCreationURL returns the provider's "open a pull request" page for
// a branch that already exists, prefilled with the branch as the source and
// base as the target.
//
// Providers can do this because the branch is the expensive half: it is
// already pushed, so "create the pull request" is a form the host fills in
// rather than an operation that needs an API call and a token. That is what
// lets the link sit in a pull request description, which is static text.
//
// A provider with no such page yields nothing, and the caller renders a dash
// rather than a link that would have to be guessed.
func PullRequestCreationURL(providerName, webURL, base, branch string) string {
	if webURL == "" || base == "" || branch == "" {
		return ""
	}

	switch providerName {
	case ProviderGitHub:
		return webURL + "/compare/" + escapeRefPath(base) + "..." + escapeRefPath(branch) + "?expand=1"
	case ProviderGitLab:
		query := url.Values{}
		query.Set("merge_request[source_branch]", branch)
		query.Set("merge_request[target_branch]", base)
		return webURL + "/-/merge_requests/new?" + query.Encode()
	case ProviderAzureDevOps:
		query := url.Values{}
		query.Set("sourceRef", "refs/heads/"+branch)
		query.Set("targetRef", "refs/heads/"+base)
		return webURL + "/pullrequestcreate?" + query.Encode()
	default:
		return ""
	}
}

// escapeRefPath escapes a branch name for use as a piece of a URL path,
// leaving the slashes alone: a branch is a path, and a URL that renders its
// separator as %2F is one a reader cannot check at a glance.
func escapeRefPath(ref string) string {
	return (&url.URL{Path: ref}).EscapedPath()
}
