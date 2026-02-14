package commands

import (
	"go.uber.org/dig"
)

// RegisterProviders registers all command providers with the DIG container.
func RegisterProviders(container *dig.Container) error {
	// Register command constructors
	if err := container.Provide(NewRunCommand); err != nil {
		return err
	}
	if err := container.Provide(NewLocalCommand); err != nil {
		return err
	}

	// Bind interfaces to implementations
	if err := container.Provide(func(impl *RunCommand) Run {
		return impl
	}); err != nil {
		return err
	}
	if err := container.Provide(func(impl *LocalCommand) Local {
		return impl
	}); err != nil {
		return err
	}

	return nil
}
