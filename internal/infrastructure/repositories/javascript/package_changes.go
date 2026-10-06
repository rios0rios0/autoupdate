package javascript

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
)

const (
	packageJSONFileName = "package.json"
	nvmrcFileName       = ".nvmrc"
	nodeVersionFileName = ".node-version"

	// jsChangelogSummary is recorded when a run moved no package a package.json
	// names and nothing else is pending: only packages a lockfile resolves for
	// something else changed, but a pull request with no changelog change while
	// nothing is pending fails the shared checks.
	jsChangelogSummary = "- changed the transitive JavaScript dependencies to their latest versions"

	// vendoredPackagesSegment marks a dependency's own manifest, never one the
	// repository declares.
	vendoredPackagesSegment = "node_modules/"
)

// lockfilePriority lists the lockfiles in the order detectLocalPackageManager
// picks the package manager from them.
//
//nolint:gochecknoglobals // read-only lookup table
var lockfilePriority = []string{pnpmLockFileName, yarnLockFileName, packageLockFileName}

// npmManifest is the part of a package.json the reader needs.
type npmManifest struct {
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
}

// manifestPair is one package.json as HEAD and the working tree declare it,
// reduced to the ranges of its registry dependencies.
type manifestPair struct {
	dir           string
	before, after map[string]string
}

// observePackageChanges reports what an upgrade moved: the version the lockfile
// resolves for every package a package.json declares -- the root one, and any
// workspace manifest the run rewrote -- and the Node.js version pin. Without a
// lockfile that moved, the declared ranges themselves are compared.
func observePackageChanges(ctx context.Context, repoDir string) ([]entities.DependencyChange, error) {
	files, err := support.ReadModifiedFiles(ctx, repoDir, isJavaScriptManifest)
	if err != nil {
		return nil, err
	}

	var changes []entities.DependencyChange
	for _, file := range files {
		if file.Path == nvmrcFileName || file.Path == nodeVersionFileName {
			changes = append(changes, support.PinChanges(file, entities.SubjectNodeVersion, parseNodeVersionFile)...)
		}
	}

	manifests, err := readManifests(repoDir, files)
	if err != nil {
		return nil, err
	}

	lock, hasLock := modifiedLockfile(files)
	for _, manifest := range manifests {
		manifestChanges, diffErr := diffManifest(manifest, lock, hasLock)
		if diffErr != nil {
			return nil, diffErr
		}
		changes = append(changes, manifestChanges...)
	}
	return changes, nil
}

// diffManifest compares what one package.json's dependencies resolve to.
func diffManifest(
	manifest manifestPair,
	lock support.ModifiedFile,
	hasLock bool,
) ([]entities.DependencyChange, error) {
	if !hasLock {
		return support.DiffDeclaredVersions(
			entities.SubjectJavaScriptPackage, bareVersions(manifest.before), bareVersions(manifest.after),
		), nil
	}

	resolve := lockfileResolvers[lock.Path]
	before, err := resolve(lock.Before, manifest.dir, manifest.before)
	if err != nil {
		return nil, fmt.Errorf("%s at HEAD: %w", lock.Path, err)
	}
	after, err := resolve(lock.After, manifest.dir, manifest.after)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", lock.Path, err)
	}
	return support.DiffDeclaredVersions(entities.SubjectJavaScriptPackage, before, after), nil
}

// isJavaScriptManifest reports whether a modified path is a file the reader
// compares: any package.json the repository declares, and the root lockfiles
// and Node.js pins.
func isJavaScriptManifest(filePath string) bool {
	if path.Base(filePath) == packageJSONFileName {
		return !strings.Contains(filePath, vendoredPackagesSegment)
	}
	switch filePath {
	case nvmrcFileName, nodeVersionFileName:
		return true
	}
	_, isLockfile := lockfileResolvers[filePath]
	return isLockfile
}

// readManifests returns the package.json files whose dependencies are
// compared: every one the run modified, and the root one even when only the
// lockfile moved, read from the working tree for both sides.
func readManifests(repoDir string, files []support.ModifiedFile) ([]manifestPair, error) {
	var manifests []manifestPair
	hasRoot := false
	for _, file := range files {
		if path.Base(file.Path) != packageJSONFileName {
			continue
		}
		pair, err := newManifestPair(path.Dir(file.Path), file.Before, file.After)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, pair)
		hasRoot = hasRoot || pair.dir == "."
	}

	if hasRoot {
		return manifests, nil
	}

	// The root package.json came from git's listing of the repository root.
	content, err := os.ReadFile(support.WorkingFilePath(repoDir, packageJSONFileName))
	if errors.Is(err, os.ErrNotExist) {
		return manifests, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", packageJSONFileName, err)
	}

	root, err := newManifestPair(".", content, content)
	if err != nil {
		return nil, err
	}
	return append(manifests, root), nil
}

// newManifestPair reads both sides of one package.json.
func newManifestPair(dir string, before, after []byte) (manifestPair, error) {
	beforeRanges, err := declaredRanges(before)
	if err != nil {
		return manifestPair{}, fmt.Errorf("%s at HEAD: %w", path.Join(dir, packageJSONFileName), err)
	}
	afterRanges, err := declaredRanges(after)
	if err != nil {
		return manifestPair{}, fmt.Errorf("%s: %w", path.Join(dir, packageJSONFileName), err)
	}
	return manifestPair{dir: dir, before: beforeRanges, after: afterRanges}, nil
}

// declaredRanges maps every registry dependency a package.json declares to its
// version range. A range naming a protocol, a path or a repository --
// `workspace:`, `link:`, `file:`, `github:user/repo`, a git URL -- names no
// registry release and is left out.
func declaredRanges(content []byte) (map[string]string, error) {
	var manifest npmManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse: %w", err)
	}

	ranges := make(map[string]string)
	for _, section := range []map[string]string{
		manifest.Dependencies, manifest.DevDependencies, manifest.OptionalDependencies,
	} {
		for name, versionRange := range section {
			if !strings.ContainsAny(versionRange, ":/") {
				ranges[name] = versionRange
			}
		}
	}
	return ranges, nil
}

// modifiedLockfile returns the lockfile the run modified, in the order the
// package manager is picked in.
func modifiedLockfile(files []support.ModifiedFile) (support.ModifiedFile, bool) {
	for _, name := range lockfilePriority {
		for _, file := range files {
			if file.Path == name {
				return file, true
			}
		}
	}
	return support.ModifiedFile{}, false
}

// bareVersions strips the operator a simple range starts with, so a range a
// run raised reads as the version it now starts from: "^1.2.0" names 1.2.0.
func bareVersions(ranges map[string]string) map[string]string {
	versions := make(map[string]string, len(ranges))
	for name, versionRange := range ranges {
		versions[name] = strings.TrimLeft(strings.TrimSpace(versionRange), "^~=v")
	}
	return versions
}
