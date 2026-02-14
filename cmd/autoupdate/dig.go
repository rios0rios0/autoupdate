package main

import (
	"github.com/rios0rios0/autoupdate/internal"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/controllers"
	"go.uber.org/dig"
)

func injectAppContext() *internal.AppInternal {
	container := dig.New()

	// Register all providers
	if err := internal.RegisterProviders(container); err != nil {
		panic(err)
	}

	// Invoke to get AppInternal
	var appInternal *internal.AppInternal
	if err := container.Invoke(func(ai *internal.AppInternal) {
		appInternal = ai
	}); err != nil {
		panic(err)
	}

	return appInternal
}

func injectLocalController() *controllers.LocalController {
	container := dig.New()

	if err := internal.RegisterProviders(container); err != nil {
		panic(err)
	}

	var localController *controllers.LocalController
	if err := container.Invoke(func(lc *controllers.LocalController) {
		localController = lc
	}); err != nil {
		panic(err)
	}

	return localController
}
