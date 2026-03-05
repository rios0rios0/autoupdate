//go:build integration || unit || test

package repositorydoubles //nolint:revive,staticcheck // Test package naming follows established project structure

import (
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	testkit "github.com/rios0rios0/testkit/pkg/test"
)

// SpyUpdaterRepositoryBuilder helps create test SpyUpdaterRepository instances with a fluent interface.
type SpyUpdaterRepositoryBuilder struct {
	*testkit.BaseBuilder
	updaterName  string
	detectResult bool
	prs          []entities.PullRequest
	createPRsErr error
}

// NewSpyUpdaterRepositoryBuilder creates a new spy updater repository builder with sensible defaults.
func NewSpyUpdaterRepositoryBuilder() *SpyUpdaterRepositoryBuilder {
	return &SpyUpdaterRepositoryBuilder{
		BaseBuilder:  testkit.NewBaseBuilder(),
		updaterName:  "terraform",
		detectResult: false,
	}
}

// WithUpdaterName sets the updater name.
func (b *SpyUpdaterRepositoryBuilder) WithUpdaterName(name string) *SpyUpdaterRepositoryBuilder {
	b.updaterName = name
	return b
}

// WithDetectResult sets the result to return from Detect.
func (b *SpyUpdaterRepositoryBuilder) WithDetectResult(result bool) *SpyUpdaterRepositoryBuilder {
	b.detectResult = result
	return b
}

// WithPRs sets the pull requests to return from CreateUpdatePRs.
func (b *SpyUpdaterRepositoryBuilder) WithPRs(prs []entities.PullRequest) *SpyUpdaterRepositoryBuilder {
	b.prs = prs
	return b
}

// WithCreatePRsErr sets the error to return from CreateUpdatePRs.
func (b *SpyUpdaterRepositoryBuilder) WithCreatePRsErr(err error) *SpyUpdaterRepositoryBuilder {
	b.createPRsErr = err
	return b
}

// Build creates the spy (satisfies testkit.Builder interface).
func (b *SpyUpdaterRepositoryBuilder) Build() interface{} {
	return b.BuildSpy()
}

// BuildSpy creates the SpyUpdaterRepository with a concrete return type.
func (b *SpyUpdaterRepositoryBuilder) BuildSpy() *SpyUpdaterRepository {
	return &SpyUpdaterRepository{
		UpdaterName:  b.updaterName,
		DetectResult: b.detectResult,
		PRs:          b.prs,
		CreatePRsErr: b.createPRsErr,
	}
}

// Reset clears the builder state, allowing it to be reused.
func (b *SpyUpdaterRepositoryBuilder) Reset() testkit.Builder {
	b.BaseBuilder.Reset()
	b.updaterName = "terraform"
	b.detectResult = false
	b.prs = nil
	b.createPRsErr = nil
	return b
}

// Clone creates a deep copy of the SpyUpdaterRepositoryBuilder.
func (b *SpyUpdaterRepositoryBuilder) Clone() testkit.Builder {
	clone := &SpyUpdaterRepositoryBuilder{
		BaseBuilder:  b.BaseBuilder.Clone().(*testkit.BaseBuilder),
		updaterName:  b.updaterName,
		detectResult: b.detectResult,
		createPRsErr: b.createPRsErr,
	}

	if b.prs != nil {
		clone.prs = make([]entities.PullRequest, len(b.prs))
		copy(clone.prs, b.prs)
	}

	return clone
}
