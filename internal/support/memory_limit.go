package support

import (
	"errors"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"
)

// errMemoryUnknown is what memoryLimit answers when nothing it reads names an amount.
var errMemoryUnknown = errors.New("neither the physical memory nor a control group limit could be read")

const (
	kibibyte = 1 << 10
	// cgroupFieldCount is the number of fields in a /proc/self/cgroup line:
	// hierarchy-ID:controller-list:cgroup-path.
	cgroupFieldCount = 3
)

// memoryLimit reports how many bytes of memory a process may use on the Linux system
// whose root directory is root: the physical memory in /proc/meminfo, or less when a
// control group caps the process. Control groups are how CI agents and container
// runtimes size a job, and /proc/meminfo knows nothing about them -- inside a container
// it reports the memory of the whole machine.
//
// The cap can sit on any ancestor of the process's own control group, so every level
// between that group and the root of its hierarchy is read, and the smallest amount
// found wins. A level whose file is missing is skipped: a container commonly sees only
// the root of its hierarchy, mounted at its own group, while /proc/self/cgroup still
// names the group's path on the host.
func memoryLimit(root fs.FS) (uint64, error) {
	limit, found := physicalMemory(root)
	for _, candidate := range cgroupMemoryLimits(root) {
		if !found || candidate < limit {
			limit, found = candidate, true
		}
	}
	if !found {
		return 0, errMemoryUnknown
	}
	return limit, nil
}

// physicalMemory reads MemTotal from /proc/meminfo.
func physicalMemory(root fs.FS) (uint64, bool) {
	data, err := fs.ReadFile(root, "proc/meminfo")
	if err != nil {
		return 0, false
	}
	for line := range strings.Lines(string(data)) {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "MemTotal:" {
			continue
		}
		kib, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil {
			return 0, false
		}
		return kib * kibibyte, true
	}
	return 0, false
}

// cgroupMemoryLimits returns every memory limit set on the process's control group or
// on one of its ancestors, under either version of control groups.
func cgroupMemoryLimits(root fs.FS) []uint64 {
	data, err := fs.ReadFile(root, "proc/self/cgroup")
	if err != nil {
		return nil
	}

	var limits []uint64
	for line := range strings.Lines(string(data)) {
		fields := strings.SplitN(strings.TrimSpace(line), ":", cgroupFieldCount)
		if len(fields) != cgroupFieldCount {
			continue
		}
		controllers, group := fields[1], fields[2]
		if fields[0] == "0" && controllers == "" {
			// The unified hierarchy of cgroup v2.
			limits = append(limits, cgroupLimits(root, "sys/fs/cgroup", group, "memory.max")...)
			continue
		}
		if slices.Contains(strings.Split(controllers, ","), "memory") {
			// The memory controller's own hierarchy under cgroup v1.
			limits = append(limits,
				cgroupLimits(root, "sys/fs/cgroup/memory", group, "memory.limit_in_bytes")...)
		}
	}
	return limits
}

// cgroupLimits reads the limit file of group and of every ancestor of it in the
// hierarchy mounted at mount, up to and including the hierarchy's root.
func cgroupLimits(root fs.FS, mount, group, file string) []uint64 {
	var limits []uint64
	for dir := path.Clean("/" + group); ; dir = path.Dir(dir) {
		if limit, ok := cgroupLimit(root, path.Join(mount, dir, file)); ok {
			limits = append(limits, limit)
		}
		if dir == "/" {
			return limits
		}
	}
}

// cgroupLimit reads one limit file. "max" is how cgroup v2 says there is no limit;
// cgroup v1 says it with a number larger than any machine's memory, which therefore
// never wins against the physical memory.
func cgroupLimit(root fs.FS, name string) (uint64, bool) {
	data, err := fs.ReadFile(root, name)
	if err != nil {
		return 0, false
	}
	value := strings.TrimSpace(string(data))
	if value == "max" {
		return 0, false
	}
	limit, err := strconv.ParseUint(value, 10, 64)
	if err != nil || limit == 0 {
		return 0, false
	}
	return limit, true
}
