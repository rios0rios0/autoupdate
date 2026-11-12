//go:build !linux && !darwin

package support

import (
	"errors"
	"runtime"
)

// errMemoryLimitUnsupported is what MemoryLimit answers where it cannot read the
// memory, which leaves a batch run with one slot for each kind of heavy build.
var errMemoryLimitUnsupported = errors.New("reading the memory limit is not supported on " + runtime.GOOS)

// MemoryLimit cannot read the memory on this platform.
func MemoryLimit() (uint64, error) {
	return 0, errMemoryLimitUnsupported
}
