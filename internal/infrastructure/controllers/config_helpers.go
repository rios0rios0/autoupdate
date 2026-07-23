package controllers

import (
	"fmt"

	configHelpers "github.com/rios0rios0/gitforge/pkg/config/domain/helpers"
	downloadHelpers "github.com/rios0rios0/gitforge/pkg/config/infrastructure/helpers"
	logger "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// applySkipCleanupFlag turns off stale aggregate-branch cleanup when --skip-cleanup is
// set. The flag is a per-run override, so it wins over the configuration file; without it
// the configured value stands, and cleanup stays enabled when nothing is configured.
func applySkipCleanupFlag(cmd *cobra.Command, settings *entities.Settings) {
	skipCleanup, _ := cmd.Flags().GetBool("skip-cleanup")
	if !skipCleanup {
		return
	}

	disabled := false
	settings.CleanupStaleBranches = &disabled
	logger.Info("Stale branch cleanup is disabled for this run by --skip-cleanup")
}

// downloadDefaultConfig fetches and decodes the default autoupdate configuration.
func downloadDefaultConfig() (*entities.Settings, error) {
	data, err := downloadHelpers.DownloadFile(entities.DefaultConfigURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download default config: %w", err)
	}
	cfg, err := entities.DecodeSettings(data, false)
	if err != nil {
		return nil, fmt.Errorf("failed to decode default config: %w", err)
	}
	return cfg, nil
}

// findReadAndValidateConfig finds, reads, validates the config file,
// and merges updater defaults from the remote default config.
func findReadAndValidateConfig(configPath string) (*entities.Settings, error) {
	if configPath == "" {
		var err error
		configPath, err = configHelpers.FindConfigFile("autoupdate")
		if err != nil {
			return nil, fmt.Errorf(
				"no config file found: %w\nSpecify one with --config or create autoupdate.yaml",
				err,
			)
		}
	}

	logger.Infof("Using config file: %s", configPath)

	settings, err := entities.NewSettings(configPath)
	if err != nil {
		return nil, err
	}

	defaultConfig, defaultErr := downloadDefaultConfig()
	if defaultErr != nil {
		logger.Warnf("Could not download default config: %v", defaultErr)
	}

	switch {
	case settings.Updaters == nil && defaultErr != nil:
		logger.Warn(
			"Missing updaters key and could not download defaults; all updaters will run with zero-value config",
		)
		settings.Updaters = make(map[string]entities.UpdaterConfig)
	case settings.Updaters == nil && defaultConfig != nil:
		logger.Info("Missing updaters key, using the default configuration")
		settings.Updaters = defaultConfig.Updaters
	case defaultConfig != nil:
		settings.Updaters = entities.MergeUpdatersConfig(
			defaultConfig.Updaters, settings.Updaters,
		)
	}

	return settings, nil
}
