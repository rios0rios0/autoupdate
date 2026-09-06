package cmdrunner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
