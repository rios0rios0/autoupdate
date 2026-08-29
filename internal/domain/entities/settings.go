package entities

import (
	"errors"
	"fmt"
	"maps"
	"path"
	"strings"

	configEntities "github.com/rios0rios0/gitforge/pkg/config/domain/entities"
)

// DefaultConfigURL is the URL to the default autoupdate configuration file.
const DefaultConfigURL = "https://raw.githubusercontent.com/rios0rios0/autoupdate/" +
	"main/configs/autoupdate.yaml"

// DefaultAggregateBranchPrefix is the prefix of the consolidated branch each run
// creates. The run date is appended to it, giving "chore/autoupdate-YYYY-MM-DD",
// and stale-branch cleanup matches the same prefix.
const DefaultAggregateBranchPrefix = "chore/autoupdate-"

// ProviderConfig is a type alias for gitforge's ProviderConfig, preserving backward compatibility.
type ProviderConfig = configEntities.ProviderConfig

// Settings is the top-level configuration for autoupdate, loaded from YAML.
type Settings struct {
	Providers       []ProviderConfig         `yaml:"providers"`
	Updaters        map[string]UpdaterConfig `yaml:"updaters"`
	ExcludeForks    bool                     `yaml:"exclude_forks"`
	ExcludeArchived bool                     `yaml:"exclude_archived"`
	ExcludeRepos    []string                 `yaml:"exclude_repos"`
	// Concurrency is how many repositories are processed in parallel within an
	// organization. Zero (the default) lets the run command pick a sensible
	// built-in default; 1 forces fully sequential processing. A CLI flag, when
	// provided, takes precedence over this value.
	Concurrency int `yaml:"concurrency"`
	// CleanupStaleBranches controls whether the aggregate branches left over by
	// earlier runs are deleted, and their pull requests closed, before the branch
	// for the current run is created. Nil (not set in config) defaults to true.
	CleanupStaleBranches *bool `yaml:"cleanup_stale_branches"`
	// AggregateBranchPrefix overrides the prefix of the dated aggregate branch. The
	// same value decides which branches the cleanup above removes.
	AggregateBranchPrefix  string `yaml:"aggregate_branch_prefix"`
	GpgKeyPath             string `yaml:"gpg_key_path"`
	GpgKeyPassphrase       string `yaml:"gpg_key_passphrase"`
	GitHubAccessToken      string `yaml:"github_access_token"`
	GitLabAccessToken      string `yaml:"gitlab_access_token"`
	AzureDevOpsAccessToken string `yaml:"azure_devops_access_token"`
	GitLabCIJobToken       string `yaml:"-"`
}

// CleanupEnabled reports whether stale aggregate-branch cleanup should run.
// Cleanup is opt-out, so an absent setting means enabled; only an explicit
// "cleanup_stale_branches: false" (or the --skip-cleanup flag) turns it off.
func CleanupEnabled(settings *Settings) bool {
	if settings == nil || settings.CleanupStaleBranches == nil {
		return true
	}
	return *settings.CleanupStaleBranches
}

// ResolveAggregateBranchPrefix returns the configured aggregate branch prefix,
// falling back to DefaultAggregateBranchPrefix. The same prefix names the branch a
// run creates and selects the branches cleanup removes, so a custom prefix can never
// leave cleanup sweeping branches autoupdate no longer makes.
func ResolveAggregateBranchPrefix(settings *Settings) string {
	if settings != nil {
		if prefix := strings.TrimSpace(settings.AggregateBranchPrefix); prefix != "" {
			return prefix
		}
	}
	return DefaultAggregateBranchPrefix
}

// UpdaterConfig holds per-updater settings.
type UpdaterConfig struct {
	Enabled      *bool  `yaml:"enabled"`
	AutoComplete *bool  `yaml:"auto_complete"`
	TargetBranch string `yaml:"target_branch"`
}

// IsEnabled returns whether the updater is enabled.
// When Enabled is nil (not set in config), it defaults to true.
func (c UpdaterConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// IsAutoComplete returns whether auto-complete is enabled.
// When AutoComplete is nil (not set in config), it defaults to false.
func (c UpdaterConfig) IsAutoComplete() bool {
	return c.AutoComplete != nil && *c.AutoComplete
}

// ValidateSettings checks the finished configuration -- the result of folding every layer.
//
// batch selects which rules apply. `autoupdate run` needs a configured provider to have
// anything to discover; `autoupdate .` takes its provider from the repository's own
// `origin` remote and has never needed one, so demanding one there would refuse a run that
// works.
func ValidateSettings(settings *Settings, batch bool) error {
	// The provider list is validated only for `run`. Local mode takes its provider from the
	// repository's own `origin` remote and never reads it, so refusing a run over an entry it
	// will not touch costs that run its updaters and exclusions as well -- LocalController
	// answers a failed load with nil settings, deliberately, so it keeps working with none.
	if batch {
		if err := validateProviders(settings.Providers); err != nil {
			return err
		}
	}

	for i, pattern := range settings.ExcludeRepos {
		trimmed := strings.TrimSpace(pattern)
		if trimmed == "" {
			continue
		}
		if _, err := path.Match(trimmed, "probe"); err != nil {
			return fmt.Errorf("exclude_repos[%d] %q: invalid glob pattern: %w",
				i, pattern, err)
		}
	}

	return ValidateAggregateBranchPrefix(settings.AggregateBranchPrefix)
}

// validateProviders checks the discovery configuration `autoupdate run` needs.
func validateProviders(providers []ProviderConfig) error {
	if len(providers) == 0 {
		return ErrNoProvidersConfigured
	}

	for i, provider := range providers {
		if provider.Type == "" {
			return fmt.Errorf("providers[%d].type is required", i)
		}
		if provider.Token == "" {
			return fmt.Errorf(
				"providers[%d].token is required (set inline, via ${ENV_VAR}, or as file path)",
				i,
			)
		}
		if len(provider.Organizations) == 0 {
			return fmt.Errorf("providers[%d].organizations must have at least one entry", i)
		}
	}

	return nil
}

// ErrNoProvidersConfigured is returned when `autoupdate run` has nothing to discover.
var ErrNoProvidersConfigured = errors.New("at least one provider must be configured")

// ErrAggregateBranchPrefixInvalid is returned for a prefix that cannot safely be used.
var ErrAggregateBranchPrefixInvalid = errors.New("invalid aggregate branch prefix")

// protectedBranchNames are the branches an aggregate prefix must not be able to reach. A
// prefix matches by string prefix, so "main" would match "main" itself and every branch
// under a "main..." name with it.
//
//nolint:gochecknoglobals // read-only lookup table
var protectedBranchNames = map[string]struct{}{
	"main": {}, "master": {}, "develop": {}, "head": {}, "trunk": {},
}

// invalidPrefixRunes are the characters git refuses in a ref name. A prefix that cannot
// name a branch can only misbehave: it will never match one AutoUpdate created, and the
// operator will believe cleanup is running when it is matching nothing.
const invalidPrefixRunes = " \t~^:?*[\\"

// ValidateAggregateBranchPrefix checks a configured prefix before anything uses it.
//
// The prefix is not only what new branches are named after -- it is the argument to a
// destructive operation. Stale-branch cleanup deletes every remote branch that starts with
// it and closes the pull request attached to each, so a prefix wider than the operator
// meant does not produce a confusing branch name, it deletes other people's work. An
// operator's typo is as capable of that as a hostile repository would be, which is why this
// runs over their own file too.
//
// A failure is an error rather than a fallback to the default: quietly substituting the
// default would mean cleanup ran against branches the operator never named.
func ValidateAggregateBranchPrefix(prefix string) error {
	if prefix == "" {
		// Unset means DefaultAggregateBranchPrefix, which is valid by construction and
		// covered by this function's tests.
		return nil
	}

	trimmed := strings.TrimSpace(prefix)
	if trimmed == "" {
		return fmt.Errorf(
			"%w: an empty prefix matches every branch in the repository",
			ErrAggregateBranchPrefixInvalid,
		)
	}
	if trimmed != prefix {
		return fmt.Errorf(
			"%w %q: leading or trailing whitespace cannot be part of a branch name",
			ErrAggregateBranchPrefixInvalid, prefix,
		)
	}

	if err := validatePrefixShape(prefix); err != nil {
		return err
	}

	if _, protected := protectedBranchNames[strings.ToLower(prefix)]; protected {
		return fmt.Errorf(
			"%w %q: that is a protected branch name, and cleanup deletes what the prefix matches",
			ErrAggregateBranchPrefixInvalid, prefix,
		)
	}

	return nil
}

// validatePrefixShape enforces what the prefix has to look like: a name git accepts, and a
// namespace it cannot escape from.
func validatePrefixShape(prefix string) error {
	if strings.ContainsAny(prefix, invalidPrefixRunes) ||
		strings.Contains(prefix, "..") || strings.Contains(prefix, "//") ||
		strings.HasPrefix(prefix, "-") || strings.HasPrefix(prefix, "/") ||
		strings.HasSuffix(prefix, ".lock") || prefix == "@" {
		return fmt.Errorf(
			"%w %q: it is not a name git will accept for a branch",
			ErrAggregateBranchPrefixInvalid, prefix,
		)
	}

	for _, r := range prefix {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf(
				"%w: control characters cannot be part of a branch name",
				ErrAggregateBranchPrefixInvalid,
			)
		}
	}

	if strings.HasPrefix(prefix, "refs/") {
		return fmt.Errorf(
			"%w %q: cleanup matches short branch names, so a refs/ prefix silently matches "+
				"nothing and leaves cleanup looking enabled while it does nothing; use %q",
			ErrAggregateBranchPrefixInvalid, prefix, DefaultAggregateBranchPrefix,
		)
	}

	slash := strings.LastIndex(prefix, "/")
	if slash < 0 {
		return fmt.Errorf(
			"%w %q: it must contain a %q so the match cannot escape into the repository's "+
				"ordinary branch names; the default is %q",
			ErrAggregateBranchPrefixInvalid, prefix, "/", DefaultAggregateBranchPrefix,
		)
	}
	if slash == len(prefix)-1 {
		return fmt.Errorf(
			"%w %q: a bare namespace matches everything under it -- %q would match every "+
				"other tool's branches in the same namespace, AutoBump's release branches "+
				"included, and cleanup deletes what it matches. Name the branches too, as "+
				"%q does",
			ErrAggregateBranchPrefixInvalid, prefix, prefix, DefaultAggregateBranchPrefix,
		)
	}

	return nil
}

// MergeUpdatersConfig deep-merges user updater overrides into defaults.
// For each updater: nil pointer fields in the override keep the default value;
// non-nil pointer fields replace the default. Non-zero string fields replace defaults.
// New updater names not present in defaults are added wholesale.
func MergeUpdatersConfig(
	defaults, overrides map[string]UpdaterConfig,
) map[string]UpdaterConfig {
	result := make(map[string]UpdaterConfig, len(defaults))
	maps.Copy(result, defaults)

	for name, override := range overrides {
		base, exists := result[name]
		if !exists {
			result[name] = override
			continue
		}

		if override.Enabled != nil {
			base.Enabled = override.Enabled
		}
		if override.AutoComplete != nil {
			base.AutoComplete = override.AutoComplete
		}
		if override.TargetBranch != "" {
			base.TargetBranch = override.TargetBranch
		}

		result[name] = base
	}

	return result
}
