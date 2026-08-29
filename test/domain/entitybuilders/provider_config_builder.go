package entitybuilders

import (
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	testkit "github.com/rios0rios0/testkit/pkg/test"
)

// ProviderConfigBuilder helps create test provider configurations with a fluent interface.
type ProviderConfigBuilder struct {
	*testkit.BaseBuilder

	providerType  string
	token         string
	organizations []string
}

// NewProviderConfigBuilder creates a new provider config builder with sensible defaults.
func NewProviderConfigBuilder() *ProviderConfigBuilder {
	return &ProviderConfigBuilder{
		BaseBuilder:   testkit.NewBaseBuilder(),
		providerType:  defaultProviderType,
		token:         "test-token",
		organizations: []string{defaultOrganization},
	}
}

// WithType sets the provider type.
func (b *ProviderConfigBuilder) WithType(t string) *ProviderConfigBuilder {
	b.providerType = t
	return b
}

// WithToken sets the authentication token.
func (b *ProviderConfigBuilder) WithToken(token string) *ProviderConfigBuilder {
	b.token = token
	return b
}

// WithOrganizations sets the organizations list.
func (b *ProviderConfigBuilder) WithOrganizations(orgs []string) *ProviderConfigBuilder {
	b.organizations = orgs
	return b
}

// Build creates the provider config (satisfies testkit.Builder interface).
func (b *ProviderConfigBuilder) Build() any {
	return b.BuildProviderConfig()
}

// BuildProviderConfig creates the provider config with a concrete return type.
func (b *ProviderConfigBuilder) BuildProviderConfig() entities.ProviderConfig {
	return entities.ProviderConfig{
		Type:          b.providerType,
		Token:         b.token,
		Organizations: b.organizations,
	}
}

// Reset clears the builder state, allowing it to be reused.
func (b *ProviderConfigBuilder) Reset() testkit.Builder {
	b.BaseBuilder.Reset()
	b.providerType = "github"
	b.token = "test-token"
	b.organizations = []string{"test-org"}
	return b
}

// Clone creates a deep copy of the ProviderConfigBuilder.
func (b *ProviderConfigBuilder) Clone() testkit.Builder {
	orgs := make([]string, len(b.organizations))
	copy(orgs, b.organizations)

	return &ProviderConfigBuilder{
		BaseBuilder:   cloneBase(b.BaseBuilder),
		providerType:  b.providerType,
		token:         b.token,
		organizations: orgs,
	}
}
