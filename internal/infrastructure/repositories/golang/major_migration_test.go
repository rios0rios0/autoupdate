package golang_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/golang"
)

func TestMigrationBranchName(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		major    golang.MajorAvailable
		expected string
	}{
		"the module's new path names the branch": {
			major: golang.MajorAvailable{
				Path: "github.com/rios0rios0/langforge", NextPath: "github.com/rios0rios0/langforge/v2",
				NextVersion: "v2.0.1",
			},
			expected: "chore/migrate-rios0rios0-langforge-v2",
		},
		"the host goes, because every dependency of one repository tends to share it": {
			major: golang.MajorAvailable{
				Path: "go.uber.org/dig", NextPath: "go.uber.org/dig/v2", NextVersion: "v2.0.0",
			},
			expected: "chore/migrate-dig-v2",
		},
		"a path that is not suffixed takes the number from the version": {
			major: golang.MajorAvailable{
				Path: "github.com/example/lib", NextPath: "github.com/example/lib",
				NextVersion: "v3.0.1",
			},
			expected: "chore/migrate-example-lib-v3",
		},
		"a fork-dot path keeps the dot that separates its major": {
			major: golang.MajorAvailable{
				Path: "gopkg.in/yaml.v2", NextPath: "gopkg.in/yaml.v3", NextVersion: "v3.0.1",
			},
			expected: "chore/migrate-yaml.v3-v3",
		},
		"a version that is not one falls back to the word": {
			major: golang.MajorAvailable{
				Path: "github.com/example/lib", NextPath: "github.com/example/lib", NextVersion: "devel",
			},
			expected: "chore/migrate-example-lib-vnext",
		},
		"a path with nothing usable in it still names a branch": {
			major: golang.MajorAvailable{
				Path: "!!!", NextPath: "???", NextVersion: "v2.0.0",
			},
			expected: "chore/migrate-module-v2",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// given / when
			branch := golang.MigrationBranchName(testCase.major)

			// then
			// Deterministic, because the branch is linked from a pull request
			// description: a second run has to find the first one's work.
			assert.Equal(t, testCase.expected, branch)
		})
	}
}

func TestMigrateImportPath(t *testing.T) {
	t.Parallel()

	const (
		oldPath = "github.com/example/lib"
		newPath = "github.com/example/lib/v3"
	)

	cases := map[string]struct {
		importPath string
		expected   string
		changed    bool
	}{
		"the module itself":  {importPath: oldPath, expected: newPath, changed: true},
		"a package under it": {importPath: oldPath + "/pkg", expected: newPath + "/pkg", changed: true},
		"a nested package under it": {
			importPath: oldPath + "/internal/x",
			expected:   newPath + "/internal/x",
			changed:    true,
		},
		"already the new major":              {importPath: newPath, changed: false},
		"a package of the new major":         {importPath: newPath + "/pkg", changed: false},
		"a different module":                 {importPath: "github.com/example/other", changed: false},
		"a path that only shares the prefix": {importPath: "github.com/example/library", changed: false},
		"a subpackage of a sibling":          {importPath: "github.com/example/lib2/pkg", changed: false},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// given / when
			migrated, changed := golang.MigrateImportPath(testCase.importPath, oldPath, newPath)

			// then
			assert.Equal(t, testCase.changed, changed)
			assert.Equal(t, testCase.expected, migrated)
		})
	}
}

func TestMigrateImportPathExcept(t *testing.T) {
	t.Parallel()

	// given / when
	migrated, changed := golang.MigrateImportPathExcept(
		"github.com/example/lib/v2/pkg",
		"github.com/example/lib",
		"github.com/example/lib/v3",
		[]string{"github.com/example/lib/v2"},
	)

	// then
	assert.False(t, changed)
	assert.Empty(t, migrated)
}

// moduleImportFixture is one file of the tree the rewrite runs over: imports of
// the module under migration, and whatever else the file carries.
const moduleImportFixture = `package main

// The old path, github.com/example/lib, is named in this comment and stays.
import (
	"github.com/example/lib"
	"github.com/example/lib/pkg"

	"strings"
)

func main() {
	println(lib.Name(), pkg.Name(), strings.ToUpper("x"))
}
`

func TestRewriteModuleImports(t *testing.T) {
	t.Parallel()

	// given
	root := t.TempDir()
	writeTestFile(t, root, "main.go", moduleImportFixture)
	writeTestFile(t, root, "internal/generated.go", `package internal

import "github.com/example/lib"
`)
	writeTestFile(t, root, "testdata/fixture.go", `package fixture

import "github.com/example/lib"
`)
	writeTestFile(t, root, "vendor/example.com/lib/lib.go", `package lib

import "github.com/example/lib/internal"
`)

	// when
	rewritten, unparsed, err := golang.RewriteModuleImports(
		root, "github.com/example/lib", "github.com/example/lib/v3",
	)

	// then
	require.NoError(t, err)
	assert.Empty(t, unparsed)
	assert.Equal(t, []string{"internal/generated.go", "main.go"}, rewritten,
		"the walk reports what it changed, in the order it walked")

	main := readTestFile(t, root, "main.go")
	assert.Contains(t, main, `"github.com/example/lib/v3"`)
	assert.Contains(t, main, `"github.com/example/lib/v3/pkg"`)
	assert.NotContains(t, main, "\"github.com/example/lib\"")
	// Nothing outside the import literals moves: the comment still names the
	// old path, and the body is untouched.
	assert.Contains(t, main, "// The old path, github.com/example/lib, is named in this comment and stays.")
	assert.Contains(t, main, "func main() {\n\tprintln(lib.Name(), pkg.Name(), strings.ToUpper(\"x\"))\n}")

	// A fixture and a vendored copy are not the repository's own code, and a
	// rewrite in either is damage rather than a migration.
	assert.Contains(t, readTestFile(t, root, "testdata/fixture.go"), `"github.com/example/lib"`)
	assert.Contains(t, readTestFile(t, root, "vendor/example.com/lib/lib.go"), `"github.com/example/lib/internal"`)
}

func TestRewriteModuleImportsLeavesUnparseableFilesAlone(t *testing.T) {
	t.Parallel()

	// given: a file whose import string is unterminated. It does not compile
	// today either, and it is most likely a template a rewrite would damage.
	const broken = "package main\n\nimport \"github.com/example/lib\n\nfunc main() {}\n"

	root := t.TempDir()
	writeTestFile(t, root, "broken.go", broken)
	writeTestFile(t, root, "main.go", moduleImportFixture)

	// when
	rewritten, unparsed, err := golang.RewriteModuleImports(
		root, "github.com/example/lib", "github.com/example/lib/v3",
	)

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"main.go"}, rewritten)
	assert.Equal(t, []string{"broken.go"}, unparsed, "a file that could not be parsed is reported")
	assert.Equal(t, broken, readTestFile(t, root, "broken.go"),
		"and is left exactly as it was")
}

func TestRewriteModuleImportsKeepsSiblingMajorImports(t *testing.T) {
	t.Parallel()

	// given
	root := t.TempDir()
	writeTestFile(t, root, "main.go", "package main\n\nimport \"github.com/example/lib/v2/pkg\"\n")

	// when
	rewritten, _, err := golang.RewriteModuleImports(root, "github.com/example/lib", "github.com/example/lib/v3")

	// then
	require.NoError(t, err)
	assert.Empty(t, rewritten)
	assert.Contains(t, readTestFile(t, root, "main.go"), `"github.com/example/lib/v2/pkg"`)
}

func TestReplacedModuleDir(t *testing.T) {
	t.Parallel()

	t.Run("should find a replacement on a line of its own", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		writeTestFile(t, root, "go.mod", "module example.com/app\n\ngo 1.25\n\n"+
			"require github.com/example/lib v1.2.3\n\n"+
			"replace github.com/example/lib => github.com/example/fork v1.2.3\n")

		// when
		dir := golang.ReplacedModuleDir(root, "github.com/example/lib")

		// then
		// The lax parse every other reader uses is defined to drop this
		// directive, so the answer has to come from the manifest's own syntax.
		assert.Equal(t, ".", dir)
	})

	t.Run("should find a replacement inside a block, and a pinned one", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		writeTestFile(t, root, "go.mod", "module example.com/app\n\ngo 1.25\n\n"+
			"require github.com/example/lib v1.2.3\n\nreplace (\n"+
			"\tgithub.com/example/lib v1.2.3 => github.com/example/fork v1.2.3\n"+
			"\tgithub.com/example/other => ../other\n)\n")

		// when
		dir := golang.ReplacedModuleDir(root, "github.com/example/lib")

		// then
		assert.Equal(t, ".", dir)
	})

	t.Run("should name the nested module that does the replacing", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		writeTestFile(t, root, "go.mod", "module example.com/app\n\ngo 1.25\n")
		writeTestFile(t, root, "tools/go.mod", "module example.com/app/tools\n\ngo 1.25\n\n"+
			"require github.com/example/lib v1.2.3\n\n"+
			"replace github.com/example/lib => ../lib\n")

		// when
		dir := golang.ReplacedModuleDir(root, "github.com/example/lib")

		// then
		// Which go.mod redirected it is said in the note, so the reader knows
		// where to look.
		assert.Equal(t, "tools", dir)
	})

	t.Run("should find nothing in a repository that does not replace it", func(t *testing.T) {
		t.Parallel()

		// given
		root := t.TempDir()
		writeTestFile(t, root, "go.mod", "module example.com/app\n\ngo 1.25\n\n"+
			"require github.com/example/lib v1.2.3\n")

		// when
		dir := golang.ReplacedModuleDir(root, "github.com/example/lib")

		// then
		assert.Empty(t, dir)
	})
}

func TestCollectMigrationChanges(t *testing.T) {
	t.Parallel()

	t.Run("should report an edited and an added file, and nothing ignored", func(t *testing.T) {
		t.Parallel()

		// given
		dir := newMigrationCheckout(t, map[string]string{
			"go.mod":     "module example.com/app\n\ngo 1.25\n",
			"main.go":    "package main\n\nfunc main() {}\n",
			".gitignore": "ignored.txt\n",
		})
		writeTestFile(t, dir, "main.go", "package main\n\nfunc main() { println(\"migrated\") }\n")
		writeTestFile(t, dir, "added.go", "package main\n\nfunc added() {}\n")
		writeTestFile(t, dir, "ignored.txt", "not part of the change set\n")

		// when
		changes, skipReason, err := golang.CollectMigrationChanges(context.Background(), dir)

		// then
		require.NoError(t, err)
		assert.Empty(t, skipReason)
		require.Len(t, changes, 2, "the ignored file is not part of what would be pushed")

		byPath := map[string]string{}
		for _, change := range changes {
			byPath[change.Path] = change.ChangeType
		}
		assert.Equal(t, map[string]string{"main.go": "edit", "added.go": "add"}, byPath)
	})

	t.Run("should refuse a deletion the provider API cannot push", func(t *testing.T) {
		t.Parallel()

		// given
		dir := newMigrationCheckout(t, map[string]string{
			"go.mod":    "module example.com/app\n\ngo 1.25\n",
			"vendor.go": "package main\n\nfunc vendored() {}\n",
		})
		require.NoError(t, os.Remove(filepath.Join(dir, "vendor.go")))

		// when
		changes, skipReason, err := golang.CollectMigrationChanges(context.Background(), dir)

		// then
		require.NoError(t, err)
		assert.Nil(t, changes)
		assert.Contains(t, skipReason, "`vendor.go`",
			"the reader is told which file, not that something went wrong")
		assert.Contains(t, skipReason, "cannot push")
	})

	t.Run("should refuse a rename the provider API cannot express", func(t *testing.T) {
		t.Parallel()

		// given
		dir := newMigrationCheckout(t, map[string]string{
			"go.mod": "module example.com/app\n\ngo 1.25\n",
			"lib.go": "package main\n\nfunc lib() {}\n",
		})
		runGit(t, dir, "mv", "lib.go", "vendor.go")

		// when
		changes, skipReason, err := golang.CollectMigrationChanges(context.Background(), dir)

		// then
		require.NoError(t, err)
		assert.Nil(t, changes)
		assert.Contains(t, skipReason, "is deleted or renamed")
	})

	t.Run("should say so when the migration changed nothing", func(t *testing.T) {
		t.Parallel()

		// given
		dir := newMigrationCheckout(t, map[string]string{
			"go.mod":  "module example.com/app\n\ngo 1.25\n",
			"main.go": "package main\n\nfunc main() {}\n",
		})

		// when
		changes, skipReason, err := golang.CollectMigrationChanges(context.Background(), dir)

		// then
		require.NoError(t, err)
		assert.Nil(t, changes)
		assert.Contains(t, skipReason, "changed nothing")
	})
}

func TestMigrationNote(t *testing.T) {
	t.Parallel()

	t.Run("should put a multi-line message on one line", func(t *testing.T) {
		t.Parallel()

		// given / when
		note := golang.MigrationNote("could not resolve\n\tbecause the module is private\n", "")

		// then
		// A compiler error pasted whole would wreck the table it is rendered
		// into.
		assert.Equal(t, "could not resolve because the module is private", note)
	})

	t.Run("should redact the token out of it", func(t *testing.T) {
		t.Parallel()

		// given / when
		note := golang.MigrationNote(
			"fatal: could not read from https://x-access-token:"+"ghs_secret"+"@github.com/o/r",
			"ghs_secret",
		)

		// then
		// The note is rendered into a pull request description, which is read
		// by everyone the repository is visible to.
		assert.NotContains(t, note, "ghs_secret")
		assert.Contains(t, note, "[REDACTED]")
	})

	t.Run("should cut a message longer than a table cell", func(t *testing.T) {
		t.Parallel()

		// given: a message long enough to be cut, with a rune on the boundary.
		message := strings.Repeat("é", 200)

		// when
		note := golang.MigrationNote(message, "")

		// then
		assert.LessOrEqual(t, len(note), 163, "the limit plus the ellipsis")
		assert.True(t, strings.HasSuffix(note, "..."), "the cut is marked")
		assert.Equal(t, strings.ToValidUTF8(note, ""), note, "the cut is on a rune boundary")
	})
}

func TestMigrationCommitMessage(t *testing.T) {
	t.Parallel()

	// given
	major := golang.MajorAvailable{
		Path: "github.com/example/lib", CurrentVersion: "v1.2.3",
		NextPath: "github.com/example/lib/v3", NextVersion: "v3.0.1",
	}

	// when
	message := golang.MigrationCommitMessage(major, false, "3 file(s) could not be parsed and kept their old imports")

	// then
	// It is also the pull request title and body the provider prefills, so it
	// has to say what the branch is and what it is not.
	assert.Contains(
		t,
		message,
		"chore(deps): prepare the migration of github.com/example/lib to github.com/example/lib/v3",
	)
	assert.Contains(t, message, "rewrites every import of `github.com/example/lib` to `github.com/example/lib/v3`")
	assert.Contains(t, message, "It does not build yet.")
	assert.Contains(t, message, "3 file(s) could not be parsed and kept their old imports")
}

func TestMigrationChangelogEntry(t *testing.T) {
	t.Parallel()

	// given
	major := golang.MajorAvailable{Path: "github.com/a/b", NextPath: "github.com/a/b/v2"}

	// when
	entry := golang.MigrationChangelogEntry(major)

	// then
	// Shaped like the upgrade's own entries, because it lands in the same
	// `### Changed` section beside them.
	assert.Equal(t,
		"- changed the imports and the module requirement of `github.com/a/b` to `github.com/a/b/v2`",
		entry)
}

func TestBaseBranchName(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"refs/heads/main": "main",
		"main":            "main",
		"release/1.x":     "release/1.x",
	}

	for ref, expected := range cases {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()

			// given / when / then
			assert.Equal(t, expected, golang.BaseBranchName(ref))
		})
	}
	assert.Equal(t, "release/1.x", golang.BaseBranchName(" release/1.x "))
}

func TestMigrationParsingHelpers(t *testing.T) {
	t.Parallel()

	// given / when / then
	assert.Equal(t, "2", golang.VersionMajorNumber("v2.1.0"))
	assert.Empty(t, golang.VersionMajorNumber("devel"))
	assert.Equal(t, "/v3", golang.PathMajorSuffix("example.com/lib/v3"))
	assert.Empty(t, golang.PathMajorSuffix("gopkg.in/yaml.v3"))
	assert.Equal(t, "refs/heads/main", golang.RefsHeads("main"))
	assert.Equal(t, "refs/remotes/origin/main", golang.RefsHeads("refs/remotes/origin/main"))
}

// --- fixtures ---

// migrationCheckout is a throwaway git repository with one commit on `main`:
// what the preparation needs, since it checks the base branch out into a
// worktree of its own.
func newMigrationCheckout(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		writeTestFile(t, dir, name, content)
	}

	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "autoupdate@example.com")
	runGit(t, dir, "config", "user.name", "autoupdate")
	// The operator's own configuration must not reach these fixtures: a
	// signing requirement or a pre-commit hook would fail the commit and look
	// like a bug in the code under test.
	runGit(t, dir, "config", "commit.gpgsign", "false")
	runGit(t, dir, "config", "core.hooksPath", filepath.Join(dir, ".no-hooks"))
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "initial")

	return dir
}

// runGit runs one git command in dir and fails the test when git refuses.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), string(output))
}

// writeTestFile writes a file into a fixture tree, creating its directories.
func writeTestFile(t *testing.T, root, name, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(name))

	// A directory needs the owner search bit, so 0o700 is the least-privilege
	// mode here; the rule compares every mode against 0600 regardless of the
	// kind of node being created.
	// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// readTestFile reads a file of a fixture tree.
func readTestFile(t *testing.T, root, name string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	require.NoError(t, err)
	return string(content)
}
