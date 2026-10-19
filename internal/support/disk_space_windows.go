//go:build windows

package support

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// DiskSpace reports the bytes the current user may still write to the volume
// holding path, and the size of that volume.
func DiskSpace(path string) (uint64, uint64, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read the volume of %s: %w", path, err)
	}

	var available, total, free uint64
	if err = windows.GetDiskFreeSpaceEx(name, &available, &total, &free); err != nil {
		return 0, 0, fmt.Errorf("failed to read the volume of %s: %w", path, err)
	}
	return available, total, nil
}
