//go:build integration || unit || test

package entitybuilders //nolint:revive,staticcheck // Test package naming follows established project structure

import (
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	testkit "github.com/rios0rios0/testkit/pkg/test"
)

// SettingsBuilder helps create test settings with a fluent interface.
type SettingsBuilder struct {
	*testkit.BaseBuilder
	providers []entities.ProviderConfig
	updaters  map[string]entities.UpdaterConfig
}

// NewSettingsBuilder creates a new settings builder with sensible defaults.
func NewSettingsBuilder() *SettingsBuilder {
	return &SettingsBuilder{
		BaseBuilder: testkit.NewBaseBuilder(),
		providers:   []entities.ProviderConfig{},
		updaters:    map[string]entities.UpdaterConfig{},
	}
}

// WithProviders sets the provider configurations.
func (b *SettingsBuilder) WithProviders(p []entities.ProviderConfig) *SettingsBuilder {
	b.providers = p
	return b
}

// WithUpdaters sets the updater configurations.
func (b *SettingsBuilder) WithUpdaters(u map[string]entities.UpdaterConfig) *SettingsBuilder {
	b.updaters = u
	return b
}

// Build creates the settings (satisfies testkit.Builder interface).
func (b *SettingsBuilder) Build() interface{} {
	return b.BuildSettings()
}

// BuildSettings creates the settings with a concrete return type.
func (b *SettingsBuilder) BuildSettings() *entities.Settings {
	return &entities.Settings{
		Providers: b.providers,
		Updaters:  b.updaters,
	}
}

// Reset clears the builder state, allowing it to be reused.
func (b *SettingsBuilder) Reset() testkit.Builder {
	b.BaseBuilder.Reset()
	b.providers = []entities.ProviderConfig{}
	b.updaters = map[string]entities.UpdaterConfig{}
	return b
}

// Clone creates a deep copy of the SettingsBuilder.
func (b *SettingsBuilder) Clone() testkit.Builder {
	providers := make([]entities.ProviderConfig, len(b.providers))
	copy(providers, b.providers)

	updaters := make(map[string]entities.UpdaterConfig, len(b.updaters))
	for k, v := range b.updaters {
		updaters[k] = v
	}

	return &SettingsBuilder{
		BaseBuilder: b.BaseBuilder.Clone().(*testkit.BaseBuilder),
		providers:   providers,
		updaters:    updaters,
	}
}
