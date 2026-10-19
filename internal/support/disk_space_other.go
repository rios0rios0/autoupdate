//go:build !linux && !darwin && !windows

package support

import (
	"errors"
	"runtime"
)

// errDiskSpaceUnsupported is what DiskSpace answers where it cannot read a
// filesystem, which leaves a batch run without a shared cache.
var errDiskSpaceUnsupported = errors.New("reading the free disk space is not supported on " + runtime.GOOS)

// DiskSpace cannot read a filesystem on this platform.
func DiskSpace(string) (uint64, uint64, error) {
	return 0, 0, errDiskSpaceUnsupported
}
