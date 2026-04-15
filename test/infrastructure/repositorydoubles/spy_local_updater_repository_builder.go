//go:build integration || unit || test

package repositorydoubles //nolint:revive,staticcheck // Test package naming follows established project structure

import (
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	testkit "github.com/rios0rios0/testkit/pkg/test"
)

// SpyLocalUpdaterRepositoryBuilder helps create test SpyLocalUpdaterRepository
// instances with a fluent interface.
type SpyLocalUpdaterRepositoryBuilder struct {
	*testkit.BaseBuilder
	updaterName   string
	detectResult  bool
	applyUpdateFn func(repoDir string) (*repositories.LocalUpdateResult, error)
}

// NewSpyLocalUpdaterRepositoryBuilder creates a new spy local updater
// repository builder with sensible defaults.
func NewSpyLocalUpdaterRepositoryBuilder() *SpyLocalUpdaterRepositoryBuilder {
	return &SpyLocalUpdaterRepositoryBuilder{
		BaseBuilder:  testkit.NewBaseBuilder(),
		updaterName:  "spy-local",
		detectResult: false,
	}
}

// WithUpdaterName sets the updater name.
func (b *SpyLocalUpdaterRepositoryBuilder) WithUpdaterName(name string) *SpyLocalUpdaterRepositoryBuilder {
	b.updaterName = name
	return b
}

// WithDetectResult sets the result to return from Detect.
func (b *SpyLocalUpdaterRepositoryBuilder) WithDetectResult(result bool) *SpyLocalUpdaterRepositoryBuilder {
	b.detectResult = result
	return b
}

// WithApplyUpdateFn sets the function invoked from ApplyUpdates. Tests use
// this to mutate the worktree and return a custom LocalUpdateResult.
func (b *SpyLocalUpdaterRepositoryBuilder) WithApplyUpdateFn(
	fn func(repoDir string) (*repositories.LocalUpdateResult, error),
) *SpyLocalUpdaterRepositoryBuilder {
	b.applyUpdateFn = fn
	return b
}

// Build creates the spy (satisfies testkit.Builder interface).
func (b *SpyLocalUpdaterRepositoryBuilder) Build() interface{} {
	return b.BuildSpy()
}

// BuildSpy creates the SpyLocalUpdaterRepository with a concrete return type.
func (b *SpyLocalUpdaterRepositoryBuilder) BuildSpy() *SpyLocalUpdaterRepository {
	return &SpyLocalUpdaterRepository{
		UpdaterName:   b.updaterName,
		DetectResult:  b.detectResult,
		ApplyUpdateFn: b.applyUpdateFn,
	}
}

// Reset clears the builder state, allowing it to be reused.
func (b *SpyLocalUpdaterRepositoryBuilder) Reset() testkit.Builder {
	b.BaseBuilder.Reset()
	b.updaterName = "spy-local"
	b.detectResult = false
	b.applyUpdateFn = nil
	return b
}

// Clone creates a deep copy of the SpyLocalUpdaterRepositoryBuilder.
func (b *SpyLocalUpdaterRepositoryBuilder) Clone() testkit.Builder {
	return &SpyLocalUpdaterRepositoryBuilder{
		BaseBuilder:   b.BaseBuilder.Clone().(*testkit.BaseBuilder),
		updaterName:   b.updaterName,
		detectResult:  b.detectResult,
		applyUpdateFn: b.applyUpdateFn,
	}
}
