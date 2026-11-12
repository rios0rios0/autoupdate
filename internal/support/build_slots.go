package support

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// BuildSlots is how a batch run limits the builds of one kind it has going at once,
// across every repository it processes. An updater whose builds are heavy enough that
// a machine holds only a few of them owns one pool, and the registry holds one updater
// instance per run, which is what makes the limit run-wide.
//
// A nil *BuildSlots is valid and limits nothing.
type BuildSlots struct {
	// kind names the builds in what the run logs and reports, such as "Go".
	kind string
	// tokens holds one token per build running.
	tokens chan struct{}
}

// NewBuildSlots returns room for size builds of kind at once, and never less than one.
func NewBuildSlots(kind string, size int) *BuildSlots {
	return &BuildSlots{kind: kind, tokens: make(chan struct{}, max(1, size))}
}

// Size reports how many builds may run at once, or zero for a nil pool.
func (s *BuildSlots) Size() int {
	if s == nil {
		return 0
	}
	return cap(s.tokens)
}

// Acquire waits until one more build may start for repo, and returns the function that
// frees its slot again; calling that function twice frees the slot once. It fails only
// when ctx ends while it waits.
//
// It says so when the wait begins, so a run that looks stalled reads as a queue.
func (s *BuildSlots) Acquire(
	ctx context.Context, logPrefix string, repo entities.Repository,
) (func(), error) {
	if s == nil {
		return func() {
			// Without a pool no slot was taken, so there is none to free.
		}, nil
	}

	select {
	case s.tokens <- struct{}{}:
	default:
		logger.Infof("[%s] %s/%s is waiting for another repository's %s build to finish (%d at a time)",
			logPrefix, repo.Organization, repo.Name, s.kind, cap(s.tokens))
		select {
		case s.tokens <- struct{}{}:
		case <-ctx.Done():
			return nil, fmt.Errorf("stopped waiting for a %s build slot: %w", s.kind, ctx.Err())
		}
	}

	var free sync.Once
	return func() { free.Do(func() { <-s.tokens }) }, nil
}

// BuildSlotsForMachine is how many builds that each need up to perBuild bytes of memory
// this machine can run side by side: one per perBuild of the memory the process may use
// (see MemoryLimit), at most one per CPU, and never fewer than one.
//
// Where the memory cannot be read it is one, which is never wrong, only slower.
func BuildSlotsForMachine(perBuild uint64) int {
	memory, err := MemoryLimit()
	if err != nil {
		logger.Debugf("[autoupdate] Could not read how much memory this process may use: %v", err)
		return 1
	}
	return buildSlotsFor(memory, runtime.NumCPU(), perBuild)
}

// buildSlotsFor is BuildSlotsForMachine for a machine of the given memory and CPUs.
func buildSlotsFor(memory uint64, cpus int, perBuild uint64) int {
	slots := max(cpus, 1)
	if perBuild == 0 {
		return slots
	}
	if byMemory := memory / perBuild; byMemory < uint64(slots) {
		return max(int(byMemory), 1) //nolint:gosec // below slots, which is an int
	}
	return slots
}
