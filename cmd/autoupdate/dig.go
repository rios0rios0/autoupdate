package main

import (
	"github.com/rios0rios0/autoupdate/internal"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/controllers"
	"go.uber.org/dig"
)

// injectApp builds the object graph once and hands back both roots the CLI needs: the
// aggregated controllers that become subcommands, and the controller behind the root
// command's positional-path form.
//
// One container, not two. The pair that used to be here each ran RegisterProviders on their
// own dig.Container, so anything meant to be a singleton existed twice -- and `local` being
// both a subcommand and the root form was the only reason to keep them apart.
func injectApp() (*internal.AppInternal, *controllers.LocalController) {
	container := dig.New()

	if err := internal.RegisterProviders(container); err != nil {
		panic(err)
	}

	var (
		appInternal     *internal.AppInternal
		localController *controllers.LocalController
	)
	if err := container.Invoke(func(
		app *internal.AppInternal, local *controllers.LocalController,
	) {
		appInternal = app
		localController = local
	}); err != nil {
		panic(err)
	}

	return appInternal, localController
}
