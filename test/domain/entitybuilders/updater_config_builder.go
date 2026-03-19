//go:build integration || unit || test

package entitybuilders //nolint:revive,staticcheck // Test package naming follows established project structure

import (
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	testkit "github.com/rios0rios0/testkit/pkg/test"
)

// UpdaterConfigBuilder helps create test updater configurations with a fluent interface.
type UpdaterConfigBuilder struct {
	*testkit.BaseBuilder
	enabled      *bool
	autoComplete bool
	targetBranch string
}

// NewUpdaterConfigBuilder creates a new updater config builder with sensible defaults.
func NewUpdaterConfigBuilder() *UpdaterConfigBuilder {
	return &UpdaterConfigBuilder{
		BaseBuilder:  testkit.NewBaseBuilder(),
		enabled:      nil,
		autoComplete: false,
		targetBranch: "",
	}
}

// WithEnabled sets the enabled flag.
func (b *UpdaterConfigBuilder) WithEnabled(enabled bool) *UpdaterConfigBuilder {
	b.enabled = &enabled
	return b
}

// WithAutoComplete sets the auto-complete flag.
func (b *UpdaterConfigBuilder) WithAutoComplete(autoComplete bool) *UpdaterConfigBuilder {
	b.autoComplete = autoComplete
	return b
}

// WithTargetBranch sets the target branch.
func (b *UpdaterConfigBuilder) WithTargetBranch(branch string) *UpdaterConfigBuilder {
	b.targetBranch = branch
	return b
}

// Build creates the updater config (satisfies testkit.Builder interface).
func (b *UpdaterConfigBuilder) Build() interface{} {
	return b.BuildUpdaterConfig()
}

// BuildUpdaterConfig creates the updater config with a concrete return type.
func (b *UpdaterConfigBuilder) BuildUpdaterConfig() entities.UpdaterConfig {
	return entities.UpdaterConfig{
		Enabled:      b.enabled,
		AutoComplete: b.autoComplete,
		TargetBranch: b.targetBranch,
	}
}

// Reset clears the builder state, allowing it to be reused.
func (b *UpdaterConfigBuilder) Reset() testkit.Builder {
	b.BaseBuilder.Reset()
	b.enabled = nil
	b.autoComplete = false
	b.targetBranch = ""
	return b
}

// Clone creates a deep copy of the UpdaterConfigBuilder.
func (b *UpdaterConfigBuilder) Clone() testkit.Builder {
	var clonedEnabled *bool
	if b.enabled != nil {
		v := *b.enabled
		clonedEnabled = &v
	}
	return &UpdaterConfigBuilder{
		BaseBuilder:  b.BaseBuilder.Clone().(*testkit.BaseBuilder),
		enabled:      clonedEnabled,
		autoComplete: b.autoComplete,
		targetBranch: b.targetBranch,
	}
}
