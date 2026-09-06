package support

import (
	"context"
	"os/exec"
)

// gitExecutable is the name every git invocation in this program starts from.
const gitExecutable = "git"

// GitCommand returns a command that runs git with args inside dir.
//
// It is the one place git is looked up. The executable is resolved through PATH
// here, once, with [exec.LookPath], and the absolute path that comes back is
// what the command runs -- rather than every call site handing a bare "git" to
// the child process to resolve again. Keeping the PATH reliance in a single
// function is what makes it reviewable: this program drives whichever git the
// operator installed, on whichever platform, so a fixed absolute path cannot
// be assumed, and the same holds for the language toolchains it reaches
// through the command runner.
//
// A failed lookup falls back to the bare name, so the failure surfaces where
// the command runs -- as the usual "executable file not found" error, next to
// what was being attempted -- rather than being reported from here.
func GitCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	//nolint:gosec // the executable is this package's own constant, and every
	// argument comes from internal call sites rather than from user input
	cmd := exec.CommandContext(ctx, resolveExecutable(gitExecutable), args...)
	cmd.Dir = dir

	return cmd
}

// resolveExecutable returns the absolute path of name on PATH, or name itself
// when the lookup fails.
func resolveExecutable(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return name
	}

	return path
}
