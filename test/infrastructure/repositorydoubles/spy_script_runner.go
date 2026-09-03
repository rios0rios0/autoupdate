package repositorydoubles

import (
	"context"
	"os"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
)

// SpyScriptRunner is a cmdrunner.Runner that keeps the body of every script it
// is handed. cmdrunner.RunScript writes the generated script to a throwaway
// file, passes the path as the first argument and deletes the file on return,
// so a double that only records the arguments is left holding a path to
// nothing by the time a test looks at it. The body has to be read while the
// call is in flight, which is the one thing this double adds over
// StubCommandRunner.
type SpyScriptRunner struct {
	// Scripts holds the body of each script run, in call order. An argument
	// that does not name a readable file records as "".
	Scripts []string
	// Calls records every invocation, as StubCommandRunner does.
	Calls  []StubCommandCall
	output string
}

// NewSpyScriptRunner creates a SpyScriptRunner whose every run succeeds with
// the given output.
func NewSpyScriptRunner(output string) *SpyScriptRunner {
	return &SpyScriptRunner{output: output}
}

// Run records the call and the script body, and succeeds.
func (s *SpyScriptRunner) Run(
	_ context.Context, name string, args []string, opts cmdrunner.RunOptions,
) (*cmdrunner.RunResult, error) {
	s.Calls = append(s.Calls, StubCommandCall{Name: name, Args: args, Opts: opts})

	body := ""
	if len(args) > 0 {
		if content, err := os.ReadFile(args[0]); err == nil {
			body = string(content)
		}
	}
	s.Scripts = append(s.Scripts, body)

	return &cmdrunner.RunResult{Output: s.output, ExitCode: 0}, nil
}
