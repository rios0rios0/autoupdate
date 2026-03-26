package cmdrunner

import (
	"context"
	"fmt"
	"os/exec"
)

// Runner abstracts subprocess execution for testability.
type Runner interface {
	Run(ctx context.Context, name string, args []string, opts RunOptions) (*RunResult, error)
}

// RunOptions configures subprocess execution.
type RunOptions struct {
	Dir string
	Env []string
}

// RunResult captures the output of a subprocess.
type RunResult struct {
	Output   string
	ExitCode int
}

// DefaultRunner executes subprocesses via [exec.CommandContext].
type DefaultRunner struct{}

// NewDefaultRunner creates a DefaultRunner.
func NewDefaultRunner() Runner {
	return &DefaultRunner{}
}

// Run executes a command and returns its combined output.
func (r *DefaultRunner) Run(
	ctx context.Context, name string, args []string, opts RunOptions,
) (*RunResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	if len(opts.Env) > 0 {
		cmd.Env = opts.Env
	}

	output, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if ok := isExitError(err, &exitErr); ok {
			exitCode = exitErr.ExitCode()
			return &RunResult{Output: string(output), ExitCode: exitCode},
				fmt.Errorf("command %s exited with code %d", name, exitCode)
		}
		return &RunResult{Output: string(output), ExitCode: -1},
			fmt.Errorf("failed to execute %s: %w", name, err)
	}

	return &RunResult{Output: string(output), ExitCode: exitCode}, nil
}

func isExitError(err error, target **exec.ExitError) bool {
	if e, ok := err.(*exec.ExitError); ok { //nolint:errorlint // need concrete type
		*target = e
		return true
	}
	return false
}
