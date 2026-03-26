//go:build unit

package repositorydoubles

import "context"

// StubVersionFetcher is a test double that returns a pre-configured version.
type StubVersionFetcher struct {
	Version string
	Err     error
}

// FetchLatestVersion returns the pre-configured version or error.
func (s *StubVersionFetcher) FetchLatestVersion(_ context.Context) (string, error) {
	return s.Version, s.Err
}
