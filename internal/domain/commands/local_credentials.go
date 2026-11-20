package commands

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	logger "github.com/sirupsen/logrus"

	configHelpers "github.com/rios0rios0/gitforge/v4/pkg/config/domain/helpers"
	credentialEntities "github.com/rios0rios0/gitforge/v4/pkg/credentials/domain/entities"
	globalEntities "github.com/rios0rios0/gitforge/v4/pkg/global/domain/entities"
)

// gitlabHost is the only GitLab server gitforge's GitLab provider talks to.
const gitlabHost = "gitlab.com"

var (
	// ErrNoPushToken reports an origin git would push to over HTTP(S) with no token it may be
	// given. Only --token or the forge's environment variables qualify: a token the forge's
	// CLI issued is kept for the provider API (see localTokens).
	ErrNoPushToken = errors.New("no token to push to an HTTPS origin with")

	// ErrSelfManagedGitLab reports a GitLab origin on a server other than gitlab.com. gitforge's
	// GitLab provider only talks to gitlab.com, so no merge request can be opened for it, and
	// the token the run would use -- most likely one for origin's own server -- must not be
	// sent there.
	ErrSelfManagedGitLab = errors.New("origin is not on gitlab.com")
)

// localTokens are the tokens a local run authenticates with, kept apart by where each one may
// be sent.
type localTokens struct {
	// Git authenticates git itself: the HTTPS push to origin, and the credential rewrites the
	// upgrade scripts fetch private dependencies through. It is only ever --token or the forge's
	// environment variable, as it was before the CLI fallback existed: the push picks its forge
	// by a substring of origin's first URL and then sends the token to origin's last one, so it
	// can reach any host a pushurl names, and a token the operator never exported must not.
	Git string

	// api returns the provider API's token; see API.
	api func(ctx context.Context) (string, error)
}

// API returns a token for gitforge's provider API calls: opening the pull request, and the
// provider a Go migration branch is committed through. Those requests go to the forge's own API
// host, so the token may be one the forge's CLI issued.
//
// It asks again on every call instead of keeping the token the run started with: a token the
// Azure CLI issues lasts about an hour, az can hand out a cached one with minutes left, and a
// long upgrade can outlast it.
func (t localTokens) API(ctx context.Context) (string, error) {
	if t.api == nil {
		return "", credentialEntities.ErrNoCredential
	}
	return t.api(ctx)
}

// resolveLocalTokens decides which token git and the provider API each receive, before the
// upgrade runs, and logs where the API's credential came from -- never the token itself.
//
// --token is the operator's explicit choice and serves both. Without it, git reads the
// environment alone and the API's token comes from the credential resolver (the forge's
// environment variables, then its CLI), so a CLI-issued token never reaches git. An origin with
// any http(s) URL is pushed to with a token, so when git has none it is refused here: before
// any branch or commit rather than at the push after them, and before the CLI is asked for a
// token the run could not use. The API's credential is asked for now only to fail fast;
// localTokens.API asks again whenever the token is about to be used.
func (it *LocalCommand) resolveLocalTokens(
	ctx context.Context, opts LocalOptions, remote *remoteInfo,
) (localTokens, error) {
	if err := it.refuseSelfManagedGitLab(remote); err != nil {
		return localTokens{}, err
	}

	forge := remote.ServiceType.DisplayName()
	if opts.Token != "" {
		logger.Infof("Authenticating to %s with the --token flag", forge)
		token := opts.Token
		return localTokens{
			Git: token,
			api: func(context.Context) (string, error) { return token, nil },
		}, nil
	}

	gitToken := it.environmentGitToken(ctx, remote)
	if gitToken == "" && slices.ContainsFunc(remote.PushURLs, pushesWithToken) {
		return localTokens{}, fmt.Errorf(
			"%w: pass --token or set %s, or switch origin to SSH; a token the forge's CLI issues "+
				"is only sent to the %s API, never to a git remote",
			ErrNoPushToken, configHelpers.TokenEnvHint(remote.ServiceType), forge,
		)
	}

	credential, err := it.apiCredential(ctx, remote)
	if err != nil {
		return localTokens{}, err
	}
	logger.Infof("Authenticating to %s with the %s", forge, credential.Source)

	return localTokens{
		Git: gitToken,
		api: func(ctx context.Context) (string, error) {
			fresh, freshErr := it.apiCredential(ctx, remote)
			if freshErr != nil {
				return "", freshErr
			}
			return fresh.Token, nil
		},
	}, nil
}

// environmentGitToken returns the token git may be given without --token: the forge's
// environment variable, read the way the API's chain reads it, or "" when none holds one.
func (it *LocalCommand) environmentGitToken(ctx context.Context, remote *remoteInfo) string {
	credential, err := it.gitCredentials.Resolve(ctx, credentialRequestFor(remote))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(credential.Token)
}

// apiCredential asks the credential resolver for the provider API's token. An answer with no
// token is no credential, whatever the resolver said, so it can never turn into requests that
// authenticate as nobody.
func (it *LocalCommand) apiCredential(
	ctx context.Context, remote *remoteInfo,
) (credentialEntities.Credential, error) {
	credential, err := it.credentials.Resolve(ctx, credentialRequestFor(remote))
	credential.Token = strings.TrimSpace(credential.Token)
	if err == nil && credential.Token == "" {
		err = fmt.Errorf(
			"%w: %s returned an empty token", credentialEntities.ErrNoCredential, describeSource(credential.Source),
		)
	}
	if err != nil {
		return credentialEntities.Credential{}, fmt.Errorf(
			"cannot authenticate to %s without --token: %w", remote.ServiceType.DisplayName(), err,
		)
	}
	return credential, nil
}

// refuseSelfManagedGitLab refuses a GitLab origin on a server other than gitlab.com, whatever
// token the run would use. gitforge's parser takes any SSH host containing "gitlab" for GitLab,
// and its provider only ever talks to gitlab.com, so the token -- most likely the one for
// origin's own server, whether GITLAB_HOST says so or not -- would be sent to gitlab.com to
// open a merge request on a repository that is not there.
func (it *LocalCommand) refuseSelfManagedGitLab(remote *remoteInfo) error {
	if remote.ServiceType != globalEntities.GITLAB {
		return nil
	}

	host := it.origins.HostName(remote.RemoteURL)
	if strings.EqualFold(host, gitlabHost) {
		return nil
	}

	return fmt.Errorf(
		"%w: its server is %s, but autoupdate's GitLab provider only talks to %s, so no token is "+
			"sent and no merge request can be opened (an SSH alias counts as %s when the SSH "+
			"client configuration gives it HostName %s)",
		ErrSelfManagedGitLab, host, gitlabHost, gitlabHost, gitlabHost,
	)
}

// describeSource names a credential source for an error, as "the gh CLI".
func describeSource(source string) string {
	if source == "" {
		return "the credential resolver"
	}
	return "the " + source
}

// credentialRequestFor asks for a token for origin's forge. On Azure DevOps it names the
// organization too, so the Azure CLI issues the token in that organization's Microsoft Entra
// tenant rather than in the signed-in account's default one; no other forge reads it.
func credentialRequestFor(remote *remoteInfo) credentialEntities.CredentialRequest {
	request := credentialEntities.CredentialRequest{ServiceType: remote.ServiceType}
	if remote.ServiceType == globalEntities.AZUREDEVOPS {
		request.Organization = remote.Org
	}
	return request
}

// pushesWithToken reports whether git authenticates to a remote URL with a token, which is how
// gitforge's PushWithTransportDetection pushes to an http(s) URL. An SSH remote is pushed to
// with the SSH agent instead.
func pushesWithToken(remoteURL string) bool {
	return strings.HasPrefix(remoteURL, "https://") || strings.HasPrefix(remoteURL, "http://")
}
