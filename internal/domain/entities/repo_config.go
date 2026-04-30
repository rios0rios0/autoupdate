package entities

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// RepoConfigFile is the file name autoupdate looks for in a target
// repository's root to read per-repository configuration.
const RepoConfigFile = ".autoupdate.yaml"

// RepoConfig is the schema for a target repository's .autoupdate.yaml.
// It lets a project opt out of automated updates without touching the
// global autoupdate configuration. When Skip is true the repository is
// short-circuited before any updater runs.
type RepoConfig struct {
	Skip   bool   `yaml:"skip"`
	Reason string `yaml:"reason"`
}

// IsSkipped reports whether the repository configuration requests that
// autoupdate skip this project entirely.
func (c *RepoConfig) IsSkipped() bool {
	return c != nil && c.Skip
}

// ParseRepoConfig decodes raw YAML bytes into a RepoConfig. Empty input
// returns a zero-value config so callers can treat "no file" and "empty
// file" as equivalent.
func ParseRepoConfig(data []byte) (*RepoConfig, error) {
	var cfg RepoConfig
	if len(data) == 0 {
		return &cfg, nil
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", RepoConfigFile, err)
	}
	return &cfg, nil
}
