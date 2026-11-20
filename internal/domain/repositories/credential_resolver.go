package repositories

import (
	"context"

	credentialEntities "github.com/rios0rios0/gitforge/v4/pkg/credentials/domain/entities"
)

// CredentialResolver is an alias for gitforge's credentials CredentialResolver. `autoupdate .`
// asks it for the provider API's token when no --token was given; the container binds it to
// gitforge's default chain, which reads the forge's environment variables and then asks its CLI
// (gh for GitHub, az for Azure DevOps).
type CredentialResolver = credentialEntities.CredentialResolver

// GitCredentialResolver obtains the token git itself may be given when no --token was: one the
// operator exported, never one a CLI issued, because git sends it to whichever host origin's
// URLs name. The container binds gitforge's EnvCredentialResolver, the reading of the forge's
// environment variables CredentialResolver's chain starts with: in the same order, trimmed, and
// with a GitLab token declined while GITLAB_HOST or GL_HOST names another server. It is a type
// of its own, rather than a second CredentialResolver, so the two cannot be wired the wrong way
// round.
type GitCredentialResolver interface {
	Resolve(
		ctx context.Context, request credentialEntities.CredentialRequest,
	) (credentialEntities.Credential, error)
}
