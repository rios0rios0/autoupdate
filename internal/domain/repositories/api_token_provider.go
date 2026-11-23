package repositories

// APITokenProvider is implemented by a provider whose AuthToken is not the token its API
// calls carry.
//
// `autoupdate .` hands such a provider to the updaters it runs on the operator's checkout:
// AuthToken answers git's token, because an updater passes AuthToken to git, while the API
// calls go out with the API's token, which the gh or az CLI may have issued and which must
// never reach git. Text a run publishes -- a pull request description, a commit message --
// is redacted with every token the run holds, so whatever redacts with a provider's
// AuthToken has to redact with its APIToken too.
type APITokenProvider interface {
	// APIToken returns the token the provider's API calls carry.
	APIToken() string
}
