package controllers

import (
	"context"

	logger "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/rios0rios0/autoupdate/internal/domain/commands"
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// RunController handles the "run" subcommand (batch mode).
type RunController struct {
	command commands.Run
}

// NewRunController creates a new RunController.
func NewRunController(command commands.Run) *RunController {
	return &RunController{command: command}
}

// GetBind returns the Cobra command metadata for the run controller.
func (it *RunController) GetBind() entities.ControllerBind {
	return entities.ControllerBind{
		Use:   "run",
		Short: "Run the dependency update engine",
		Long: `Discover repositories, scan for outdated dependencies,
and create Pull Requests.

This is the main command intended to be used in a cronjob.
It reads the configuration file, discovers repositories from
each configured provider and organization, then runs all
enabled updaters against each repository.`,
	}
}

// Execute runs the batch update mode.
func (it *RunController) Execute(cmd *cobra.Command, _ []string) {
	ctx := context.Background()

	configPath, _ := cmd.Flags().GetString("config")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	verbose, _ := cmd.Flags().GetBool("verbose")
	providerFilter, _ := cmd.Flags().GetString("provider")
	orgOverride, _ := cmd.Flags().GetString("org")
	updaterFilter, _ := cmd.Flags().GetString("updater")
	concurrency, _ := cmd.Flags().GetInt("concurrency")

	settings, err := findReadAndValidateConfig(configPath, true)
	if err != nil {
		logger.Errorf("failed to load config: %v", err)
		return
	}
	applySkipCleanupFlag(cmd, settings)

	logger.Info("Starting autoupdate run...")

	if runErr := it.command.Execute(ctx, settings, commands.RunOptions{
		DryRun:       dryRun,
		Verbose:      verbose,
		ProviderName: providerFilter,
		OrgOverride:  orgOverride,
		UpdaterName:  updaterFilter,
		Concurrency:  concurrency,
		Shard:        shardFromFlags(cmd),
	}); runErr != nil {
		logger.Errorf("Run failed: %v", runErr)
	}
}

// AddFlags adds the run-specific flags to the given Cobra command.
func (it *RunController) AddFlags(cmd *cobra.Command) {
	cmd.Flags().String("provider", "", "Only process this provider (github, gitlab, azuredevops)")
	cmd.Flags().String("org", "", "Only process this organization/group")
	cmd.Flags().String("updater", "",
		"Only run this updater (terraform, golang, python, javascript, dart, ruby, java, csharp, pipeline, dockerfile)",
	)
	cmd.Flags().Int("concurrency", 0,
		"Number of repositories to process in parallel (default 4; 1 = sequential)",
	)
	cmd.Flags().Var(&shardFlag{}, "shard",
		"Only process this shard of each organization's repositories, counted from 1 (e.g. 2/4), "+
			"to split a run across parallel CI jobs",
	)
}

// shardFromFlags returns the shard `--shard` named, or the whole run when it named none.
func shardFromFlags(cmd *cobra.Command) entities.Shard {
	if flag := cmd.Flags().Lookup("shard"); flag != nil {
		if value, ok := flag.Value.(*shardFlag); ok {
			return value.shard
		}
	}
	return entities.Shard{}
}

// shardFlag is the value behind `--shard`. Cobra sets it while it parses the command line,
// before Execute runs, so a value that names no shard fails the command -- and the process,
// with a non-zero status -- before the configuration is read or any provider is asked for a
// repository. Logging the mistake and carrying on would run the whole organization in every
// job that made it.
type shardFlag struct {
	shard entities.Shard
}

// String implements pflag.Value.
func (f *shardFlag) String() string {
	return f.shard.String()
}

// Set implements pflag.Value, accepting exactly what entities.ParseShard accepts.
func (f *shardFlag) Set(value string) error {
	shard, err := entities.ParseShard(value)
	if err != nil {
		return err
	}
	f.shard = shard
	return nil
}

// Type implements pflag.Value. It is what the help prints as the flag's argument.
func (f *shardFlag) Type() string {
	return "index/total"
}
