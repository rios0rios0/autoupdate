package support

import (
	"bytes"
	"context"
	"fmt"
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
