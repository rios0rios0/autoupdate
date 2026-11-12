//go:build darwin

package support

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// MemoryLimit reports the machine's physical memory, which macOS does not cap per
// process.
func MemoryLimit() (uint64, error) {
	memory, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0, fmt.Errorf("failed to read the physical memory: %w", err)
	}
	return memory, nil
}
