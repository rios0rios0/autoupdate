package support

import (
	"fmt"
	"os"
	"path/filepath"
)

// RepositoryWorkspacePattern names the directory a batch run gives each
// repository, as an [os.MkdirTemp] pattern. The stale-directory sweep matches the
// same name, so a run that was killed before it could clean up is cleaned up by
// the next one.
const RepositoryWorkspacePattern = "autoupdate-batch-*"

const (
	workspaceRepoDir    = "repo"
	workspaceToolingDir = "tooling"
)

// RepositoryWorkspace is the one directory a batch run gives a repository. The
// clone lives in it, and so does everything the updaters' package managers write
// outside the clone (see ScriptEnv), so removing it removes every byte the
// repository cost the run -- whether a pull request was opened, nothing needed
// upgrading, or an updater failed halfway.
type RepositoryWorkspace struct {
	root string
}

// NewRepositoryWorkspace creates a workspace under the system temporary
// directory, which TMPDIR relocates.
func NewRepositoryWorkspace() (*RepositoryWorkspace, error) {
	root, err := os.MkdirTemp("", RepositoryWorkspacePattern)
	if err != nil {
		return nil, fmt.Errorf("failed to create the repository workspace: %w", err)
	}

	workspace := &RepositoryWorkspace{root: root}
	if err = prepareToolingDir(workspace.ToolingDir()); err != nil {
		_ = RemoveTree(root)
		return nil, err
	}
	return workspace, nil
}

// Root returns the workspace directory itself.
func (w *RepositoryWorkspace) Root() string {
	return w.root
}

// RepoDir returns where the repository is cloned. It does not exist until the
// clone creates it.
func (w *RepositoryWorkspace) RepoDir() string {
	return filepath.Join(w.root, workspaceRepoDir)
}

// ToolingDir returns the directory the updaters' scripts keep their caches,
// downloads and temporary files in, through ScriptEnv.
func (w *RepositoryWorkspace) ToolingDir() string {
	return filepath.Join(w.root, workspaceToolingDir)
}

// Remove deletes the workspace and everything in it. Calling it again is harmless.
func (w *RepositoryWorkspace) Remove() error {
	return RemoveTree(w.root)
}
