package entities

import (
	"go.uber.org/dig"
)

// RegisterProviders registers all entity providers with the DIG container.
func RegisterProviders(container *dig.Container) error {
	return nil // Settings requires a config file path, provided by controllers layer
}
