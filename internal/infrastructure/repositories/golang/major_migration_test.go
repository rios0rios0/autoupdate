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
		changes, err := golang.CollectMigrationChanges(context.Background(), dir)

		// then
		require.NoError(t, err)
		assert.Empty(t, changes.Skip)
		assert.Empty(t, changes.LeftInPlace)
		require.Len(t, changes.Files, 2, "the ignored file is not part of what would be pushed")
		assert.Equal(t, map[string]string{"main.go": "edit", "added.go": "add"}, changeTypesByPath(changes.Files))
	})

	t.Run("should refuse a deletion the provider API cannot push", func(t *testing.T) {
		t.Parallel()

		// given: a repository that vendors, and a deletion beside the vendor
		// directory rather than in it. A file whose name merely starts like
		// the directory is not in it, and is refused as every deletion was.
		dir := newMigrationCheckout(t, map[string]string{
			"go.mod":                               "module example.com/app\n\ngo 1.25\n",
			"vendor.go":                            "package main\n\nfunc vendored() {}\n",
			"vendor/modules.txt":                   newMajorVendorManifest,
			"vendor/github.com/example/lib/lib.go": "package lib\n",
		})
		require.NoError(t, os.Remove(filepath.Join(dir, "vendor.go")))

		// when
		changes, err := golang.CollectMigrationChanges(context.Background(), dir)

		// then
		require.NoError(t, err)
		assert.Nil(t, changes.Files)
		assert.Equal(t,
			"`vendor.go` is deleted or renamed by the migration, which the provider API cannot push",
			changes.Skip,
			"the reader is told which file, not that something went wrong")
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
		changes, err := golang.CollectMigrationChanges(context.Background(), dir)

		// then
		require.NoError(t, err)
		assert.Nil(t, changes.Files)
		assert.Contains(t, changes.Skip, "is deleted or renamed")
	})

	t.Run("should leave in place the vendored files of a module no longer vendored", func(t *testing.T) {
		t.Parallel()

		// given: re-vendoring after the migration, in the root module and in a
		// nested one. The old major's files are gone from both trees, and
		// neither manifest lists it any more.
		dir := newMigrationCheckout(t, map[string]string{
			"go.mod":                                     "module example.com/app\n\ngo 1.25\n",
			"vendor/modules.txt":                         oldMajorVendorManifest,
			"vendor/github.com/example/lib/lib.go":       "package lib\n",
			"tools/go.mod":                               "module example.com/app/tools\n\ngo 1.25\n",
			"tools/vendor/modules.txt":                   oldMajorVendorManifest,
			"tools/vendor/github.com/example/lib/lib.go": "package lib\n",
		})
		for _, moduleDir := range []string{".", "tools"} {
			revendorNewMajor(t)(filepath.Join(dir, moduleDir))
		}

		// when
		changes, err := golang.CollectMigrationChanges(context.Background(), dir)

		// then
		require.NoError(t, err)
		assert.Empty(t, changes.Skip)
		assert.Equal(t, []string{
			"tools/vendor/github.com/example/lib/lib.go",
			"vendor/github.com/example/lib/lib.go",
		}, changes.LeftInPlace, "the branch keeps them, since it starts from the base that has them")
		assert.Equal(t, map[string]string{
			"tools/vendor/github.com/example/lib/v3/lib.go": "add",
			"tools/vendor/modules.txt":                      "edit",
			"vendor/github.com/example/lib/v3/lib.go":       "add",
			"vendor/modules.txt":                            "edit",
		}, changeTypesByPath(changes.Files), "only what the provider can express is pushed")
	})

	t.Run("should refuse a deletion from a module that is still vendored", func(t *testing.T) {
		t.Parallel()

		// given: the new major moved a shared dependency -- here a replaced
		// one, vendored under its own path -- to a version that lost a file.
		// Left in place, the file would be compiled with the rest of its
		// package.
		const manifest = "# github.com/example/shared v1.0.0 => github.com/example/fork v1.1.0\n" +
			"## explicit\ngithub.com/example/shared\n"
		dir := newMigrationCheckout(t, map[string]string{
			"go.mod":                                "module example.com/app\n\ngo 1.25\n",
			"vendor/modules.txt":                    manifest,
			"vendor/github.com/example/shared/a.go": "package shared\n",
			"vendor/github.com/example/shared/b.go": "package shared\n",
		})
		require.NoError(t, os.Remove(filepath.Join(dir, "vendor", "github.com", "example", "shared", "b.go")))
		writeTestFile(t, dir, "vendor/github.com/example/shared/a.go", "package shared\n\nfunc B() {}\n")

		// when
		changes, err := golang.CollectMigrationChanges(context.Background(), dir)

		// then
		require.NoError(t, err)
		assert.Nil(t, changes.Files)
		assert.Empty(t, changes.LeftInPlace)
		assert.Equal(t,
			"`vendor/github.com/example/shared/b.go` is deleted from a module that is still vendored, "+
				"which the provider API cannot push",
			changes.Skip)
	})

	t.Run("should refuse a deletion outside a vendor directory whatever else may stay", func(t *testing.T) {
		t.Parallel()

		// given: the old module's vendored files may stay, and a file that
		// sorts after them is deleted outside the vendor directory.
		dir := newMigrationCheckout(t, map[string]string{
			"go.mod":                               "module example.com/app\n\ngo 1.25\n",
			"vendor/modules.txt":                   oldMajorVendorManifest,
			"vendor/github.com/example/lib/lib.go": "package lib\n",
			"zz.go":                                "package main\n",
		})
		revendorNewMajor(t)(dir)
		require.NoError(t, os.Remove(filepath.Join(dir, "zz.go")))

		// when
		changes, err := golang.CollectMigrationChanges(context.Background(), dir)

		// then
		require.NoError(t, err)
		assert.Nil(t, changes.Files)
		assert.Empty(t, changes.LeftInPlace, "nothing is pushed, so nothing is left in place either")
		assert.Equal(t,
			"`zz.go` is deleted or renamed by the migration, which the provider API cannot push",
			changes.Skip)
	})

	t.Run("should refuse a deletion in a vendor directory that is no module's", func(t *testing.T) {
		t.Parallel()

		// given: a directory called vendor beside no go.mod, whose manifest
		// would let the deletion stay if it were read.
		dir := newMigrationCheckout(t, map[string]string{
			"go.mod":                  "module example.com/app\n\ngo 1.25\n",
			"docs/vendor/modules.txt": "# github.com/example/other v1.0.0\n",
			"docs/vendor/github.com/example/lib/lib.go": "package lib\n",
		})
		require.NoError(t, os.Remove(filepath.Join(dir, "docs", "vendor", "github.com", "example", "lib", "lib.go")))

		// when
		changes, err := golang.CollectMigrationChanges(context.Background(), dir)

		// then
		require.NoError(t, err)
		assert.Nil(t, changes.Files)
		assert.Contains(t, changes.Skip, "is deleted or renamed by the migration")
	})

	t.Run("should refuse a deletion in a vendor directory without a manifest", func(t *testing.T) {
		t.Parallel()

		// given: a vendor tree from before modules, which no manifest
		// describes, so nothing in it can be shown to be unread.
		dir := newMigrationCheckout(t, map[string]string{
			"go.mod":                               "module example.com/app\n\ngo 1.25\n",
			"vendor/github.com/example/lib/lib.go": "package lib\n",
			"vendor/github.com/example/lib/doc.go": "package lib\n",
		})
		require.NoError(t, os.Remove(filepath.Join(dir, "vendor", "github.com", "example", "lib", "doc.go")))

		// when
		changes, err := golang.CollectMigrationChanges(context.Background(), dir)

		// then
		require.NoError(t, err)
		assert.Nil(t, changes.Files)
		assert.Contains(t, changes.Skip, "is deleted or renamed by the migration")
	})

	t.Run("should say so when the migration changed nothing", func(t *testing.T) {
		t.Parallel()

		// given
		dir := newMigrationCheckout(t, map[string]string{
			"go.mod":  "module example.com/app\n\ngo 1.25\n",
			"main.go": "package main\n\nfunc main() {}\n",
		})

		// when
		changes, err := golang.CollectMigrationChanges(context.Background(), dir)

		// then
		require.NoError(t, err)
		assert.Nil(t, changes.Files)
		assert.Contains(t, changes.Skip, "changed nothing")
	})
}

func TestLeftInPlaceNote(t *testing.T) {
	t.Parallel()

	const reason = ": the provider API cannot delete files, and `go mod vendor` removes them"

	cases := []struct {
		name        string
		leftInPlace []string
		want        string
	}{
		{
			name: "should name the root module's vendor directory once",
			leftInPlace: []string{
				"vendor/github.com/example/lib/lib.go",
				"vendor/github.com/example/lib/pkg/pkg.go",
			},
			want: "the old module's vendored files are still under `vendor/`" + reason,
		},
		{
			name: "should name the vendor directory of each module",
			leftInPlace: []string{
				"tools/vendor/github.com/example/lib/lib.go",
				"vendor/github.com/example/lib/lib.go",
			},
			want: "the old module's vendored files are still under `tools/vendor/` and `vendor/`" + reason,
		},
		{
			name: "should name two and count the rest",
			leftInPlace: []string{
				"a/vendor/github.com/example/lib/lib.go", "b/vendor/github.com/example/lib/lib.go",
				"c/vendor/github.com/example/lib/lib.go", "d/vendor/github.com/example/lib/lib.go",
			},
			want: "the old module's vendored files are still under `a/vendor/`, `b/vendor/` and 2 more" + reason,
		},
		{
			name:        "should stop at the module's own vendor directory",
			leftInPlace: []string{"tools/vendor/github.com/example/vendor/lib.go"},
			want:        "the old module's vendored files are still under `tools/vendor/`" + reason,
		},
		{
			name:        "should name the directory of a file in no vendor directory",
			leftInPlace: []string{"lib/lib.go"},
			want:        "the old module's vendored files are still under `lib/`" + reason,
		},
		{
			name:        "should say nothing when nothing was left in place",
			leftInPlace: nil,
			want:        "",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// given
			leftInPlace := testCase.leftInPlace

			// when
			note := golang.LeftInPlaceNote(leftInPlace)

			// then
			assert.Equal(t, testCase.want, note)
		})
	}
}

func TestWithStandingNote(t *testing.T) {
	t.Parallel()

	t.Run("should append the standing note to a note that leaves room for it", func(t *testing.T) {
		t.Parallel()

		// given
		const note = "it does not build yet: main.go:9:2: undefined: lib.NewName"

		// when
		joined := golang.WithStandingNote(note, leftInPlaceVendorNote)

		// then
		assert.Equal(t, note+"; "+leftInPlaceVendorNote, joined)
	})

	t.Run("should cut the note before the standing one rather than the standing one", func(t *testing.T) {
		t.Parallel()

		// given: a compiler error long enough to fill a note on its own.
		note := "it does not build yet: " + strings.Repeat("github.com/example/lib/v3/internal/deep/pkg ", 10)

		// when
		joined := golang.WithStandingNote(note, leftInPlaceVendorNote)

		// then
		assert.True(t, strings.HasSuffix(joined, "...; "+leftInPlaceVendorNote),
			"the cut is marked, and the standing note follows it whole")
		assert.LessOrEqual(t, len(joined), 300)
		assert.Equal(t, joined, golang.MigrationNote(joined, ""),
			"so the cut the note gets when it is rendered takes nothing more")
	})

	t.Run("should not cut a note that fits beside the standing one exactly", func(t *testing.T) {
		t.Parallel()

		// given
		note := strings.Repeat("x", 300-len("; ")-len(leftInPlaceVendorNote))

		// when
		joined := golang.WithStandingNote(note, leftInPlaceVendorNote)

		// then
		assert.Equal(t, note+"; "+leftInPlaceVendorNote, joined)
		assert.Len(t, joined, 300)
	})

	t.Run("should be the standing note alone when nothing precedes it", func(t *testing.T) {
		t.Parallel()

		// given / when
		joined := golang.WithStandingNote("", leftInPlaceVendorNote)

		// then
		assert.Equal(t, leftInPlaceVendorNote, joined)
	})

	t.Run("should leave the note alone when there is nothing to append", func(t *testing.T) {
		t.Parallel()

		// given / when
		joined := golang.WithStandingNote("it does not build yet", "")

		// then
		assert.Equal(t, "it does not build yet", joined)
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

	t.Run("should cut a message too long for the description", func(t *testing.T) {
		t.Parallel()

		// given: a message long enough to be cut, with a rune on the boundary.
		message := strings.Repeat("é", 200)

		// when
		note := golang.MigrationNote(message, "")

		// then
		assert.LessOrEqual(t, len(note), 303, "the limit plus the ellipsis")
		assert.True(t, strings.HasSuffix(note, "..."), "the cut is marked")
		assert.Equal(t, strings.ToValidUTF8(note, ""), note, "the cut is on a rune boundary")
	})

	t.Run("should keep the package paths a go error names", func(t *testing.T) {
		t.Parallel()

		// given: the error that stopped gitforge's migration to go-git v6,
		// whose reason comes after two long package paths.
		message := "go mod tidy failed: example.com/repro imports " +
			"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability: " +
			"module github.com/go-git/go-git/v6@latest found (v6.0.0-beta.1), but does not contain package " +
			"github.com/go-git/go-git/v6/plumbing/protocol/packp/capability"

		// when
		note := golang.MigrationNote(message, "")

		// then
		assert.Contains(t, note, "but does not contain package", "the cut must not fall before the reason")
	})
}

func TestMigrationDetail(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		output string
		want   string
	}{
		{
			// What aisync's notes read: the download, not why `go get` failed.
			name: "should skip what the go command says it is doing",
			output: "go: downloading github.com/example/lib/v3 v3.0.1\n" +
				"go: github.com/example/lib/v3@v3.0.1: reading https://proxy.golang.org/github.com/example/lib/" +
				"v3/@v/v3.0.1.mod: 404 Not Found\n",
			want: "github.com/example/lib/v3@v3.0.1: reading https://proxy.golang.org/github.com/example/lib/" +
				"v3/@v/v3.0.1.mod: 404 Not Found",
		},
		{
			// What gitforge's note read: "go: finding module for package ...".
			name: "should join an error the go command wrapped over several lines",
			output: "go: finding module for package github.com/example/lib/v3/pkg\n" +
				"go: example.com/app imports\n" +
				"\tgithub.com/example/lib/v3/pkg: module github.com/example/lib/v3@latest found (v3.0.1), " +
				"but does not contain package github.com/example/lib/v3/pkg\n",
			want: "example.com/app imports github.com/example/lib/v3/pkg: module github.com/example/lib/v3@latest " +
				"found (v3.0.1), but does not contain package github.com/example/lib/v3/pkg",
		},
		{
			// What autoupdate's own migration note read: the package header.
			name: "should skip the package header a build prints above its diagnostics",
			output: "# example.com/app/internal/push\n" +
				"internal/push/push.go:12:3: undefined: lib.Push\n",
			want: "internal/push/push.go:12:3: undefined: lib.Push",
		},
		{
			name: "should keep the lines that say why a module was refused",
			output: "go: downloading github.com/example/lib/v3 v3.0.1\n" +
				"go: github.com/example/lib/v3@v3.0.1: parsing go.mod:\n" +
				"\tmodule declares its path as: example.land/lib/v3\n" +
				"\t        but was required as: github.com/example/lib/v3\n",
			want: "github.com/example/lib/v3@v3.0.1: parsing go.mod: module declares its path as: " +
				"example.land/lib/v3 but was required as: github.com/example/lib/v3",
		},
		{
			name:   "should report nothing when the output holds no error",
			output: "go: downloading github.com/example/lib/v3 v3.0.1\n\n",
			want:   "",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// given
			output := testCase.output

			// when
			detail := golang.MigrationDetail(output)

			// then
			assert.Equal(t, testCase.want, detail)
		})
	}
}

func TestMissingPackagesNote(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		missing []string
		want    string
	}{
		{
			name:    "should name the one package the new major does not have",
			missing: []string{"github.com/example/lib/v3/pkg"},
			want:    "it does not build yet: `github.com/example/lib/v3` has no package `pkg`",
		},
		{
			name:    "should name both of two",
			missing: []string{"github.com/example/lib/v3/a", "github.com/example/lib/v3/b"},
			want:    "it does not build yet: `github.com/example/lib/v3` has no packages `a` and `b`",
		},
		{
			name: "should name two and count the rest",
			missing: []string{
				"github.com/example/lib/v3/a", "github.com/example/lib/v3/b",
				"github.com/example/lib/v3/c", "github.com/example/lib/v3/d",
			},
			want: "it does not build yet: `github.com/example/lib/v3` has no packages `a`, `b` and 2 more",
		},
		{
			name:    "should spell out the module's own package in full",
			missing: []string{"github.com/example/lib/v3"},
			want:    "it does not build yet: `github.com/example/lib/v3` has no package `github.com/example/lib/v3`",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// given
			missing := testCase.missing

			// when
			note := golang.MissingPackagesNote("github.com/example/lib/v3", missing)

			// then
			assert.Equal(t, testCase.want, note)
		})
	}
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
