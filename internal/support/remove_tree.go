package support

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// removableDirMode is what RemoveTree grants a directory's owner before its second
// attempt: enough to list the directory, unlink its entries and descend into it.
const removableDirMode = 0o700

// RemoveTree deletes path and everything beneath it, including the read-only
// trees package managers write.
//
// [os.RemoveAll] alone does not do that. Go marks every directory of its module
// cache read-only, and an entry cannot be unlinked from a directory its owner may
// not write to, so RemoveAll gives up at the first one and leaves the whole cache
// behind -- reporting it only through an error that cleanup code habitually
// discards. RemoveTree makes every directory under path writable by its owner and
// tries again, which is what `go clean -modcache` does. File modes need no such
// treatment: unlinking a file is decided by its directory on Unix, and
// [os.Remove] clears the read-only attribute itself on Windows.
//
// A path that does not exist is not an error.
func RemoveTree(path string) error {
	if err := os.RemoveAll(path); err == nil {
		return nil
	}

	makeDirectoriesWritable(path)

	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("failed to remove %s: %w", path, err)
	}
	return nil
}

// makeDirectoriesWritable grants the owner write access to path and to every
// directory beneath it.
//
// Every chmod goes through an [os.Root] opened on path's parent, so no symbolic
// link can carry it outside the tree: not a link at path itself, which is removed
// rather than followed, not one inside the tree, and not one swapped in for a
// directory while the walk is running, because the root refuses to resolve a name
// that leaves it. Failures are not reported here; the removal that follows says
// what is left.
func makeDirectoriesWritable(path string) {
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return
	}
	defer parent.Close()

	name := filepath.Base(path)
	if info, lstatErr := parent.Lstat(name); lstatErr != nil || !info.IsDir() {
		return
	}

	tree, err := parent.OpenRoot(name)
	if err != nil {
		return
	}
	defer tree.Close()

	// A directory is visited before it is read, so granting access here is what
	// lets the walk list it. An error is the walk reporting a directory it still
	// could not read; there is nothing more to do for it.
	_ = fs.WalkDir(tree.FS(), ".", func(entry string, d fs.DirEntry, walkErr error) error {
		if walkErr == nil && d.IsDir() {
			_ = tree.Chmod(entry, removableDirMode)
		}
		return nil
	})
}
