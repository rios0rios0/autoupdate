package repositories

import (
	domainRepos "github.com/rios0rios0/autoupdate/internal/domain/repositories"
)

// UpdaterRegistry manages all registered dependency updater implementations.
type UpdaterRegistry struct {
	updaters map[string]domainRepos.UpdaterRepository
}

// NewUpdaterRegistry creates an empty updater registry.
func NewUpdaterRegistry() *UpdaterRegistry {
	return &UpdaterRegistry{
		updaters: make(map[string]domainRepos.UpdaterRepository),
	}
}

// Register adds an updater under its name.
func (r *UpdaterRegistry) Register(u domainRepos.UpdaterRepository) {
	r.updaters[u.Name()] = u
}

// Get returns the updater with the given name, or nil if not registered.
func (r *UpdaterRegistry) Get(name string) domainRepos.UpdaterRepository {
	return r.updaters[name]
}

// All returns every registered updater.
func (r *UpdaterRegistry) All() []domainRepos.UpdaterRepository {
	result := make([]domainRepos.UpdaterRepository, 0, len(r.updaters))
	for _, u := range r.updaters {
		result = append(result, u)
	}
	return result
}

// Names returns the list of registered updater names.
func (r *UpdaterRegistry) Names() []string {
	names := make([]string, 0, len(r.updaters))
	for name := range r.updaters {
		names = append(names, name)
	}
	return names
}
