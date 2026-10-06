package support

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// ModifiedPaths returns the repository-relative paths, with forward slashes, of
// the tracked files the working tree has modified since HEAD.
//
// HEAD is the baseline every dependency reader compares against, and it is the
// right one on both paths that write a changelog: in batch mode each updater
// runs after the previous updater's snapshot commit, and in local mode the
// branch is created from a commit the working tree was stashed against. Only
// modifications are listed -- a manifest the run created has no "before" to
// compare, and an added dependency is not an upgrade.
func ModifiedPaths(ctx context.Context, repoDir string) ([]string, error) {
	output, err := GitCommand(
		ctx, repoDir, "diff", "--name-only", "-z", "--no-renames", "--diff-filter=M", "HEAD",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list the files modified since HEAD in %s: %w", repoDir, err)
	}

	var paths []string
	for name := range bytes.SplitSeq(output, []byte{0}) {
		if len(name) > 0 {
			paths = append(paths, string(name))
		}
	}
	return paths, nil
}

// HeadFileContent returns the content a tracked file had at HEAD. The path is
// repository-relative with forward slashes, as [ModifiedPaths] returns it.
//
// It reads the blob through git's plumbing, so no textconv driver or filter the
// repository configures can change the bytes compared.
func HeadFileContent(ctx context.Context, repoDir, path string) ([]byte, error) {
	content, err := GitCommand(ctx, repoDir, "cat-file", "blob", "HEAD:"+path).Output()
	if err != nil {
		return nil, fmt.Errorf("failed to read %s at HEAD in %s: %w", path, repoDir, err)
	}
	return content, nil
}

// WorkingFilePath converts a repository-relative path, as [ModifiedPaths]
// returns it, into a path under repoDir using the host separator.
func WorkingFilePath(repoDir, path string) string {
	return filepath.Join(repoDir, filepath.FromSlash(path))
}

// ModifiedFile is a tracked file a run modified, as HEAD had it and as the
// working tree has it now.
type ModifiedFile struct {
	// Path is repository-relative, with forward slashes.
	Path   string
	Before []byte
	After  []byte
}

// ReadModifiedFiles returns both versions of every tracked file the working tree
// modified since HEAD whose path match accepts. It is the one way a dependency
// reader looks at what a run changed, so every reader compares the same two
// states.
func ReadModifiedFiles(ctx context.Context, repoDir string, match func(path string) bool) ([]ModifiedFile, error) {
	paths, err := ModifiedPaths(ctx, repoDir)
	if err != nil {
		return nil, err
	}

	var files []ModifiedFile
	for _, path := range paths {
		if !match(path) {
			continue
		}

		before, headErr := HeadFileContent(ctx, repoDir, path)
		if headErr != nil {
			return nil, headErr
		}

		// path came from git's own listing of the files modified in repoDir.
		after, readErr := os.ReadFile(WorkingFilePath(repoDir, path))
		if readErr != nil {
			return nil, fmt.Errorf("failed to read %s: %w", path, readErr)
		}

		files = append(files, ModifiedFile{Path: path, Before: before, After: after})
	}
	return files, nil
}
