package golang

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/support"
)

const (
	// goModFileName is the Go module manifest every module must declare.
	goModFileName = "go.mod"

	// rootModuleDir is the repo-relative directory of the root module.
	rootModuleDir = "."
)

// skippedModuleDirs are directory names that may contain a go.mod which is
// not a first-class module of the repository: vendored dependencies, Go test
// fixtures (which intentionally hold broken or pinned manifests), and
// JavaScript dependencies. Upgrading those would corrupt the tree.
var skippedModuleDirs = map[string]struct{}{ //nolint:gochecknoglobals // immutable lookup set
	"vendor":       {},
	"testdata":     {},
	"node_modules": {},
}

// moduleDirsFromPaths converts a list of repo-relative go.mod paths into the
// sorted, de-duplicated directories that contain them. The root module ("."),
// when present, always comes first so that it keeps driving the branch name
// and changelog wording; the remaining modules follow in lexicographic order
// to make runs deterministic.
func moduleDirsFromPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	dirs := make([]string, 0, len(paths))

	for _, p := range paths {
		normalized := path.Clean(strings.ReplaceAll(p, "\\", "/"))
		// Azure DevOps returns absolute item paths ("/go.mod") while GitHub and
		// GitLab return repo-relative ones; without this the root module would
		// be recorded as "/" and never recognised as the root.
		normalized = strings.TrimPrefix(normalized, "/")
		if path.Base(normalized) != goModFileName || isSkippedModulePath(normalized) {
			continue
		}

		dir := path.Dir(normalized)
		if _, duplicate := seen[dir]; duplicate {
			continue
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}

	sort.Strings(dirs)

	// Pin the root module first regardless of how the other directories sort.
	if idx := indexOf(dirs, rootModuleDir); idx > 0 {
		dirs = append([]string{rootModuleDir}, append(dirs[:idx:idx], dirs[idx+1:]...)...)
	}

	return dirs
}

// isSkippedModulePath reports whether any segment of the given go.mod path
// lives inside a vendored, fixture, or hidden directory.
func isSkippedModulePath(goModPath string) bool {
	segments := strings.SplitSeq(path.Dir(goModPath), "/")
	for segment := range segments {
		if segment == rootModuleDir || segment == "" {
			continue
		}
		if _, skipped := skippedModuleDirs[segment]; skipped {
			return true
		}
		if strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}

func indexOf(values []string, target string) int {
	for i, v := range values {
		if v == target {
			return i
		}
	}
	return -1
}

// goModPathFor returns the repo-relative go.mod path of a module directory.
func goModPathFor(dir string) string {
	if dir == "" || dir == rootModuleDir {
		return goModFileName
	}
	return path.Join(dir, goModFileName)
}

// resolveVersionUpgradeNeed reports whether *any* module in the repository
// declares a go directive other than targetVersion, the module directory the
// decision was based on, and whether a module could be read at all.
//
// The answer has to span every module because the upgrade runs in every
// module: deciding from the root alone would label a run "dependencies only"
// while the script bumps the go directive of a nested module, which then
// contradicts itself across the branch name, the changelog entry and the PR
// title. A stale root short-circuits the scan, so the common single-module
// case still costs one lookup.
func resolveVersionUpgradeNeed(
	read func(dir string) (string, error),
	moduleDirs func() []string,
	targetVersion string,
) (bool, string, bool) {
	rootContent, rootErr := read(rootModuleDir)
	if rootErr == nil && parseGoDirective(rootContent) != targetVersion {
		return true, rootModuleDir, true
	}

	sourceDir, found := rootModuleDir, rootErr == nil
	for _, dir := range moduleDirs() {
		if dir == rootModuleDir {
			continue // already read above
		}

		content, err := read(dir)
		if err != nil {
			continue
		}
		if !found {
			sourceDir, found = dir, true
		}
		if parseGoDirective(content) != targetVersion {
			return true, dir, true
		}
	}

	return false, sourceDir, found
}

// discoverLocalModuleDirs walks an on-disk repository and returns every
// directory holding a go.mod, relative to repoDir. A repository whose only
// module lives in a subdirectory (for example an isolated test-harness module
// inside an otherwise non-Go repository) yields that subdirectory alone.
func discoverLocalModuleDirs(repoDir string) []string {
	paths, err := support.WalkFilesByPredicate(repoDir, func(name string) bool {
		return name == goModFileName
	})
	if err != nil {
		// An unreadable subtree aborts the walk with whatever it collected so
		// far. Discarding that would strand modules that were found and are
		// perfectly upgradable, so the partial result is kept — matching the
		// upgrade script, which also tolerates unreadable subtrees.
		logger.Warnf(
			"[golang] Scan of %s ended early (%v); continuing with the %d path(s) found",
			repoDir, err, len(paths),
		)
	}

	return moduleDirsFromPaths(paths)
}

// localGoModReader returns a reader that resolves module directories against
// an on-disk repository root.
func localGoModReader(repoDir string) func(dir string) (string, error) {
	return func(dir string) (string, error) {
		data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(goModPathFor(dir))))
		if err != nil {
			return "", fmt.Errorf("failed to read %s: %w", goModPathFor(dir), err)
		}
		return string(data), nil
	}
}

// discoverRemoteModuleDirs lists the repository's go.mod files through the
// provider API, so that nested modules are known before anything is cloned.
func discoverRemoteModuleDirs(
	ctx context.Context,
	provider repositories.ProviderRepository,
	repo entities.Repository,
) []string {
	files, err := provider.ListFiles(ctx, repo, goModFileName)
	if err != nil {
		logger.Warnf(
			"[golang] Failed to list go.mod files for %s/%s: %v",
			repo.Organization, repo.Name, err,
		)
		return nil
	}

	paths := make([]string, 0, len(files))
	for _, f := range files {
		if f.IsDir {
			continue
		}
		paths = append(paths, f.Path)
	}

	return moduleDirsFromPaths(paths)
}
