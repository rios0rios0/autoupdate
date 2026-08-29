package controllers

import (
	"fmt"
	"os"

	logger "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/rios0rios0/autoupdate/configs"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	configHelpers "github.com/rios0rios0/gitforge/pkg/config/domain/helpers"
	downloadHelpers "github.com/rios0rios0/gitforge/pkg/config/infrastructure/helpers"
)

// applySkipCleanupFlag turns off stale branch cleanup when --skip-cleanup is set.
// The flag is a per-run override, so it wins over the configuration file; without it the
// configured value stands, and cleanup stays enabled when nothing is configured at all.
func applySkipCleanupFlag(cmd *cobra.Command, settings *entities.Settings) {
	skipCleanup, _ := cmd.Flags().GetBool("skip-cleanup")
	if !skipCleanup {
		return
	}

	disabled := false
	settings.CleanupStaleBranches = &disabled
	logger.Info("Stale branch cleanup is disabled for this run by --skip-cleanup")
}

// resolveOperatorConfigPath decides which file holds the operator's configuration: the one
// named with --config, or the one in their home directory.
//
// An empty path with a nil error means the operator keeps no configuration, and that is no
// longer an error: the built-in defaults are the base of every run, so there is nothing
// left to fall back to.
//
// The working directory is deliberately not searched. AutoUpdate runs against a local
// repository as `autoupdate .`, with that repository as the working directory, and a target
// repository may carry its own `.autoupdate.yaml` -- same file name, narrower schema.
// Reading it as the operator's configuration substitutes a project's settings for an
// operator's: it would be decoded strictly although a project file is allowed to be
// partial, its `providers` and credential keys would be honoured where the project layer
// bans them, and no providers, updaters or tokens would come out the other side.
func resolveOperatorConfigPath(configPath string) string {
	if configPath != "" {
		return configPath
	}

	logger.Debug("No config file specified, searching the operator's home directory")

	path, err := configHelpers.FindGlobalConfigFile("autoupdate")
	if err != nil {
		logger.Infof(
			"No configuration found in the home directory (%v); running on AutoUpdate's "+
				"built-in defaults. Name your own with --config if you keep it elsewhere",
			err,
		)
		return ""
	}

	logger.Infof("Using config file: %s", path)

	return path
}

// configLoader assembles the configuration layers and folds them.
//
// fetch is a field rather than a direct call so that a test can supply an offline one.
// Everything below it takes bytes, so the layering itself is testable without a network at
// all -- which the old shape was not.
type configLoader struct {
	fetch func(url string) ([]byte, error)
}

// newConfigLoader returns the loader the application uses.
func newConfigLoader() configLoader {
	return configLoader{fetch: downloadHelpers.DownloadFile}
}

// layers returns the three operator-facing configuration layers, in override order.
//
// The target repository's own `.autoupdate.yaml` is the fourth and is not here: in `run`
// mode it is fetched per repository over the provider API, and in local mode it is read
// from the repository on disk.
func (l configLoader) layers(configPath string) ([]entities.ConfigLayer, error) {
	//nolint:exhaustruct // each layer sets only the fields that distinguish it
	layers := []entities.ConfigLayer{
		{
			Name:   entities.LayerBuiltInDefaults,
			Data:   configs.Default,
			Scope:  entities.ScopeRestricted,
			Strict: true,
		},
	}

	// The published defaults are the same document served from `main`, so a change reaches
	// an installed binary without a release. They are restricted and optional: bytes
	// fetched over the network have no business naming a token, a provider or a branch
	// prefix, and an unreachable GitHub must not stop a run.
	if data, err := l.fetch(entities.DefaultConfigURL); err == nil {
		//nolint:exhaustruct // Strict stays false: `main` may be newer than this binary
		layers = append(layers, entities.ConfigLayer{
			Name:     entities.LayerPublishedDefaults,
			Origin:   entities.DefaultConfigURL,
			Data:     data,
			Scope:    entities.ScopeRestricted,
			Optional: true,
		})
	} else {
		logger.Debugf(
			"Could not fetch the published defaults (%v); running on the built-in ones", err,
		)
	}

	operatorConfigPath := resolveOperatorConfigPath(configPath)
	if operatorConfigPath == "" {
		return layers, nil
	}

	data, err := os.ReadFile(operatorConfigPath)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to read the %s (%s): %w", entities.LayerOperatorConfig, operatorConfigPath, err,
		)
	}

	//nolint:exhaustruct // Optional stays false: a file the operator named must be read
	layers = append(layers, entities.ConfigLayer{
		Name:   entities.LayerOperatorConfig,
		Origin: operatorConfigPath,
		Data:   data,
		Scope:  entities.ScopeOperator,
		Strict: true,
	})

	return layers, nil
}

// resolve folds the operator-facing layers, resolves the secrets and validates the result.
func (l configLoader) resolve(configPath string, batch bool) (*entities.Settings, error) {
	layers, err := l.layers(configPath)
	if err != nil {
		return nil, err
	}

	settings, err := entities.ResolveSettings(layers)
	if err != nil {
		return nil, err
	}

	if err = entities.ValidateSettings(settings, batch); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return settings, nil
}

// findReadAndValidateConfig resolves the configuration a command runs on. batch selects the
// validation rules: `autoupdate run` needs a configured provider to have anything to
// discover, `autoupdate .` does not.
func findReadAndValidateConfig(configPath string, batch bool) (*entities.Settings, error) {
	return newConfigLoader().resolve(configPath, batch)
}
