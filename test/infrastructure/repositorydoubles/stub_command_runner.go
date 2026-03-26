//go:build unit

package repositorydoubles

import (
	"context"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
)

// StubCommandRunner is a test double that returns pre-configured results.
type StubCommandRunner struct {
	Calls   []StubCommandCall
	results []cmdrunner.RunResult
	errors  []error
	idx     int
}

// StubCommandCall records a single call to Run.
type StubCommandCall struct {
	Name string
	Args []string
	Opts cmdrunner.RunOptions
}

// NewStubCommandRunner creates a StubCommandRunner with the given result sequence.
func NewStubCommandRunner(results ...cmdrunner.RunResult) *StubCommandRunner {
	errs := make([]error, len(results))
	return &StubCommandRunner{results: results, errors: errs}
}

// NewStubCommandRunnerWithError creates a StubCommandRunner that returns an error.
func NewStubCommandRunnerWithError(err error) *StubCommandRunner {
	return &StubCommandRunner{
		results: []cmdrunner.RunResult{{}},
		errors:  []error{err},
	}
}

// Run returns the next pre-configured result.
func (s *StubCommandRunner) Run(
	_ context.Context, name string, args []string, opts cmdrunner.RunOptions,
) (*cmdrunner.RunResult, error) {
	s.Calls = append(s.Calls, StubCommandCall{Name: name, Args: args, Opts: opts})
	if s.idx >= len(s.results) {
		return &cmdrunner.RunResult{}, nil
	}
	result := s.results[s.idx]
	err := s.errors[s.idx]
	s.idx++
	return &result, err
}
