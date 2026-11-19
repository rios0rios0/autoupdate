package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	logger "github.com/sirupsen/logrus"

	configHelpers "github.com/rios0rios0/gitforge/v4/pkg/config/domain/helpers"
	credentialEntities "github.com/rios0rios0/gitforge/v4/pkg/credentials/domain/entities"
	globalEntities "github.com/rios0rios0/gitforge/v4/pkg/global/domain/entities"
)

// ErrNoPushToken reports an HTTPS origin with no token git may be given. git can only push to
// an HTTPS remote with a token, and only --token or the forge's environment variables qualify:
// a token the forge's CLI issued is kept for the provider API (see localTokens).
var ErrNoPushToken = errors.New("no token to push to an HTTPS origin with")

// localTokens are the tokens a local run authenticates with, kept apart by where each one may
// be sent.
type localTokens struct {
	// API authenticates gitforge's provider API calls: opening the pull request, and the
	// provider a Go migration branch is committed through. Those requests go to the forge's own
	// API host, so this may be a token the forge's CLI issued.
	API string
	// Git authenticates git itself: the HTTPS push to origin, and the credential rewrites the
	// upgrade scripts fetch private dependencies through. It is only ever --token or the forge's
	// environment variable, as it was before the CLI fallback existed: gitforge matches a remote
	// to a forge by a substring of its URL, so origin may name any host whose URL merely contains
	// "github.com", and a token the operator never exported must not travel there.
	Git string
}

// resolveLocalTokens decides which token the provider API and git each receive, and logs where
// the credential came from -- never the token itself.
//
// --token is the operator's explicit choice and serves both. Without it, git reads the
// environment alone and the API token comes from the credential resolver (the forge's
// environment variables, then its CLI), so a CLI-issued token never reaches git. An HTTPS
// origin is the one remote git needs a token for, so when the environment has none it is
// refused here: before the upgrade runs rather than at the push after it, and before the CLI
// is asked for a token the run could not use.
func (it *LocalCommand) resolveLocalTokens(
	ctx context.Context, opts LocalOptions, remote *remoteInfo,
) (localTokens, error) {
	forge := remote.ServiceType.DisplayName()
	if opts.Token != "" {
		logger.Infof("Authenticating to %s with the --token flag", forge)
		return localTokens{API: opts.Token, Git: opts.Token}, nil
	}

	gitToken := configHelpers.ResolveTokenFromEnv(remote.ServiceType)
	if gitToken == "" && pushesWithToken(remote.PushURL) {
		return localTokens{}, fmt.Errorf(
			"%w: pass --token or set %s, or switch origin to SSH; a token the forge's CLI issues "+
				"is only sent to the %s API, never to a git remote",
			ErrNoPushToken, configHelpers.TokenEnvHint(remote.ServiceType), forge,
		)
	}

	credential, err := it.credentials.Resolve(ctx, credentialRequestFor(remote))
	if err != nil {
		return localTokens{}, fmt.Errorf("cannot authenticate to %s without --token: %w", forge, err)
	}

	logger.Infof("Authenticating to %s with the %s", forge, credential.Source)
	return localTokens{API: credential.Token, Git: gitToken}, nil
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

// pushesWithToken reports whether git authenticates to the remote with a token, which is how
// gitforge's PushWithTransportDetection pushes to an http(s) URL. An SSH remote is pushed to
// with the SSH agent instead.
func pushesWithToken(remoteURL string) bool {
	return strings.HasPrefix(remoteURL, "https://") || strings.HasPrefix(remoteURL, "http://")
}
