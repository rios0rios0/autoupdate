//go:build linux

package support

import "os"

// MemoryLimit reports how many bytes of memory this process may use: the machine's
// physical memory, or the limit of a control group it runs in when that is lower.
func MemoryLimit() (uint64, error) {
	return memoryLimit(os.DirFS("/"))
}
