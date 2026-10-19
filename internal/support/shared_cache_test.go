package support_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/support"
)

const gibibyte = 1 << 30

// diskStub reports the same free and total space for every path, the way a
// filesystem does between two writes. Tests set it up before the cache reads it.
type diskStub struct {
	free  uint64
	total uint64
	err   error
}

func (d *diskStub) space(string) (uint64, uint64, error) {
	return d.free, d.total, d.err
}

// newSharedCache creates a shared cache reading its free space from disk, removed
// when the test ends whatever the test did to it.
func newSharedCache(t *testing.T, disk *diskStub) *support.SharedCache {
	t.Helper()

	cache, err := support.NewSharedCacheWithSpace(disk.space)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cache.Remove() })
	return cache
}

func TestNewSharedCache(t *testing.T) {
	t.Parallel()

	t.Run("should create the cache in the temporary directory under the name the sweep matches", func(t *testing.T) {
		t.Parallel()

		// given / when
		cache := newSharedCache(t, &diskStub{free: 50 * gibibyte, total: 100 * gibibyte})

		// then
		//nolint:usetesting // the system temporary directory is what is being tested
		assert.Equal(t, filepath.Clean(os.TempDir()), filepath.Dir(cache.Dir()))
		assert.True(t, strings.HasPrefix(filepath.Base(cache.Dir()), "autoupdate-cache-"),
			"the stale-directory sweep matches the cache by this prefix, got %s", cache.Dir())
		assert.DirExists(t, cache.Dir())
	})

	t.Run("should share only where the platform can read the free space of its disk", func(t *testing.T) {
		t.Parallel()

		// given / when
		cache, err := support.NewSharedCache()

		// then the platforms autoupdate is released for share, and any other falls back
		// to keeping the Go caches in each repository's workspace
		if slices.Contains([]string{"linux", "darwin", "windows"}, runtime.GOOS) {
			require.NoError(t, err)
			assert.NoError(t, cache.Remove())
		} else {
			require.Error(t, err)
			assert.Nil(t, cache)
		}
	})

	t.Run("should keep its modification time fresh while it lives", func(t *testing.T) {
		t.Parallel()

		// given a cache whose own modification time is two hours old, which is how a busy
		// cache looks when every write lands deep inside it
		if !slices.Contains([]string{"linux", "darwin", "windows"}, runtime.GOOS) {
			t.Skip("no shared cache on a platform that cannot read its free space")
		}
		cache, err := support.NewSharedCacheWithHeartbeat(10 * time.Millisecond)
		require.NoError(t, err)
		t.Cleanup(func() { _ = cache.Remove() })
		past := time.Now().Add(-2 * time.Hour)
		require.NoError(t, os.Chtimes(cache.Dir(), past, past))

		// when / then the heartbeat renews it, so no sweep takes it for abandoned
		assert.Eventually(t, func() bool {
			info, statErr := os.Stat(cache.Dir())
			return statErr == nil && time.Since(info.ModTime()) < time.Minute
		}, 5*time.Second, 10*time.Millisecond)
	})
}

// TestNewSharedCacheWithoutDiskSpace is not parallel: it points TMPDIR at a
// directory of its own with t.Setenv, so that the cache it refuses to create can be
// shown not to have been left behind.
func TestNewSharedCacheWithoutDiskSpace(t *testing.T) {
	t.Run("should refuse to share when the free space of the disk cannot be read", func(t *testing.T) {
		// given
		tmpDir := t.TempDir()
		t.Setenv("TMPDIR", tmpDir)
		disk := &diskStub{err: errors.New("statfs: operation not supported")}

		// when
		cache, err := support.NewSharedCacheWithSpace(disk.space)

		// then
		require.Error(t, err)
		assert.Nil(t, cache)
		entries, readErr := os.ReadDir(tmpDir)
		require.NoError(t, readErr)
		assert.Empty(t, entries, "the refused cache should not be left behind")
	})
}

func TestSharedCacheNil(t *testing.T) {
	t.Parallel()

	t.Run("should share nothing when the run has no shared cache", func(t *testing.T) {
		t.Parallel()

		// given
		var cache *support.SharedCache

		// when
		release := cache.Use()
		release()
		trimmed := cache.Trim()
		removeErr := cache.Remove()

		// then
		assert.Empty(t, cache.Dir(), "ScriptEnv keeps every cache in the workspace when there is no shared directory")
		assert.False(t, trimmed)
		assert.NoError(t, removeErr)
	})
}

func TestSharedCacheTrim(t *testing.T) {
	t.Parallel()

	t.Run("should empty the cache when the disk runs low", func(t *testing.T) {
		t.Parallel()

		// given a module cache as Go leaves it, on a disk with a gibibyte left of a hundred
		cache := newSharedCache(t, &diskStub{free: 1 * gibibyte, total: 100 * gibibyte})
		writeReadOnlyModuleCache(t, cache.Dir())

		// when
		trimmed := cache.Trim()

		// then the cache is empty and still there for the repositories to come
		assert.True(t, trimmed)
		assert.DirExists(t, cache.Dir())
		entries, err := os.ReadDir(cache.Dir())
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("should leave the cache alone while the disk has room", func(t *testing.T) {
		t.Parallel()

		// given
		cache := newSharedCache(t, &diskStub{free: 50 * gibibyte, total: 100 * gibibyte})
		writeReadOnlyModuleCache(t, cache.Dir())

		// when
		trimmed := cache.Trim()

		// then
		assert.False(t, trimmed)
		assert.FileExists(t, filepath.Join(cache.Dir(), "go", "mod", "example.com", "dependency@v1.0.0", "go.mod"))
	})

	t.Run("should leave a tenth of a disk too small to leave ten gibibytes free", func(t *testing.T) {
		t.Parallel()

		// given a 20 GiB disk, whose floor is 2 GiB, with 3 GiB and with 1 GiB left
		roomy := newSharedCache(t, &diskStub{free: 3 * gibibyte, total: 20 * gibibyte})
		writeReadOnlyModuleCache(t, roomy.Dir())
		low := newSharedCache(t, &diskStub{free: 1 * gibibyte, total: 20 * gibibyte})
		writeReadOnlyModuleCache(t, low.Dir())

		// when / then
		assert.False(t, roomy.Trim(), "3 GiB free is above a tenth of 20 GiB")
		assert.True(t, low.Trim(), "1 GiB free is below a tenth of 20 GiB")
	})

	t.Run("should keep a cache with nothing in it as it is", func(t *testing.T) {
		t.Parallel()

		// given
		cache := newSharedCache(t, &diskStub{free: 1 * gibibyte, total: 100 * gibibyte})

		// when
		trimmed := cache.Trim()

		// then there was nothing to give back
		assert.False(t, trimmed)
		assert.DirExists(t, cache.Dir())
	})

	t.Run("should keep the cache when the free space cannot be read any more", func(t *testing.T) {
		t.Parallel()

		// given a cache whose disk stops answering after it was created
		disk := &diskStub{free: 1 * gibibyte, total: 100 * gibibyte}
		cache := newSharedCache(t, disk)
		writeReadOnlyModuleCache(t, cache.Dir())
		disk.err = errors.New("statfs: input/output error")

		// when
		trimmed := cache.Trim()

		// then
		assert.False(t, trimmed)
		assert.FileExists(t, filepath.Join(cache.Dir(), "go", "mod", "example.com", "dependency@v1.0.0", "go.mod"))
	})

	t.Run("should wait for every script using the cache before emptying it", func(t *testing.T) {
		t.Parallel()

		// given a script holding the cache while the disk runs low
		cache := newSharedCache(t, &diskStub{free: 1 * gibibyte, total: 100 * gibibyte})
		writeReadOnlyModuleCache(t, cache.Dir())
		release := cache.Use()

		// when
		trimmed := make(chan bool)
		go func() { trimmed <- cache.Trim() }()

		// then nothing is emptied under the script, and the cache is emptied once it is done
		select {
		case <-trimmed:
			t.Fatal("the cache was emptied while a script was using it")
		case <-time.After(100 * time.Millisecond):
		}
		assert.FileExists(t, filepath.Join(cache.Dir(), "go", "mod", "example.com", "dependency@v1.0.0", "go.mod"))
		release()
		assert.True(t, <-trimmed)
	})
}

func TestSharedCacheRemove(t *testing.T) {
	t.Parallel()

	t.Run("should remove the cache with the read-only module cache Go leaves in it", func(t *testing.T) {
		t.Parallel()

		// given
		cache := newSharedCache(t, &diskStub{free: 50 * gibibyte, total: 100 * gibibyte})
		writeReadOnlyModuleCache(t, cache.Dir())

		// when
		err := cache.Remove()

		// then
		require.NoError(t, err)
		assert.NoDirExists(t, cache.Dir())
		assert.NoError(t, cache.Remove(), "removing it again should be harmless")
	})
}
