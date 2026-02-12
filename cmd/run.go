package cmd

import (
	"context"
	"fmt"

	logger "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/rios0rios0/autoupdate/application"
	"github.com/rios0rios0/autoupdate/config"
	providerPkg "github.com/rios0rios0/autoupdate/infrastructure/provider"
	adoProv "github.com/rios0rios0/autoupdate/infrastructure/provider/azuredevops"
	ghProv "github.com/rios0rios0/autoupdate/infrastructure/provider/github"
	glProv "github.com/rios0rios0/autoupdate/infrastructure/provider/gitlab"
	updaterPkg "github.com/rios0rios0/autoupdate/infrastructure/updater"
	goUpdater "github.com/rios0rios0/autoupdate/infrastructure/updater/golang"
	jsUpdater "github.com/rios0rios0/autoupdate/infrastructure/updater/javascript"
	pyUpdater "github.com/rios0rios0/autoupdate/infrastructure/updater/python"
	tfUpdater "github.com/rios0rios0/autoupdate/infrastructure/updater/terraform"
)

//nolint:gochecknoglobals // required by cobra CLI pattern
var (
	providerFilter string
	orgOverride    string
	updaterFilter  string
)

//nolint:gochecknoglobals // required by cobra CLI pattern
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the dependency update engine",
	Long: `Discover repositories, scan for outdated dependencies,
and create Pull Requests.

This is the main command intended to be used in a cronjob.
It reads the configuration file, discovers repositories from
each configured provider and organization, then runs all
enabled updaters against each repository.`,
	RunE: runUpdate,
}

//nolint:gochecknoinits // required by cobra CLI pattern
func init() {
	runCmd.Flags().StringVar(
		&providerFilter, "provider", "",
		"Only process this provider (github, gitlab, azuredevops)",
	)
	runCmd.Flags().StringVar(
		&orgOverride, "org", "",
		"Only process this organization/group",
	)
	runCmd.Flags().StringVar(
		&updaterFilter, "updater", "",
		"Only run this updater (terraform, golang, python, javascript)",
	)
	rootCmd.AddCommand(runCmd)
}

func runUpdate(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Load configuration
	cfgPath := configPath
	if cfgPath == "" {
		var err error
		cfgPath, err = config.FindConfigFile()
		if err != nil {
			return fmt.Errorf(
				"no config file found: %w\nSpecify one with --config or create autoupdate.yaml",
				err,
			)
		}
	}

	logger.Infof("Using config file: %s", cfgPath)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Build registries
	provRegistry := buildProviderRegistry()
	updRegistry := buildUpdaterRegistry()

	// Create and run the service
	svc := application.NewUpdateService(provRegistry, updRegistry)

	logger.Info("Starting autoupdate run...")

	return svc.Run(ctx, cfg, application.RunOptions{
		DryRun:       dryRun,
		Verbose:      verbose,
		ProviderName: providerFilter,
		OrgOverride:  orgOverride,
		UpdaterName:  updaterFilter,
	})
}

func buildProviderRegistry() *providerPkg.Registry {
	reg := providerPkg.NewRegistry()
	reg.Register("github", ghProv.New)
	reg.Register("gitlab", glProv.New)
	reg.Register("azuredevops", adoProv.New)
	return reg
}

func buildUpdaterRegistry() *updaterPkg.Registry {
	reg := updaterPkg.NewRegistry()
	reg.Register(tfUpdater.New())
	reg.Register(goUpdater.New())
	reg.Register(pyUpdater.New())
	reg.Register(jsUpdater.New())
	return reg
}
