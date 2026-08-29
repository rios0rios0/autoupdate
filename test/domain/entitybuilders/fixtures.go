package entitybuilders

// The values every builder in this package defaults to. Named once so a test that asserts
// on them and a builder that produces them cannot drift apart.
const (
	defaultProviderType = "github"
	defaultOrganization = "test-org"
)
