package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	logger "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for autoupdate.
type Config struct {
	Providers []ProviderConfig         `yaml:"providers"`
	Updaters  map[string]UpdaterConfig `yaml:"updaters"`
}

// ProviderConfig describes a single Git hosting provider instance.
type ProviderConfig struct {
	Type          string   `yaml:"type"`          // "github", "gitlab", "azuredevops"
	Token         string   `yaml:"token"`         // Inline, ${ENV_VAR}, or file path
	Organizations []string `yaml:"organizations"` // Org names or URLs
}

// UpdaterConfig holds per-updater settings.
type UpdaterConfig struct {
	Enabled      bool   `yaml:"enabled"`
	AutoComplete bool   `yaml:"auto_complete"`
	TargetBranch string `yaml:"target_branch"`
}

// envVarPattern matches ${VAR_NAME} placeholders.
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)}`)

// Load reads and parses a configuration file, expanding environment variables
// and resolving token file paths.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	var cfg Config
	if unmarshalErr := yaml.Unmarshal(data, &cfg); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", unmarshalErr)
	}

	// Resolve tokens (env vars and file paths)
	for i := range cfg.Providers {
		cfg.Providers[i].Token = resolveToken(cfg.Providers[i].Token)
	}

	if validateErr := validate(&cfg); validateErr != nil {
		return nil, validateErr
	}

	return &cfg, nil
}

// FindConfigFile searches for a configuration file in standard locations.
// Returns the path to the first file found or an error if none is found.
func FindConfigFile() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	}

	locations := []string{
		".",
		".config",
		"configs",
	}
	if homeDir != "" {
		locations = append(
			locations,
			homeDir,
			filepath.Join(homeDir, ".config"),
		)
	}

	patterns := []string{
		".autoupdate.yaml",
		".autoupdate.yml",
		"autoupdate.yaml",
		"autoupdate.yml",
	}

	for _, loc := range locations {
		for _, pat := range patterns {
			p := filepath.Join(loc, pat)
			if _, statErr := os.Stat(p); statErr == nil {
				return p, nil
			}
		}
	}

	return "", errors.New("config file not found in default locations")
}

// resolveToken expands environment variable references (${VAR}) and, if the
// resulting string is a path to an existing file, reads the token from the file.
func resolveToken(raw string) string {
	if raw == "" {
		return raw
	}

	// Expand ${ENV_VAR} references
	resolved := envVarPattern.ReplaceAllStringFunc(raw, func(match string) string {
		varName := envVarPattern.FindStringSubmatch(match)[1]
		if val := os.Getenv(varName); val != "" {
			return val
		}
		logger.Warnf("Environment variable %q is not set", varName)
		return ""
	})

	// If the resolved value is a path to an existing file, read the token from it
	if _, statErr := os.Stat(resolved); statErr == nil {
		data, readErr := os.ReadFile(resolved)
		if readErr != nil {
			logger.Warnf("Failed to read token file %q: %v", resolved, readErr)
			return resolved
		}
		logger.Infof("Read token from file %q", resolved)
		return strings.TrimSpace(string(data))
	}

	return resolved
}

// validate checks for required configuration values.
func validate(cfg *Config) error {
	if len(cfg.Providers) == 0 {
		return errors.New("at least one provider must be configured")
	}

	for i, p := range cfg.Providers {
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
