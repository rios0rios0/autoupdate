package gitlocal

import (
	"slices"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	globalEntities "github.com/rios0rios0/gitforge/v4/pkg/global/domain/entities"
	registryInfra "github.com/rios0rios0/gitforge/v4/pkg/registry/infrastructure"
)

// collectTokens returns all possible tokens for authentication, ordered by
// priority: provider discovery token > global token by service type >
// CI_JOB_TOKEN fallback for GitLab, when cloneURL is on the server that issued
// it (jobTokenAccepted).
// Mirrors autobump's collectTokens() pattern.
func collectTokens(
	serviceType globalEntities.ServiceType,
	providerToken string,
	settings *entities.Settings,
	cloneURL string,
) []string {
	var tokens []string

	if providerToken != "" {
		tokens = append(tokens, providerToken)
	}

	switch serviceType { //nolint:exhaustive // only three service types have global tokens
	case globalEntities.GITHUB:
		if settings.GitHubAccessToken != "" {
			tokens = appendUnique(tokens, settings.GitHubAccessToken)
		}
	case globalEntities.GITLAB:
		if settings.GitLabAccessToken != "" {
			tokens = appendUnique(tokens, settings.GitLabAccessToken)
		}
		if settings.GitLabCIJobToken != "" && jobTokenAccepted(settings.GitLabCIServerHost, cloneURL) {
			tokens = appendUnique(tokens, settings.GitLabCIJobToken)
		}
	case globalEntities.AZUREDEVOPS:
		if settings.AzureDevOpsAccessToken != "" {
			tokens = appendUnique(tokens, settings.AzureDevOpsAccessToken)
		}
	}

	return tokens
}

// jobTokenAccepted reports whether git may send a GitLab CI job token to
// cloneURL's host. Only the server that issued the token accepts it, the CI
// server the job runs on (ciServerHost); with none known the token is not sent
// at all. Anywhere else it is a credential for the wrong server: gitforge
// matches GitLab by a substring of the URL, so a self-managed runner's token
// would otherwise reach gitlab.com, or a gitlab.com job's token another server.
func jobTokenAccepted(ciServerHost, cloneURL string) bool {
	if ciServerHost == "" {
		return false
	}
	endpoint, err := transport.NewEndpoint(cloneURL)
	return err == nil && strings.EqualFold(endpoint.Host, ciServerHost)
}

// CollectBatchAuthMethods creates providers with each available token and
// collects all authentication methods, enabling multi-token auth retry.
// Mirrors autobump's collectAuthMethods() pattern (service.go:827-855).
func CollectBatchAuthMethods(
	resolver PushAuthResolver,
	serviceType globalEntities.ServiceType,
	providerToken string,
	settings *entities.Settings,
	cloneURL string,
) []transport.AuthMethod {
	tokens := collectTokens(serviceType, providerToken, settings, cloneURL)
	name := registryInfra.ServiceTypeToProviderName(serviceType)
	if name == "" || resolver == nil {
		return nil
	}

	var authMethods []transport.AuthMethod
	for _, token := range tokens {
		lgap, err := resolver.GetAuthProvider(serviceType, token)
		if err != nil {
			logger.Warnf("Failed to create auth provider for %v with token: %v", serviceType, err)
			continue
		}
		lgap.ConfigureTransport()
		methods := lgap.GetAuthMethods("")
		authMethods = append(authMethods, methods...)
	}

	return authMethods
}

// ResolveServiceTypeFromURL resolves the ServiceType for a given clone URL
// using the PushAuthResolver's adapter matching.
func ResolveServiceTypeFromURL(resolver PushAuthResolver, cloneURL string) globalEntities.ServiceType {
	if resolver == nil {
		return globalEntities.UNKNOWN
	}

	adapter := resolver.GetAdapterByURL(cloneURL)
	if adapter == nil {
		return globalEntities.UNKNOWN
	}

	return adapter.GetServiceType()
}

// appendUnique appends a value to the slice only if it's not already present.
func appendUnique(slice []string, value string) []string {
	if slices.Contains(slice, value) {
		return slice
	}
	return append(slice, value)
}
