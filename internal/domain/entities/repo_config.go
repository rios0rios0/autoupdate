package entities

import (
	"fmt"

	logger "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// RepoConfigFile is the file name autoupdate looks for in a target repository's root.
const RepoConfigFile = ".autoupdate.yaml"

// RepoConfig is the schema of a target repository's `.autoupdate.yaml`.
//
// The file has two halves that have always been one document. The opt-out marker lets a
// project take itself out of automated updates without touching the operator's
// configuration; the project layer is the last and narrowest of the four configuration
// layers, and lets it adjust the settings it is updated under rather than only refusing.
type RepoConfig struct {
	Skip   bool   `yaml:"skip"`
	Reason string `yaml:"reason"`

	// Layer is the settings half of the same document, carried as YAML rather than as
	// decoded fields on purpose. Applying it through ApplyLayer is what lets a repository
	// write `exclude_forks: false` over an operator's `true`: yaml.v3 assigns only the
	// keys a document carries, so absent and false stay distinguishable and not one field
	// has to become a pointer. A struct copied field by field could not tell those apart.
	Layer []byte `yaml:"-"`
}

// IsSkipped reports whether the repository configuration asks autoupdate to skip this
// project entirely.
func (c *RepoConfig) IsSkipped() bool {
	return c != nil && c.Skip
}

// ParseRepoConfig decodes a target repository's `.autoupdate.yaml`.
//
// Empty input returns a zero-value config, so callers can treat "no file" and "empty file"
// as the same thing. Malformed input is an error, which each caller answers in the way its
// situation deserves: local mode fails, because the user explicitly asked for that file to
// be read; run mode fails open, because a flaky API call must not silently disable every
// update in an organization.
func ParseRepoConfig(data []byte) (*RepoConfig, error) {
	var config RepoConfig //nolint:exhaustruct // the document decides what is set
	if len(data) == 0 {
		return &config, nil
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", RepoConfigFile, err)
	}

	layer, err := narrowToProjectSchema(data)
	if err != nil {
		return nil, err
	}
	config.Layer = layer

	return &config, nil
}

// projectLayerKeys is the project layer's schema: the top-level keys a target repository's
// own file may set. It is a deliberate subset of what the operator's file may say.
//
//nolint:gochecknoglobals // read-only lookup table
var projectLayerKeys = map[string]struct{}{
	"skip": {}, "reason": {},
	"updaters": {}, "exclude_repos": {},
	"cleanup_stale_branches": {},
	"allow_major_updates":    {},
	"exclude_forks":          {}, "exclude_archived": {},
}

// narrowToProjectSchema reduces a repository's document to the keys the project layer may
// carry, and reports the operator-only ones it removed.
//
// Narrowing rather than checking is the point: the document a repository wrote never
// reaches a decoder that has a field for `providers` or for a token, so no amount of care
// about *when* to sanitize can be got wrong later.
func narrowToProjectSchema(data []byte) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", RepoConfigFile, err)
	}

	root := documentMapping(&document)
	if root == nil {
		// An empty document, or one whose root is not a mapping. The strict decode above
		// already accepted it, so there is simply nothing to layer.
		return nil, nil
	}

	kept := make([]*yaml.Node, 0, len(root.Content))
	for i := 0; i+1 < len(root.Content); i += mappingStride {
		key := root.Content[i].Value
		if _, allowed := projectLayerKeys[key]; allowed {
			kept = append(kept, root.Content[i], root.Content[i+1])
			continue
		}

		if reason, operatorOnly := operatorOnlyKeys[key]; operatorOnly {
			logger.Warnf("Ignoring %q from %s: %s", key, RepoConfigFile, reason)
		} else {
			logger.Debugf("Ignoring unknown key %q in %s", key, RepoConfigFile)
		}
	}

	narrowed := *root
	narrowed.Content = kept

	layer, err := yaml.Marshal(&narrowed)
	if err != nil {
		return nil, fmt.Errorf("failed to re-encode %s: %w", RepoConfigFile, err)
	}

	return layer, nil
}
