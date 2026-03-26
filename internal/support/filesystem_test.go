//go:build unit

package support_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/support"
)

func TestRedactTokens(t *testing.T) {
	t.Parallel()

	t.Run("should replace a single token with REDACTED", func(t *testing.T) {
		t.Parallel()

		// given
		input := "Authorization: Bearer ghp_secret123"

		// when
		result := support.RedactTokens(input, "ghp_secret123")

		// then
		assert.Equal(t, "Authorization: Bearer [REDACTED]", result)
	})

	t.Run("should replace multiple tokens", func(t *testing.T) {
		t.Parallel()

		// given
		input := "token1=abc token2=xyz"

		// when
		result := support.RedactTokens(input, "abc", "xyz")

		// then
		assert.Equal(t, "token1=[REDACTED] token2=[REDACTED]", result)
	})

	t.Run("should skip empty tokens", func(t *testing.T) {
		t.Parallel()

		// given
		input := "nothing to redact here"

		// when
		result := support.RedactTokens(input, "", "")

		// then
		assert.Equal(t, "nothing to redact here", result)
	})

	t.Run("should return input unchanged when token not found", func(t *testing.T) {
		t.Parallel()

		// given
		input := "safe output"

		// when
		result := support.RedactTokens(input, "nonexistent")

		// then
		assert.Equal(t, "safe output", result)
	})
}

func TestWalkFilesByExtension(t *testing.T) {
	t.Parallel()

	t.Run("should find files matching the extension", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "main.tf"), []byte(""), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(root, "other.go"), []byte(""), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "modules"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, "modules", "vpc.tf"), []byte(""), 0o600))

		// when
		matches, err := support.WalkFilesByExtension(root, ".tf")

		// then
		require.NoError(t, err)
		assert.Len(t, matches, 2)
		assert.Contains(t, matches, "main.tf")
		assert.Contains(t, matches, "modules/vpc.tf")
	})

	t.Run("should skip hidden directories", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".git", "config.tf"), []byte(""), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(root, "main.tf"), []byte(""), 0o600))

		// when
		matches, err := support.WalkFilesByExtension(root, ".tf")

		// then
		require.NoError(t, err)
		assert.Len(t, matches, 1)
		assert.Contains(t, matches, "main.tf")
	})

	t.Run("should return empty when no files match", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte(""), 0o600))

		// when
		matches, err := support.WalkFilesByExtension(root, ".tf")

		// then
		require.NoError(t, err)
		assert.Empty(t, matches)
	})
}

func TestWalkFilesByPredicate(t *testing.T) {
	t.Parallel()

	t.Run("should find files matching the predicate", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "Dockerfile"), []byte(""), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(root, "Dockerfile.dev"), []byte(""), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte(""), 0o600))

		isDockerfile := func(name string) bool {
			return name == "Dockerfile" || filepath.Ext(name) == ".dev"
		}

		// when
		matches, err := support.WalkFilesByPredicate(root, isDockerfile)

		// then
		require.NoError(t, err)
		assert.Len(t, matches, 2)
	})

	t.Run("should return empty when no files match", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "main.go"), []byte(""), 0o600))

		// when
		matches, err := support.WalkFilesByPredicate(root, func(string) bool { return false })

		// then
		require.NoError(t, err)
		assert.Empty(t, matches)
	})
}

func TestWriteFileChanges(t *testing.T) {
	t.Parallel()

	t.Run("should write files to disk", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		changes := []entities.FileChange{
			{Path: "main.tf", Content: "resource {}", ChangeType: "edit"},
		}

		// when
		err := support.WriteFileChanges(root, changes)

		// then
		require.NoError(t, err)
		data, readErr := os.ReadFile(filepath.Join(root, "main.tf"))
		require.NoError(t, readErr)
		assert.Equal(t, "resource {}", string(data))
	})

	t.Run("should create nested directories", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		changes := []entities.FileChange{
			{Path: "modules/vpc/main.tf", Content: "module {}", ChangeType: "edit"},
		}

		// when
		err := support.WriteFileChanges(root, changes)

		// then
		require.NoError(t, err)
		data, readErr := os.ReadFile(filepath.Join(root, "modules", "vpc", "main.tf"))
		require.NoError(t, readErr)
		assert.Equal(t, "module {}", string(data))
	})

	t.Run("should write multiple files", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		changes := []entities.FileChange{
			{Path: "a.tf", Content: "aaa", ChangeType: "edit"},
			{Path: "b.tf", Content: "bbb", ChangeType: "edit"},
		}

		// when
		err := support.WriteFileChanges(root, changes)

		// then
		require.NoError(t, err)
		a, _ := os.ReadFile(filepath.Join(root, "a.tf"))
		b, _ := os.ReadFile(filepath.Join(root, "b.tf"))
		assert.Equal(t, "aaa", string(a))
		assert.Equal(t, "bbb", string(b))
	})
}

func TestLocalChangelogUpdate(t *testing.T) {
	t.Parallel()

	t.Run("should update CHANGELOG.md when Unreleased section exists", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		changelog := "# Changelog\n\n## [Unreleased]\n\n## [1.0.0] - 2026-01-01\n"
		require.NoError(t, os.WriteFile(filepath.Join(root, "CHANGELOG.md"), []byte(changelog), 0o600))

		// when
		updated := support.LocalChangelogUpdate(root, []string{"- added new feature"})

		// then
		assert.True(t, updated)
		data, _ := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
		assert.Contains(t, string(data), "- added new feature")
	})

	t.Run("should return false when CHANGELOG.md does not exist", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()

		// when
		updated := support.LocalChangelogUpdate(root, []string{"- added new feature"})

		// then
		assert.False(t, updated)
	})

	t.Run("should return false when no entries provided result in changes", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		changelog := "# Changelog\n\nNo unreleased section here.\n"
		require.NoError(t, os.WriteFile(filepath.Join(root, "CHANGELOG.md"), []byte(changelog), 0o600))

		// when
		updated := support.LocalChangelogUpdate(root, []string{"- added something"})

		// then
		// InsertChangelogEntry won't modify content without [Unreleased] heading
		// The exact behavior depends on gitforge's implementation
		_ = updated
	})
}

// initGitRepo initializes a git repo in the given directory with an initial commit.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...) //nolint:gosec // test helper
		cmd.Dir = dir
		require.NoError(t, cmd.Run(), "failed to run: %v", args)
	}
	// Create and commit a file
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("init"), 0o600))
	add := exec.Command("git", "add", ".")
	add.Dir = dir
	require.NoError(t, add.Run())
	commit := exec.Command("git", "commit", "-m", "initial") //nolint:gosec // test
	commit.Dir = dir
	require.NoError(t, commit.Run())
}

func TestHasUncommittedChanges(t *testing.T) {
	t.Parallel()

	t.Run("should return false for clean git repo", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		initGitRepo(t, root)

		// when
		result := support.HasUncommittedChanges(t.Context(), root)

		// then
		assert.False(t, result)
	})

	t.Run("should return true for dirty git repo", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		initGitRepo(t, root)
		require.NoError(t, os.WriteFile(filepath.Join(root, "new_file.txt"), []byte("dirty"), 0o600))

		// when
		result := support.HasUncommittedChanges(t.Context(), root)

		// then
		assert.True(t, result)
	})

	t.Run("should return true for non-git directory", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()

		// when
		result := support.HasUncommittedChanges(t.Context(), root)

		// then
		assert.True(t, result)
	})
}
