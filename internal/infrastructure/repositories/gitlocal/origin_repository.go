package gitlocal

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/kevinburke/ssh_config"
)

// OriginRepository reads what a push to a checkout's origin involves: origin's URLs from the
// checkout's own git configuration, and the server an SSH host alias stands for from the
// operator's SSH client configuration. It implements repositories.OriginRepository.
type OriginRepository struct {
	// SSHHostName returns the HostName the SSH client configuration gives alias, or "" when it
	// gives none. Nil reads ~/.ssh/config and /etc/ssh/ssh_config with kevinburke/ssh_config,
	// the parser gitforge's SSH transport resolves aliases with.
	SSHHostName func(alias string) string
}

// NewOriginRepository creates an OriginRepository that reads the operator's SSH client
// configuration.
func NewOriginRepository() *OriginRepository {
	return &OriginRepository{}
}

// PushURLs returns every URL a push to origin may dial; see OriginPushURLs.
func (r *OriginRepository) PushURLs(repoDir string) ([]string, error) {
	return OriginPushURLs(repoDir)
}

// HostName returns the server remoteURL reaches: its host, or, for an SSH URL whose host is an
// alias, the HostName the SSH client configuration gives it. It is "" for a URL that names no
// host, such as a path on the local filesystem.
func (r *OriginRepository) HostName(remoteURL string) string {
	host, overSSH := remoteHost(remoteURL)
	if !overSSH || host == "" {
		return host
	}

	lookup := r.SSHHostName
	if lookup == nil {
		lookup = func(alias string) string { return ssh_config.Get(alias, "HostName") }
	}
	if resolved := strings.TrimSpace(lookup(host)); resolved != "" {
		return resolved
	}

	return host
}

// OriginPushURLs returns every URL a push to origin may dial: origin's URL list as go-git
// reads it, each `url` followed by each `pushurl`, from the repository's own configuration with
// only that configuration's insteadOf rules applied.
//
// A caller deciding whether a push needs a token has to look at all of them. go-git pushes to
// the last URL, a pushurl when there is one, while collectAuthMethods picks its adapter from
// the first, so an SSH `url` followed by an HTTPS `pushurl` is pushed over HTTPS. And git's
// `remote get-url origin` prints the fetch URL with the global and system insteadOf rules
// applied too, which go-git never reads.
func OriginPushURLs(repoDir string) ([]string, error) {
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository at %s: %w", repoDir, err)
	}

	origin, err := repo.Remote("origin")
	if err != nil {
		return nil, fmt.Errorf("failed to get origin remote: %w", err)
	}

	urls := origin.Config().URLs
	if len(urls) == 0 {
		return nil, fmt.Errorf("origin remote in %s has no URL", repoDir)
	}

	return slices.Clone(urls), nil
}

// remoteHost returns the host a remote URL names, and whether git reaches it over SSH: an
// `ssh://` URL, or git's scp-like `[user@]host:path`, whose colon comes before any slash.
func remoteHost(remoteURL string) (string, bool) {
	if strings.Contains(remoteURL, "://") {
		parsed, err := url.Parse(remoteURL)
		if err != nil {
			return "", false
		}
		return parsed.Hostname(), slices.Contains([]string{"ssh", "git+ssh", "ssh+git"}, parsed.Scheme)
	}

	colon := strings.Index(remoteURL, ":")
	if colon < 0 {
		return "", false
	}
	if slash := strings.Index(remoteURL, "/"); slash >= 0 && slash < colon {
		return "", false
	}

	host := remoteURL[:colon]
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}

	return host, true
}
