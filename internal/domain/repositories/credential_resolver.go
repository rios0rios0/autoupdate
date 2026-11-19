package repositories

import (
	credentialEntities "github.com/rios0rios0/gitforge/v4/pkg/credentials/domain/entities"
)

// CredentialResolver is an alias for gitforge's credentials CredentialResolver. `autoupdate .`
// asks it for a forge token when no --token was given; the container binds it to gitforge's
// default chain, which reads the forge's environment variables and then asks its CLI (gh for
// GitHub, az for Azure DevOps).
type CredentialResolver = credentialEntities.CredentialResolver
