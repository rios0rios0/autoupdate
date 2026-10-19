//go:build linux || darwin

package support

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// DiskSpace reports the bytes an unprivileged process may still write to the
// filesystem holding path, and the size of that filesystem.
func DiskSpace(path string) (uint64, uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, 0, fmt.Errorf("failed to read the filesystem of %s: %w", path, err)
	}
	blockSize := uint64(stat.Bsize) //nolint:gosec // a block size is never negative; the field is signed on Linux
	return stat.Bavail * blockSize, stat.Blocks * blockSize, nil
}
