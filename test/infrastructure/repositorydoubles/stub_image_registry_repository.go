package repositorydoubles

import (
	"context"
	"sync"

	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// StubImageRegistryRepository implements repositories.ImageRegistryRepository with canned
// answers keyed by "<registry>/<repository>", and records every lookup it is asked for.
type StubImageRegistryRepository struct {
	mu sync.Mutex

	// Tags is what each registry holds for each repository. A key it does not list answers
	// an empty list, as a registry with no such repository does.
	Tags map[string][]string
	// Errs makes the lookup of a key fail.
	Errs map[string]error
	// Lookups records every "<registry>/<repository>" asked for, in order.
	Lookups []string
}

var _ repositories.ImageRegistryRepository = (*StubImageRegistryRepository)(nil)

// ListTags returns the canned answer for registry and repository.
func (s *StubImageRegistryRepository) ListTags(_ context.Context, registry, repository string) ([]string, error) {
	key := registry + "/" + repository

	s.mu.Lock()
	s.Lookups = append(s.Lookups, key)
	s.mu.Unlock()

	if err, ok := s.Errs[key]; ok {
		return nil, err
	}
	return s.Tags[key], nil
}
