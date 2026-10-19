package support

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	logger "github.com/sirupsen/logrus"
)

// SharedCachePattern names the directory a batch run shares between the
// repositories it processes, as an [os.MkdirTemp] pattern. The stale-directory
// sweep matches the same name, so the cache of a run that was killed is removed by
// the next one.
const SharedCachePattern = "autoupdate-cache-*"

const (
	gibibyte = 1 << 30
	// minFreeSpace is how much of its filesystem the shared cache leaves free, at
	// most: Trim empties the cache when less than this is left.
	minFreeSpace = 10 * gibibyte
	// minFreeSpaceDivisor scales minFreeSpace down on a filesystem smaller than
	// ten times it, where a tenth of the filesystem is left free instead.
	minFreeSpaceDivisor = 10
)

// SharedCache is the one directory a batch run shares between the repositories it
// processes, holding the caches toolingRedirects marks shared: the Go module cache,
// which also holds every toolchain GOTOOLCHAIN downloads, and the Go build cache.
// Both are built for several go commands at once, and they are what the Go
// repositories of an organization have most in common.
//
// Each repository's own workspace starts empty, so with these caches in it every Go
// repository downloaded its whole module graph and toolchain again, and compiled
// every dependency from scratch for the compile guard -- up to three times a module.
// Shared, the first repository to need a module or a compiled package pays for it.
//
// What sharing gives up is the bound a workspace puts on the disk, so the cache
// bounds itself: Trim empties it when the filesystem it lives on runs low, once no
// script holds it through Use, and the run removes it when it ends. A run that
// cannot read how much space is left does not share at all.
//
// A nil *SharedCache is valid and shares nothing: Dir is empty, so ScriptEnv keeps
// every cache in the repository's workspace, and Use, Trim and Remove do nothing.
type SharedCache struct {
	root string
	// inUse is held for reading by every script using the cache, and for writing
	// by the Trim that empties it.
	inUse sync.RWMutex
	// space reports the free and total bytes of the filesystem holding a path.
	space func(path string) (free, total uint64, err error)
}

// NewSharedCache creates a shared cache under the system temporary directory,
// which TMPDIR relocates.
func NewSharedCache() (*SharedCache, error) {
	return newSharedCache(DiskSpace)
}

func newSharedCache(space func(path string) (free, total uint64, err error)) (*SharedCache, error) {
	root, err := os.MkdirTemp("", SharedCachePattern)
	if err != nil {
		return nil, fmt.Errorf("failed to create the shared cache: %w", err)
	}

	// Sharing is only safe while the disk can be watched: an unbounded cache is
	// exactly what filled the disk before every repository had a workspace.
	if _, _, err = space(root); err != nil {
		_ = RemoveTree(root)
		return nil, fmt.Errorf("failed to read the free space for the shared cache: %w", err)
	}
	return &SharedCache{root: root, space: space}, nil
}

// Dir returns the directory the shared caches live in, or "" for a nil cache.
func (c *SharedCache) Dir() string {
	if c == nil {
		return ""
	}
	return c.root
}

// Use marks the cache as in use until the returned function is called. Trim waits
// for every use to end before it empties the cache, and a use that starts while a
// Trim is waiting waits for the Trim.
func (c *SharedCache) Use() func() {
	if c == nil {
		return func() {}
	}
	c.inUse.RLock()
	return c.inUse.RUnlock
}

// Trim empties the cache when the filesystem it lives on has less free space than
// it should leave (see freeSpaceFloor), and reports whether it did. It must not be
// called while holding a Use.
//
// Emptying costs the repositories after it a fresh download and compile, which is
// the price the run used to pay on every repository; running out of disk costs the
// run. Another repository may have emptied the cache while this one waited, so the
// space is read again once no script is using it.
func (c *SharedCache) Trim() bool {
	if c == nil || !c.lowOnSpace() {
		return false
	}

	c.inUse.Lock()
	defer c.inUse.Unlock()
	if !c.lowOnSpace() || c.empty() {
		return false
	}

	if err := c.clear(); err != nil {
		logger.Warnf("[autoupdate] Could not empty the shared cache %s: %v", c.root, err)
		return false
	}
	free, total, _ := c.space(c.root)
	logger.Infof("[autoupdate] Emptied the shared Go caches to keep the disk from filling up (%s free of %s)",
		gibibytes(free), gibibytes(total))
	return true
}

// Remove deletes the cache and everything in it, once no script is using it.
// Calling it again is harmless.
func (c *SharedCache) Remove() error {
	if c == nil {
		return nil
	}
	c.inUse.Lock()
	defer c.inUse.Unlock()
	return RemoveTree(c.root)
}

// lowOnSpace reports whether the filesystem holding the cache has less free space
// than freeSpaceFloor asks for. Space that cannot be read is not reported as low:
// the cache only got here by reading it once, and a transient failure is no reason
// to throw away what the run has downloaded.
func (c *SharedCache) lowOnSpace() bool {
	free, total, err := c.space(c.root)
	if err != nil {
		logger.Debugf("[autoupdate] Could not read the free space around the shared cache: %v", err)
		return false
	}
	return free < freeSpaceFloor(total)
}

// freeSpaceFloor is how much of a filesystem of total bytes the shared cache leaves
// free: minFreeSpace, or a tenth of a filesystem too small for that to be one.
func freeSpaceFloor(total uint64) uint64 {
	return min(minFreeSpace, total/minFreeSpaceDivisor)
}

// empty reports whether the cache holds nothing, so there is nothing to give back.
func (c *SharedCache) empty() bool {
	entries, err := os.ReadDir(c.root)
	return err == nil && len(entries) == 0
}

// clear removes everything in the cache and keeps the directory itself, which the
// go commands of the repositories still to come recreate their caches in.
func (c *SharedCache) clear() error {
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return fmt.Errorf("failed to list the shared cache: %w", err)
	}

	var errs []error
	for _, entry := range entries {
		errs = append(errs, RemoveTree(filepath.Join(c.root, entry.Name())))
	}
	return errors.Join(errs...)
}

// gibibytes renders a byte count of a filesystem in the unit disks are read in.
func gibibytes(bytes uint64) string {
	return fmt.Sprintf("%.1f GiB", float64(bytes)/gibibyte)
}
