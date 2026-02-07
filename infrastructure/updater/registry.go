package updater

import (
	"github.com/rios0rios0/autoupdate/domain"
)

// Registry manages all registered dependency updater implementations.
type Registry struct {
	updaters map[string]domain.Updater
}

// NewRegistry creates an empty updater registry.
func NewRegistry() *Registry {
	return &Registry{
		updaters: make(map[string]domain.Updater),
	}
}

// Register adds an updater under its name.
func (r *Registry) Register(u domain.Updater) {
	r.updaters[u.Name()] = u
}

// Get returns the updater with the given name, or nil if not registered.
func (r *Registry) Get(name string) domain.Updater {
	return r.updaters[name]
}

// All returns every registered updater.
func (r *Registry) All() []domain.Updater {
	result := make([]domain.Updater, 0, len(r.updaters))
	for _, u := range r.updaters {
		result = append(result, u)
	}
	return result
}

// Names returns the list of registered updater names.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.updaters))
	for name := range r.updaters {
		names = append(names, name)
	}
	return names
}
