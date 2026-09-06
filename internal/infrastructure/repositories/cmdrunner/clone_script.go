package cmdrunner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/support"
)

// cloneDirName is the directory, under the throwaway root, the script clones
// the repository into.
const cloneDirName = "repo"

// CloneScriptRun describes a script that clones a repository into a throwaway
// directory of its own and works there -- the remote flow of the script-driven
// updaters, which clone, upgrade and push from bash.
type CloneScriptRun struct {
	// Body is the script source.
	Body string
	// TempPattern names the throwaway directories, as an os.MkdirTemp pattern
	// (e.g. "autoupdate-python-*"). It only shows up in diagnostics.
	TempPattern string
	// Env builds the script environment once the clone directory is known;
	// the script is expected to clone into repoDir.
	Env func(repoDir string) []string
	// Secrets are scrubbed from the output before it is folded into an error.
	// A script that clones over HTTPS echoes URLs carrying the auth token, and
	// the error travels further than the script does.
	Secrets []string
}

// RunCloneScript creates the directory the script clones into, runs the script
// with it as the working directory, and returns the combined output. The
// directory -- clone and all -- is removed before returning whatever the
// outcome, so nothing the script pushed or left behind outlives the call.
func RunCloneScript(ctx context.Context, runner Runner, run CloneScriptRun) (string, error) {
	cloneRoot, err := os.MkdirTemp("", run.TempPattern)
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(cloneRoot)

	return RunScript(ctx, runner, ScriptRun{
		Body:        run.Body,
		TempPattern: run.TempPattern,
		Dir:         cloneRoot,
		Env:         run.Env(filepath.Join(cloneRoot, cloneDirName)),
		RedactOutput: func(output string) string {
			return support.RedactTokens(output, run.Secrets...)
		},
	})
}

const (
	// changesPushedMarker is echoed by every clone script that pushed its branch.
	changesPushedMarker = "CHANGES_PUSHED=true"

	// versionUpdatedSuffix completes the runtime-version marker: a script that
	// moved the pin echoes "<VersionMarker>_UPDATED=true", the spelling
	// support.VersionPinUpdateScript emits.
	versionUpdatedSuffix = "_UPDATED=true"
)

// UpgradeScriptRun is a clone script that reports what it did through markers
// echoed on its output: the "changes pushed" marker every updater script
// shares, and the updater's own runtime-version marker.
type UpgradeScriptRun struct {
	CloneScriptRun

	// VersionMarker is the variable the runtime-version marker is spelled
	// after, e.g. "PYTHON_VERSION" for the "PYTHON_VERSION_UPDATED=true" the
	// script echoes when it moved the pin.
	VersionMarker string
}

// UpgradeScriptResult is what the markers on the script output amount to.
type UpgradeScriptResult struct {
	// Output is the combined script output, kept for the callers that read
	// further detail out of it.
	Output string
	// HasChanges reports whether the script pushed anything.
	HasChanges bool
	// VersionUpdated reports whether the script moved the runtime version pin.
	VersionUpdated bool
}

// RunUpgradeScript runs a clone script and reads back the markers it echoed.
// The markers are how the bash half of an updater reports to the Go half, and
// reading them in one place keeps every updater agreeing on what they mean.
func RunUpgradeScript(
	ctx context.Context,
	runner Runner,
	run UpgradeScriptRun,
) (UpgradeScriptResult, error) {
	output, err := RunCloneScript(ctx, runner, run.CloneScriptRun)
	if err != nil {
		return UpgradeScriptResult{}, err
	}

	return UpgradeScriptResult{
		Output:         output,
		HasChanges:     strings.Contains(output, changesPushedMarker),
		VersionUpdated: strings.Contains(output, run.VersionMarker+versionUpdatedSuffix),
	}, nil
}
