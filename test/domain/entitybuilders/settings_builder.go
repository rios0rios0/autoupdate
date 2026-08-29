package entitybuilders

import (
	"maps"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	testkit "github.com/rios0rios0/testkit/pkg/test"
)

// SettingsBuilder helps create test settings with a fluent interface.
type SettingsBuilder struct {
	*testkit.BaseBuilder

	providers       []entities.ProviderConfig
	updaters        map[string]entities.UpdaterConfig
	excludeForks    bool
	excludeArchived bool
	excludeRepos    []string

	cleanupStaleBranches  *bool
	aggregateBranchPrefix string
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

// WithExcludeForks sets the exclude forks flag.
func (b *SettingsBuilder) WithExcludeForks(exclude bool) *SettingsBuilder {
	b.excludeForks = exclude
	return b
}

// WithExcludeArchived sets the exclude archived flag.
func (b *SettingsBuilder) WithExcludeArchived(exclude bool) *SettingsBuilder {
	b.excludeArchived = exclude
	return b
}

// WithExcludeRepos sets the global repository exclusion patterns.
func (b *SettingsBuilder) WithExcludeRepos(patterns []string) *SettingsBuilder {
	b.excludeRepos = patterns
	return b
}

// WithCleanupStaleBranches sets the stale aggregate-branch cleanup toggle.
func (b *SettingsBuilder) WithCleanupStaleBranches(cleanup bool) *SettingsBuilder {
	b.cleanupStaleBranches = &cleanup
	return b
}

// WithAggregateBranchPrefix sets the aggregate branch prefix.
func (b *SettingsBuilder) WithAggregateBranchPrefix(prefix string) *SettingsBuilder {
	b.aggregateBranchPrefix = prefix
	return b
}

// Build creates the settings (satisfies testkit.Builder interface).
func (b *SettingsBuilder) Build() any {
	return b.BuildSettings()
}

// BuildSettings creates the settings with a concrete return type.
func (b *SettingsBuilder) BuildSettings() *entities.Settings {
	return &entities.Settings{
		Providers:       b.providers,
		Updaters:        b.updaters,
		ExcludeForks:    b.excludeForks,
		ExcludeArchived: b.excludeArchived,
		ExcludeRepos:    b.excludeRepos,

		CleanupStaleBranches:  b.cleanupStaleBranches,
		AggregateBranchPrefix: b.aggregateBranchPrefix,
	}
}

// Reset clears the builder state, allowing it to be reused.
func (b *SettingsBuilder) Reset() testkit.Builder {
	b.BaseBuilder.Reset()
	b.providers = []entities.ProviderConfig{}
	b.updaters = map[string]entities.UpdaterConfig{}
	b.excludeForks = false
	b.excludeArchived = false
	b.excludeRepos = nil
	b.cleanupStaleBranches = nil
	b.aggregateBranchPrefix = ""
	return b
}

// Clone creates a deep copy of the SettingsBuilder.
func (b *SettingsBuilder) Clone() testkit.Builder {
	providers := make([]entities.ProviderConfig, len(b.providers))
	copy(providers, b.providers)

	updaters := make(map[string]entities.UpdaterConfig, len(b.updaters))
	maps.Copy(updaters, b.updaters)

	excludeRepos := make([]string, len(b.excludeRepos))
	copy(excludeRepos, b.excludeRepos)

	// The clone gets its own bool so the two builders never share the pointer:
	// a deep copy that hands out the same address is not a deep copy.
	var cleanupStaleBranches *bool
	if b.cleanupStaleBranches != nil {
		cleanup := *b.cleanupStaleBranches
		cleanupStaleBranches = &cleanup
	}

	return &SettingsBuilder{
		BaseBuilder:     cloneBase(b.BaseBuilder),
		providers:       providers,
		updaters:        updaters,
		excludeForks:    b.excludeForks,
		excludeArchived: b.excludeArchived,
		excludeRepos:    excludeRepos,

		cleanupStaleBranches:  cleanupStaleBranches,
		aggregateBranchPrefix: b.aggregateBranchPrefix,
	}
}
