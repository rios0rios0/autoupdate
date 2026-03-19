package support

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	logger "github.com/sirupsen/logrus"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
)

// WalkFilesByExtension finds all files under root whose names end with ext.
// Returns paths relative to root, normalized to forward slashes.
func WalkFilesByExtension(root, ext string) ([]string, error) {
	var matches []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip hidden directories (e.g. .git)
			if d.Name() != "." && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ext) {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			matches = append(matches, filepath.ToSlash(rel))
		}
		return nil
	})
	return matches, err
}

// WalkFilesByPredicate finds all files under root where match(baseName)
// returns true. Returns paths relative to root, normalized to forward slashes.
func WalkFilesByPredicate(root string, match func(name string) bool) ([]string, error) {
	var matches []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() != "." && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if match(d.Name()) {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			matches = append(matches, filepath.ToSlash(rel))
		}
		return nil
	})
	return matches, err
}

// WriteFileChanges writes file changes to the filesystem rooted at rootDir.
func WriteFileChanges(rootDir string, changes []entities.FileChange) error {
	for _, c := range changes {
		fullPath := filepath.Join(rootDir, c.Path)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", c.Path, err)
		}
		if err := os.WriteFile(fullPath, []byte(c.Content), 0o600); err != nil {
			return fmt.Errorf("failed to write %s: %w", c.Path, err)
		}
	}
	return nil
}

// RedactTokens replaces occurrences of the given tokens with "[REDACTED]"
// in the input string. This prevents auth tokens from leaking into logs
// or error messages when script output is captured.
func RedactTokens(input string, tokens ...string) string {
	for _, token := range tokens {
		if token != "" {
			input = strings.ReplaceAll(input, token, "[REDACTED]")
		}
	}
	return input
}

// HasUncommittedChanges returns true when the git working tree at repoDir
// contains unstaged or untracked modifications. On error (e.g. git not
// found, not a repo) it returns true to avoid false negatives that would
// incorrectly skip updates.
func HasUncommittedChanges(ctx context.Context, repoDir string) bool {
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = repoDir
	output, err := cmd.Output()
	if err != nil {
		logger.Warnf("Failed to check git status in %s: %v", repoDir, err)
		return true
	}
	return len(strings.TrimSpace(string(output))) > 0
}

// LocalChangelogUpdate reads CHANGELOG.md from repoDir, inserts entries,
// and writes it back if modified. Returns true if the file was updated.
func LocalChangelogUpdate(repoDir string, entries []string) bool {
	changelogPath := filepath.Clean(filepath.Join(repoDir, "CHANGELOG.md"))
	data, err := os.ReadFile(changelogPath)
	if err != nil {
		logger.Warnf("Failed to read CHANGELOG.md: %v", err)
		return false
	}

	content := string(data)
	modified := entities.InsertChangelogEntry(content, entries)
	if modified == content {
		return false
	}

	writeErr := os.WriteFile( //nolint:gosec // repoDir is a controlled internal path
		changelogPath,
		[]byte(modified),
		0o600,
	)
	if writeErr != nil {
		logger.Warnf("Failed to write CHANGELOG.md: %v", writeErr)
		return false
	}
	return true
}
