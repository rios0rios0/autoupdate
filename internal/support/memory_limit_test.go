package support_test

import (
	"strconv"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/support"
)

// cgroupV1Unlimited is the value cgroup v1 reports for a group without a limit: the
// largest page-aligned int64.
const cgroupV1Unlimited = "9223372036854771712"

// linuxSystem lays out the files memoryLimit reads: /proc/meminfo with the machine's
// memory in GiB (none when zero), /proc/self/cgroup, and any other file the case needs.
func linuxSystem(memTotalGiB uint64, cgroup string, files map[string]string) fstest.MapFS {
	system := fstest.MapFS{}
	if memTotalGiB > 0 {
		kib := strconv.FormatUint(memTotalGiB*gibibyte/1024, 10)
		system["proc/meminfo"] = &fstest.MapFile{
			Data: []byte("MemTotal:       " + kib + " kB\nMemFree:          123456 kB\n"),
		}
	}
	if cgroup != "" {
		system["proc/self/cgroup"] = &fstest.MapFile{Data: []byte(cgroup)}
	}
	for name, content := range files {
		system[name] = &fstest.MapFile{Data: []byte(content + "\n")}
	}
	return system
}

func TestMemoryLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		system fstest.MapFS
		want   uint64
	}{
		{
			name: "should read the physical memory when no control group limits the process",
			system: linuxSystem(
				16,
				"0::/user.slice\n",
				map[string]string{"sys/fs/cgroup/user.slice/memory.max": "max"},
			),
			want: 16 * gibibyte,
		},
		{
			name: "should read a cgroup v2 limit set on an ancestor of the process's group",
			system: linuxSystem(7, "0::/agent.slice/job.scope\n", map[string]string{
				"sys/fs/cgroup/agent.slice/job.scope/memory.max": "max",
				"sys/fs/cgroup/agent.slice/memory.max":           strconv.FormatUint(6*gibibyte, 10),
			}),
			want: 6 * gibibyte,
		},
		{
			name: "should read the limit of a container that sees only the root of its hierarchy",
			system: linuxSystem(64, "0::/kubepods/pod-1/container-1\n", map[string]string{
				"sys/fs/cgroup/memory.max": strconv.FormatUint(4*gibibyte, 10),
			}),
			want: 4 * gibibyte,
		},
		{
			name: "should read a cgroup v1 memory limit",
			system: linuxSystem(32, "5:cpuacct,cpu:/docker/abc\n4:memory:/docker/abc\n", map[string]string{
				"sys/fs/cgroup/memory/docker/abc/memory.limit_in_bytes": strconv.FormatUint(2*gibibyte, 10),
			}),
			want: 2 * gibibyte,
		},
		{
			name: "should keep the physical memory when cgroup v1 reports no limit",
			system: linuxSystem(8, "4:memory:/\n", map[string]string{
				"sys/fs/cgroup/memory/memory.limit_in_bytes": cgroupV1Unlimited,
			}),
			want: 8 * gibibyte,
		},
		{
			name: "should read a control group limit when the physical memory cannot be read",
			system: linuxSystem(0, "0::/\n", map[string]string{
				"proc/meminfo":             "MemTotal:       lots kB",
				"sys/fs/cgroup/memory.max": strconv.FormatUint(3*gibibyte, 10),
			}),
			want: 3 * gibibyte,
		},
		{
			name: "should ignore what it cannot read as an amount",
			system: linuxSystem(8, "not a cgroup line\n0::/job.scope\n", map[string]string{
				"sys/fs/cgroup/job.scope/memory.max": "unlimited",
			}),
			want: 8 * gibibyte,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// given the system of the case

			// when
			got, err := support.MemoryLimitFrom(tt.system)

			// then
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("should fail when neither the memory nor a limit can be read", func(t *testing.T) {
		t.Parallel()

		// given a system without /proc/meminfo or any control group file
		system := linuxSystem(0, "", nil)

		// when
		_, err := support.MemoryLimitFrom(system)

		// then
		assert.Error(t, err)
	})
}
