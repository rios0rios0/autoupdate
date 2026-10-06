package repositorydoubles

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
)

const (
	// fakeDirMode keeps the directories the fake creates owner-only: they live
	// under t.TempDir() and are read by the test process alone.
	fakeDirMode  = 0o700
	fakeFileMode = 0o600
)

// FakeUpgradeRunner is a cmdrunner.Runner standing in for a package manager:
// every run writes the given files into the directory the command runs in, the
// way the upgrade script would have rewritten the manifests, and succeeds with
// the given output. It lets a test drive an updater's ApplyUpdates end to end
// -- the script run, the change detection and the changelog written from what
// moved -- without a toolchain or a network.
type FakeUpgradeRunner struct {
	// Calls records every invocation, as StubCommandRunner does.
	Calls  []StubCommandCall
	files  map[string]string
	output string
}

// NewFakeUpgradeRunner creates a FakeUpgradeRunner writing files, given as
// repository-relative paths with forward slashes.
func NewFakeUpgradeRunner(files map[string]string, output string) *FakeUpgradeRunner {
	return &FakeUpgradeRunner{files: files, output: output}
}

// Run records the call, writes the files under opts.Dir, and succeeds.
func (f *FakeUpgradeRunner) Run(
	_ context.Context, name string, args []string, opts cmdrunner.RunOptions,
) (*cmdrunner.RunResult, error) {
	f.Calls = append(f.Calls, StubCommandCall{Name: name, Args: args, Opts: opts})

	for path, content := range f.files {
		full := filepath.Join(opts.Dir, filepath.FromSlash(path))
		// A directory needs the owner search bit, so 0o700 is the
		// least-privilege mode here.
		// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
		if err := os.MkdirAll(filepath.Dir(full), fakeDirMode); err != nil {
			return nil, fmt.Errorf("failed to create the directory for %s: %w", path, err)
		}
		if err := os.WriteFile(full, []byte(content), fakeFileMode); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", path, err)
		}
	}

	return &cmdrunner.RunResult{Output: f.output}, nil
}
