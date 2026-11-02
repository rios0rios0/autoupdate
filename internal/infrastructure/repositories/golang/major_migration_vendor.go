package golang

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/support"
)

const (
	// vendorDirName is the directory beside a go.mod that the module keeps its
	// vendored dependencies in.
	vendorDirName = "vendor"

	// vendorManifestName is the manifest `go mod vendor` writes into it, naming
	// every module that provides a package the module builds or tests.
	vendorManifestName = "modules.txt"

	// maxNamedVendorDirs is how many vendor directories a note names before it
	// only counts the rest.
	maxNamedVendorDirs = 2
)

// vendorDir is the vendor directory of one module in the worktree, and what its
// manifest lists.
type vendorDir struct {
	// prefix is the directory relative to the worktree, slash-terminated:
	// `vendor/` for the root module, `tools/vendor/` for one in tools.
	prefix string
	// modules are the module paths the manifest lists, each one vendored in
	// the directory of that name under prefix.
	modules []string
	// previous are the module paths the manifest listed before the migration
	// re-vendored it -- the base's, read at HEAD -- which is where the files
	// re-vendoring deleted came from.
	previous []string
}

// vendoredModules reads the vendor directory of every module in the worktree
// that has a readable manifest, as it is now and as the base had it. One
// without a manifest now is left out, so nothing in it is ever taken for a file
// the build does not read; one the base had no manifest for has no previous
// modules, which only ever makes the ownership of a file look more vendored.
func vendoredModules(ctx context.Context, worktree string) []vendorDir {
	var dirs []vendorDir
	for _, moduleDir := range discoverLocalModuleDirs(worktree) {
		prefix := path.Join(moduleDir, vendorDirName) + "/"

		manifest, err := os.ReadFile(filepath.Join(worktree, filepath.FromSlash(prefix+vendorManifestName)))
		if err != nil {
			continue
		}

		var previous []string
		if before, headErr := support.HeadFileContent(ctx, worktree, prefix+vendorManifestName); headErr == nil {
			previous = vendorManifestModules(string(before))
		}

		dirs = append(dirs, vendorDir{
			prefix:   prefix,
			modules:  vendorManifestModules(string(manifest)),
			previous: previous,
		})
	}

	return dirs
}

// vendorManifestModules returns the module paths a modules.txt lists: the first
// field of every `# <module> <version>` line. A replaced module is vendored
// under its own path too, so a `=> <replacement>` after it changes nothing.
func vendorManifestModules(manifest string) []string {
	var modules []string
	for line := range strings.SplitSeq(manifest, "\n") {
		entry, found := strings.CutPrefix(line, "# ")
		if !found {
			continue
		}
		if fields := strings.Fields(entry); len(fields) > 0 {
			modules = append(modules, fields[0])
		}
	}

	return modules
}

// deletionRefusal says why a file the migration deleted cannot stay on the
// branch, and nothing when it can: when it lies in a module's own vendor
// directory, in no module that directory still vendors.
//
// Such a file is inert, which is the whole case for keeping it. modules.txt
// lists every module that provides a package the module builds or tests, so a
// module it no longer lists is one nothing imports, and `./...` never matches
// inside vendor/. In a module that declares go 1.23 or later the go command
// will not even read such a package when something does import it: it says it
// is ignoring it, and fails the import exactly as it would without the files.
// The branch therefore builds exactly when the worktree does, and vet's answer
// stands for both.
//
// A file deleted from a module that is still vendored is the opposite case. A
// shared dependency the new major moves to a newer version can lose a file
// between the two, and left in place that file is compiled with the rest of its
// package: when the newer version merged it into another one, every
// declaration in it is redeclared. That deletion is refused, as is every one
// outside a module's vendor directory.
//
// Which module a file belongs to is the longest module path it lies under,
// across the manifest before the migration and after it. Any listed path
// that is a prefix is not enough: v1 of a module sits at the root of the tree
// every later major's suffixed path lies inside, so migrating `/v4` to `/v5`
// while something else still vendors v1 read every deleted `/v4` file as v1's,
// and refused a migration whose deletions were all inert.
func deletionRefusal(name string, vendored []vendorDir) string {
	// The module discovery skips anything below a vendor directory, so no
	// module's vendor directory lies inside another's, and at most one prefix
	// can match.
	for _, dir := range vendored {
		rel, found := strings.CutPrefix(name, dir.prefix)
		if !found {
			continue
		}
		if owner := owningVendoredModule(rel, dir.modules, dir.previous); slices.Contains(dir.modules, owner) {
			return fmt.Sprintf(
				"`%s` is deleted from `%s`, which is still vendored, and the provider API cannot push the deletion",
				name, owner,
			)
		}
		return ""
	}

	return deletedOrRenamedRefusal(name)
}

// owningVendoredModule returns the module a vendored file belongs to: the
// longest of the given module paths that the file's path, relative to the
// vendor directory, lies under. It is empty when none does.
func owningVendoredModule(rel string, manifests ...[]string) string {
	owner := ""
	for _, modules := range manifests {
		for _, module := range modules {
			if len(module) > len(owner) && strings.HasPrefix(rel, module+"/") {
				owner = module
			}
		}
	}

	return owner
}

// leftInPlaceNote tells the reader what the branch keeps that a clean re-vendor
// would not, why, and what removes it. It is short, because notes are cut at
// migrationNoteLimit, and names at most maxNamedVendorDirs directories.
func leftInPlaceNote(leftInPlace []string) string {
	if len(leftInPlace) == 0 {
		return ""
	}

	dirs := make([]string, 0, 1)
	for _, name := range leftInPlace {
		if dir := vendorDirOf(name); !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	slices.Sort(dirs)

	return "the old module's vendored files are still under " + namedVendorDirs(dirs) +
		": the provider API cannot delete files, and `go mod vendor` removes them"
}

// vendorDirOf returns the vendor directory a file left in place lies in: its
// path up to the first `vendor` segment. A module directory never has one --
// the module discovery skips them -- so that segment is the module's own.
func vendorDirOf(name string) string {
	segments := strings.Split(name, "/")
	for i, segment := range segments[:len(segments)-1] {
		if segment == vendorDirName {
			return strings.Join(segments[:i+1], "/") + "/"
		}
	}

	return path.Dir(name) + "/"
}

// namedVendorDirs names the vendor directories a note is about, counting the
// ones past maxNamedVendorDirs rather than naming them.
func namedVendorDirs(dirs []string) string {
	named := make([]string, 0, maxNamedVendorDirs)
	for _, dir := range dirs[:min(len(dirs), maxNamedVendorDirs)] {
		named = append(named, "`"+dir+"`")
	}

	switch len(dirs) {
	case 1:
		return named[0]
	case maxNamedVendorDirs:
		return named[0] + " and " + named[1]
	default:
		return strings.Join(named, ", ") + " and " + strconv.Itoa(len(dirs)-maxNamedVendorDirs) + " more"
	}
}
