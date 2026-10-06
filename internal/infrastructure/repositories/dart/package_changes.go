package dart

import (
	"context"
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
)

const (
	pubspecLockFileName = "pubspec.lock"
	pubspecFileName     = "pubspec.yaml"

	// dartChangelogSummary is recorded when a run moved no direct dependency and
	// nothing else is pending: only packages the lock resolves for something
	// else changed, but a pull request with no changelog change while nothing is
	// pending fails the shared checks.
	dartChangelogSummary = "- changed the transitive Dart dependencies to their latest versions"

	// directDependencyPrefix opens the "dependency" value pub writes for a
	// package the pubspec names itself: "direct main", "direct dev",
	// "direct overridden".
	directDependencyPrefix = "direct"

	// sdkSource marks a package the Flutter SDK provides, whose locked version
	// is a placeholder rather than a release.
	sdkSource = "sdk"
)

// pubspecLock is the part of a pubspec.lock the reader needs.
type pubspecLock struct {
	Packages map[string]pubspecLockedPackage `yaml:"packages"`
}

// pubspecLockedPackage is one package a pubspec.lock resolves.
type pubspecLockedPackage struct {
	Dependency string `yaml:"dependency"`
	Source     string `yaml:"source"`
	Version    string `yaml:"version"`
}

// pubspecManifest is the part of a pubspec.yaml the reader needs. A dependency
// is a constraint string or a mapping (git, path, sdk); only the strings name a
// version.
type pubspecManifest struct {
	Dependencies    map[string]yaml.Node `yaml:"dependencies"`
	DevDependencies map[string]yaml.Node `yaml:"dev_dependencies"`
}

// observePackageChanges reports what an upgrade moved: the versions pubspec.lock
// resolves for the packages the pubspec names itself, the pubspec.yaml
// constraints of a package whose lock is not committed, and the Flutter SDK pin
// in .fvmrc.
func observePackageChanges(ctx context.Context, repoDir string) ([]entities.DependencyChange, error) {
	files, err := support.ReadModifiedFiles(ctx, repoDir, func(filePath string) bool {
		base := path.Base(filePath)
		return base == pubspecLockFileName || base == pubspecFileName || filePath == FvmConfigFile
	})
	if err != nil {
		return nil, err
	}

	lockedDirs := make(map[string]bool)
	for _, file := range files {
		if path.Base(file.Path) == pubspecLockFileName {
			lockedDirs[path.Dir(file.Path)] = true
		}
	}

	var changes []entities.DependencyChange
	for _, file := range files {
		fileChanges, diffErr := diffDartFile(file, lockedDirs)
		if diffErr != nil {
			return nil, diffErr
		}
		changes = append(changes, fileChanges...)
	}
	return changes, nil
}

// diffDartFile compares one manifest the run modified. A pubspec.yaml whose lock
// moved too is left to the lock, which holds the version actually resolved.
func diffDartFile(file support.ModifiedFile, lockedDirs map[string]bool) ([]entities.DependencyChange, error) {
	switch {
	case file.Path == FvmConfigFile:
		return support.PinChanges(file, entities.SubjectFlutterVersion, ParseFvmVersion), nil
	case path.Base(file.Path) == pubspecLockFileName:
		return diffManifests(file, lockedDirectVersions)
	case lockedDirs[path.Dir(file.Path)]:
		return nil, nil
	}
	return diffManifests(file, declaredConstraints)
}

// diffManifests compares two versions of one manifest through read.
func diffManifests(
	file support.ModifiedFile,
	read func(content []byte) (map[string]string, error),
) ([]entities.DependencyChange, error) {
	before, err := read(file.Before)
	if err != nil {
		return nil, fmt.Errorf("%s at HEAD: %w", file.Path, err)
	}
	after, err := read(file.After)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file.Path, err)
	}
	return support.DiffDeclaredVersions(entities.SubjectDartPackage, before, after), nil
}

// lockedDirectVersions maps every package a pubspec.lock resolves for the
// pubspec itself to its version, skipping the packages the SDK provides.
func lockedDirectVersions(content []byte) (map[string]string, error) {
	var lock pubspecLock
	if err := yaml.Unmarshal(content, &lock); err != nil {
		return nil, fmt.Errorf("failed to parse: %w", err)
	}

	versions := make(map[string]string, len(lock.Packages))
	for name, locked := range lock.Packages {
		if strings.HasPrefix(locked.Dependency, directDependencyPrefix) &&
			locked.Source != sdkSource && locked.Version != "" {
			versions[name] = locked.Version
		}
	}
	return versions, nil
}

// declaredConstraints maps every dependency a pubspec.yaml constrains by a
// version string to that constraint.
func declaredConstraints(content []byte) (map[string]string, error) {
	var manifest pubspecManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse: %w", err)
	}

	constraints := make(map[string]string)
	for _, section := range []map[string]yaml.Node{manifest.Dependencies, manifest.DevDependencies} {
		for name, node := range section {
			if node.Kind == yaml.ScalarNode && node.Value != "" {
				constraints[name] = node.Value
			}
		}
	}
	return constraints, nil
}
