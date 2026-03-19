package entities

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"strings"

	configEntities "github.com/rios0rios0/gitforge/pkg/config/domain/entities"
	"gopkg.in/yaml.v3"
)

// DefaultConfigURL is the URL to the default autoupdate configuration file.
const DefaultConfigURL = "https://raw.githubusercontent.com/rios0rios0/autoupdate/" +
	"main/configs/autoupdate.yaml"

// ProviderConfig is a type alias for gitforge's ProviderConfig, preserving backward compatibility.
type ProviderConfig = configEntities.ProviderConfig

// Settings is the top-level configuration for autoupdate, loaded from YAML.
type Settings struct {
	Providers              []ProviderConfig         `yaml:"providers"`
	Updaters               map[string]UpdaterConfig `yaml:"updaters"`
	ExcludeForks           bool                     `yaml:"exclude_forks"`
	ExcludeArchived        bool                     `yaml:"exclude_archived"`
	GpgKeyPath             string                   `yaml:"gpg_key_path"`
	GpgKeyPassphrase       string                   `yaml:"gpg_key_passphrase"`
	GitHubAccessToken      string                   `yaml:"github_access_token"`
	GitLabAccessToken      string                   `yaml:"gitlab_access_token"`
	AzureDevOpsAccessToken string                   `yaml:"azure_devops_access_token"`
	GitLabCIJobToken       string                   `yaml:"-"`
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

// NewSettings reads and parses a configuration file, expanding environment variables
// and resolving token file paths.
func NewSettings(path string) (*Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	var settings Settings
	if unmarshalErr := yaml.Unmarshal(data, &settings); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", unmarshalErr)
	}

	// Resolve tokens (env vars and file paths)
	for i := range settings.Providers {
		settings.Providers[i].Token = settings.Providers[i].ResolveToken()
	}

	// Resolve global token fields using the same ${ENV_VAR} expansion and
	// file path resolution as provider tokens (via gitforge's ResolveToken).
	settings.GpgKeyPassphrase = configEntities.ResolveToken(settings.GpgKeyPassphrase)
	settings.GitHubAccessToken = configEntities.ResolveToken(settings.GitHubAccessToken)
	settings.GitLabAccessToken = configEntities.ResolveToken(settings.GitLabAccessToken)
	settings.AzureDevOpsAccessToken = configEntities.ResolveToken(settings.AzureDevOpsAccessToken)

	settings.GitLabCIJobToken = os.Getenv("CI_JOB_TOKEN")

	if settings.GpgKeyPassphrase == "" {
		settings.GpgKeyPassphrase = os.Getenv("GPG_PASSPHRASE")
	}

	if validateErr := ValidateSettings(&settings); validateErr != nil {
		return nil, validateErr
	}

	return &settings, nil
}

// DecodeSettings decodes YAML data into a Settings struct.
// When strict is true, unknown fields cause an error (user config).
// When strict is false, unknown fields are ignored (default config).
func DecodeSettings(data []byte, strict bool) (*Settings, error) {
	var settings Settings
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(strict)
	if err := decoder.Decode(&settings); err != nil {
		return nil, fmt.Errorf("failed to decode settings: %w", err)
	}
	return &settings, nil
}

// ValidateSettings checks for required configuration values.
func ValidateSettings(settings *Settings) error {
	if len(settings.Providers) == 0 {
		return errors.New("at least one provider must be configured")
	}

	for i, p := range settings.Providers {
		if p.Type == "" {
			return fmt.Errorf("providers[%d].type is required", i)
		}
		if p.Token == "" {
			return fmt.Errorf(
				"providers[%d].token is required (set inline, via ${ENV_VAR}, or as file path)",
				i,
			)
		}
		if len(p.Organizations) == 0 {
			return fmt.Errorf(
				"providers[%d].organizations must have at least one entry",
				i,
			)
		}
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
