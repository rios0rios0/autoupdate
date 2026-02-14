//go:build integration || unit || test

package entitybuilders //nolint:revive,staticcheck // Test package naming follows established project structure

import (
	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	testkit "github.com/rios0rios0/testkit/pkg/test"
)

// DependencyBuilder helps create test dependencies with a fluent interface.
type DependencyBuilder struct {
	*testkit.BaseBuilder
	name       string
	source     string
	currentVer string
	latestVer  string
	filePath   string
	line       int
}

// NewDependencyBuilder creates a new dependency builder with sensible defaults.
func NewDependencyBuilder() *DependencyBuilder {
	return &DependencyBuilder{
		BaseBuilder: testkit.NewBaseBuilder(),
		name:        "test-dependency",
		source:      "github.com/test/dep",
		currentVer:  "1.0.0",
		latestVer:   "2.0.0",
		filePath:    "go.mod",
		line:        1,
	}
}

// WithName sets the dependency name.
func (b *DependencyBuilder) WithName(name string) *DependencyBuilder {
	b.name = name
	return b
}

// WithSource sets the source URL/path.
func (b *DependencyBuilder) WithSource(source string) *DependencyBuilder {
	b.source = source
	return b
}

// WithCurrentVer sets the current version.
func (b *DependencyBuilder) WithCurrentVer(version string) *DependencyBuilder {
	b.currentVer = version
	return b
}

// WithLatestVer sets the latest version.
func (b *DependencyBuilder) WithLatestVer(version string) *DependencyBuilder {
	b.latestVer = version
	return b
}

// WithFilePath sets the file path.
func (b *DependencyBuilder) WithFilePath(path string) *DependencyBuilder {
	b.filePath = path
	return b
}

// WithLine sets the line number.
func (b *DependencyBuilder) WithLine(line int) *DependencyBuilder {
	b.line = line
	return b
}

// Build creates the dependency (satisfies testkit.Builder interface).
func (b *DependencyBuilder) Build() interface{} {
	return b.BuildDependency()
}

// BuildDependency creates the dependency with a concrete return type.
func (b *DependencyBuilder) BuildDependency() entities.Dependency {
	return entities.Dependency{
		Name:       b.name,
		Source:     b.source,
		CurrentVer: b.currentVer,
		LatestVer:  b.latestVer,
		FilePath:   b.filePath,
		Line:       b.line,
	}
}

// Reset clears the builder state, allowing it to be reused.
func (b *DependencyBuilder) Reset() testkit.Builder {
	b.BaseBuilder.Reset()
	b.name = "test-dependency"
	b.source = "github.com/test/dep"
	b.currentVer = "1.0.0"
	b.latestVer = "2.0.0"
	b.filePath = "go.mod"
	b.line = 1
	return b
}

// Clone creates a deep copy of the DependencyBuilder.
func (b *DependencyBuilder) Clone() testkit.Builder {
	return &DependencyBuilder{
		BaseBuilder: b.BaseBuilder.Clone().(*testkit.BaseBuilder),
		name:        b.name,
		source:      b.source,
		currentVer:  b.currentVer,
		latestVer:   b.latestVer,
		filePath:    b.filePath,
		line:        b.line,
	}
}
