package cmdrunner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	logger "github.com/sirupsen/logrus"
)

// scriptFileMode is the mode the generated script is written with. The owner
// execute bit is set even though the script is handed to bash by path rather
// than executed directly, so that a developer reproducing a failed run can
// invoke the file they were shown.
const scriptFileMode = 0o700

// ScriptRun describes one generated-script execution.
type ScriptRun struct {
	// Body is the script source.
	Body string
	// TempPattern names the throwaway directory, as an os.MkdirTemp pattern
	// (e.g. "autoupdate-dart-local-*"). It only shows up in diagnostics.
	TempPattern string
	// Dir is the working directory the script runs in — usually the
	// repository being upgraded, which is deliberately not where the script
	// itself is written.
	Dir string
	// Env is the complete environment for the script.
	Env []string
	// LogPrefix tags the debug line carrying the script output, e.g. "dart".
	LogPrefix string
	// Verbose logs the script output at debug level.
	Verbose bool
	// RedactOutput, when set, scrubs the output before it is folded into an
	// error. A script that clones over HTTPS echoes URLs carrying the auth
	// token, and the error travels further than the script does.
	RedactOutput func(string) string
}

// RunScript writes run.Body into a throwaway directory, executes it with bash,
// and returns the combined output.
//
// The script is written to a file rather than piped through `bash -c` so that a
// failing line reports a usable line number, and it is written outside the
// repository so that it never shows up as an untracked file in the change the
// caller is about to inspect. The directory, and the script with it, is removed
// before returning whatever the outcome.
//
// On failure the output is folded into the error: a package manager explains
// itself on stdout, and an exit code on its own says nothing about which
// dependency could not be resolved.
func RunScript(ctx context.Context, runner Runner, run ScriptRun) (string, error) {
	tmpDir, err := os.MkdirTemp("", run.TempPattern)
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	scriptPath := filepath.Join(tmpDir, "upgrade.sh")
	if writeErr := os.WriteFile(scriptPath, []byte(run.Body), scriptFileMode); writeErr != nil {
		return "", fmt.Errorf("failed to write script: %w", writeErr)
	}

	result, runErr := runner.Run(ctx, "bash", []string{scriptPath}, RunOptions{
		Dir: run.Dir,
		Env: run.Env,
	})

	// The runner reports the output alongside the error, so read it before
	// deciding whether the run failed.
	var output string
	if result != nil {
		output = result.Output
	}

	if run.Verbose {
		logger.Debugf("[%s] Script output:\n%s", run.LogPrefix, output)
	}

	if runErr != nil {
		reported := output
		if run.RedactOutput != nil {
			reported = run.RedactOutput(reported)
		}
		return "", fmt.Errorf("upgrade script failed: %w\nOutput:\n%s", runErr, reported)
	}

	return output, nil
}
