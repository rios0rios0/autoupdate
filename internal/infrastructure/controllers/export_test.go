package controllers

import "github.com/rios0rios0/autoupdate/internal/domain/entities"

// ResolveOperatorConfigPath exports resolveOperatorConfigPath for testing.
var ResolveOperatorConfigPath = resolveOperatorConfigPath //nolint:gochecknoglobals // test export

// ResolveWithFetch folds the configuration layers using the supplied fetch for the
// published-defaults layer, so a test can exercise the assembly -- including an unreachable
// network -- without one.
func ResolveWithFetch(
	configPath string, batch bool, fetch func(url string) ([]byte, error),
) (*entities.Settings, error) {
	return configLoader{fetch: fetch}.resolve(configPath, batch)
}

// LayerNamesWithFetch returns the names of the layers assembled for configPath, in override
// order.
func LayerNamesWithFetch(
	configPath string, fetch func(url string) ([]byte, error),
) ([]string, error) {
	layers, err := configLoader{fetch: fetch}.layers(configPath)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(layers))
	for _, layer := range layers {
		names = append(names, layer.Name)
	}
	return names, nil
}
