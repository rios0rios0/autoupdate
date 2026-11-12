// Package overlap measures how many of the calls a test drives run at the same time,
// for the code that must keep some kind of work from running side by side.
package overlap

import (
	"sync"
	"time"
)

// Recorder stands in for the work being limited: every call to Hold counts as one
// piece of work that starts, stays running until the test calls Release, and ends.
type Recorder struct {
	mu      sync.Mutex
	running int
	peak    int
	runs    int
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

// NewRecorder returns a Recorder for up to calls pieces of work.
func NewRecorder(calls int) *Recorder {
	return &Recorder{
		started: make(chan struct{}, calls),
		release: make(chan struct{}),
	}
}

// Hold records one piece of work starting, keeps it running until Release, and records
// it ending.
func (r *Recorder) Hold() {
	r.mu.Lock()
	r.runs++
	r.running++
	r.peak = max(r.peak, r.running)
	r.mu.Unlock()

	r.started <- struct{}{}
	<-r.release

	r.mu.Lock()
	r.running--
	r.mu.Unlock()
}

// AwaitStarts blocks until n pieces of work have started.
func (r *Recorder) AwaitStarts(n int) {
	for range n {
		<-r.started
	}
}

// AnotherStartsWithin reports whether one more piece of work starts within d. Work that
// is held back never starts, however long d is; d only bounds how quickly work that is
// not held back shows up.
func (r *Recorder) AnotherStartsWithin(d time.Duration) bool {
	select {
	case <-r.started:
		return true
	case <-time.After(d):
		return false
	}
}

// Release lets every piece of work held, and every one still to come, run to its end.
func (r *Recorder) Release() {
	r.once.Do(func() { close(r.release) })
}

// Peak reports the most pieces of work that ran at the same time.
func (r *Recorder) Peak() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.peak
}

// Runs reports how many pieces of work started.
func (r *Recorder) Runs() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs
}
