package internal

import (
	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/controllers"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories"
	"go.uber.org/dig"
)

// RegisterProviders registers all internal providers with the DIG container.
func RegisterProviders(container *dig.Container) error {
	// Register all layers (bottom-up: infrastructure repos -> domain entities -> domain commands -> controllers)
	if err := repositories.RegisterProviders(container); err != nil {
		return err
	}
	if err := entities.RegisterProviders(container); err != nil {
		return err
	}
	if err := commands.RegisterProviders(container); err != nil {
		return err
	}
	if err := controllers.RegisterProviders(container); err != nil {
		return err
	}

	// Register the main app internal
	if err := container.Provide(NewAppInternal); err != nil {
		return err
	}

	return nil
}
