package golang

import (
	"context"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	logger "github.com/sirupsen/logrus"
	"golang.org/x/mod/modfile"

	"github.com/rios0rios0/autoupdate/internal/domain/entities"
	"github.com/rios0rios0/autoupdate/internal/domain/repositories"
	"github.com/rios0rios0/autoupdate/internal/infrastructure/repositories/cmdrunner"
	"github.com/rios0rios0/autoupdate/internal/support"
)

const (
	// migrationBranchPrefix names the branches a prepared migration lives on.
	//
	// The name is derived from the dependency rather than dated, and that is
	// deliberate: the notice links to the branch from a pull request
	// description, and the link has to keep working after the description is
	// read a week later. It also makes the second run of a day find the branch
	// the first one created instead of proposing another.
	migrationBranchPrefix = "chore/migrate-"

	// migrationWorktreePattern is the prefix of the directory the preparation
	// checks the base branch out into. A killed run leaves it behind, and the
	// recognisable name is what lets the temp-directory sweep remove it.
	migrationWorktreePattern = "autoupdate-migration-*"

	// migrationNoteLimit is how long a note may get before it is cut. Notes are
	// rendered into a table in a pull request description, and a compiler error
	// pasted whole would wreck the table it sits in.
	migrationNoteLimit = 160

	// replaceVerb is the directive that redirects a module somewhere other than
	// where its path says it lives.
	replaceVerb = "replace"

	minimumStatusRecordLength = 4
)

// MigrationPrepared is what the run did about one newer major: the finding
// itself, and whether a branch carrying the mechanical part of the migration
// now exists.
//
// It is a separate type from MajorAvailable rather than fields hung on it,
// because the two answer different questions and travel to different places:
// MajorAvailable is a fact the probe reported, and this is an action the run
// took about it, which only the caller that can reach the provider API can
// produce.
type MigrationPrepared struct {
	MajorAvailable

	// Prepared is true when a branch carrying the migration exists: created by
	// this run, or already there from an earlier one.
	Prepared bool
	// Compiles is whether this run built the prepared tree. It is only
	// meaningful with Prepared: a branch that already existed was never built
	// here, and it reports false rather than claiming something untested.
	Compiles bool
	// Branch is the branch the migration is on, empty when none was prepared.
	Branch string
	// URL opens the provider's "create a pull request" page for the branch. It
	// is empty when the provider has no such page, which the description
	// renders as a dash rather than a link that would have to be guessed.
	URL string
	// Note explains a migration that was not prepared, and records what a
	// reader should know about one that was (that it does not compile yet, for
	// instance).
	Note string
}

// unpreparedMigrations pairs the findings with the fact that nothing was done
// about them, which is what a run that skips the preparation -- or a caller
// that cannot reach the provider API -- has to report.
func unpreparedMigrations(majors []MajorAvailable) []MigrationPrepared {
	results := make([]MigrationPrepared, 0, len(majors))
	for _, major := range majors {
		results = append(results, MigrationPrepared{MajorAvailable: major})
	}

	return results
}

// migrationParams is everything preparing the migrations needs: where the
// checkout is, which branch the branches are proposed against, and how to reach
// the provider that will hold them.
type migrationParams struct {
	// RepoDir is the run's checkout. The preparation does not touch it: it adds
	// a worktree of the base branch and works there, so the upgrade branch
	// being assembled stays exactly as the updaters left it.
	RepoDir string
	// ToolingDir is where the worktree, the git credentials and the Go caches
	// live during the preparation. Empty leaves them on the process
	// environment, which is what standalone mode wants.
	ToolingDir string
	// BaseBranch is the short name of the branch the migration is proposed
	// against -- the one the pull request that carries the notice targets.
	BaseBranch   string
	ProviderName string
	Provider     repositories.ProviderRepository
	Repo         entities.Repository
	AuthToken    string
	GoBinary     string
	HasConfigSH  bool
	Majors       []MajorAvailable
	// DryRun reports the findings without preparing anything, which is what a
	// rehearsal owes: the notice is part of what the run would produce, and a
	// branch on the remote is not a rehearsal.
	DryRun bool
	// Runner runs the go commands. Injected rather than reached for, so the
	// preparation is testable without a toolchain on the machine.
	Runner cmdrunner.Runner
}

// prepareMigrationsForRun is the decision the preparation hangs off: a run that
// declined it, or that is only rehearsing, reports the findings without acting
// on them.
func prepareMigrationsForRun(ctx context.Context, params migrationParams, enabled bool) []MigrationPrepared {
	if !enabled {
		logger.Debugf(
			"[golang] prepare_major_migrations is off; the newer majors are reported without a migration",
		)
		return unpreparedMigrations(params.Majors)
	}

	return prepareMajorMigrations(ctx, params)
}

// prepareMajorMigrations prepares one branch per newer major, and never fails:
// a migration that could not be prepared is reported on its own row, because
// the notice is a service to the reader and must not take a run down with it.
func prepareMajorMigrations(ctx context.Context, params migrationParams) []MigrationPrepared {
	if len(params.Majors) == 0 {
		return nil
	}
	if params.DryRun {
		return unpreparedMigrations(params.Majors)
	}
	if params.Provider == nil {
		logger.Warnf(
			"[golang] No provider API is available; the newer majors are reported without a migration",
		)
		return unpreparedMigrations(params.Majors)
	}
	if params.Runner == nil {
		logger.Warnf("[golang] No Go command runner is available; the newer majors are reported without a migration")
		return unpreparedMigrations(params.Majors)
	}

	logger.Infof(
		"[golang] Preparing a migration branch for %d newer major(s)", len(params.Majors),
	)

	configPath, cleanupConfig := migrationGitConfig(params)
	defer cleanupConfig()

	env := append(
		support.ScriptEnv(params.ToolingDir),
		support.GitAuthEnv(configPath, params.AuthToken)...,
	)
	webURL := support.RepositoryWebURL(params.Repo, params.ProviderName)

	results := make([]MigrationPrepared, 0, len(params.Majors))
	for _, major := range params.Majors {
		results = append(results, prepareOneMigration(ctx, params, major, webURL, env))
	}

	return results
}

// prepareOneMigration prepares the branch for one newer major, or says why it
// could not.
func prepareOneMigration(
	ctx context.Context,
	params migrationParams,
	major MajorAvailable,
	webURL string,
	env []string,
) MigrationPrepared {
	//nolint:exhaustruct // the fields are filled in as the preparation gets there
	prepared := MigrationPrepared{MajorAvailable: major}
	prepared.Branch = migrationBranchName(major)
	prepared.URL = support.PullRequestCreationURL(
		params.ProviderName, webURL, params.BaseBranch, prepared.Branch,
	)

	// Checked before anything is prepared, so a run that repeats an earlier
	// one costs a lookup instead of a clone, a rewrite and a push.
	if support.PullRequestOpen(ctx, params.Provider, params.Repo, updaterName, prepared.Branch) {
		prepared.Prepared = true
		prepared.Note = "a pull request is already open for the prepared branch"
		return prepared
	}

	worktree, removeWorktree, err := addMigrationWorktree(ctx, params)
	if err != nil {
		prepared.Note = migrationNote(err.Error(), params.AuthToken)
		return prepared
	}
	defer removeWorktree()

	build, err := prepareMigrationInWorktree(ctx, params, major, worktree, env)
	if err != nil {
		prepared.Note = migrationNote(err.Error(), params.AuthToken)
		return prepared
	}
	if build.Skip != "" {
		prepared.Note = migrationNote(build.Skip, params.AuthToken)
		return prepared
	}

	changes, skipReason, err := collectMigrationChanges(ctx, worktree)
	if err != nil {
		prepared.Note = migrationNote(err.Error(), params.AuthToken)
		return prepared
	}
	if skipReason != "" {
		prepared.Note = migrationNote(skipReason, params.AuthToken)
		return prepared
	}

	err = params.Provider.CreateBranchWithChanges(ctx, params.Repo, entities.BranchInput{
		BranchName:    prepared.Branch,
		BaseBranch:    refsHeads(params.BaseBranch),
		Changes:       changes,
		CommitMessage: migrationCommitMessage(major, build),
	})
	switch {
	case err == nil:
		prepared.Prepared = true
		prepared.Compiles = build.Compiles
		prepared.Note = migrationNote(build.Note, params.AuthToken)
		logger.Infof(
			"[golang] Prepared the migration of %s to %s (%s) on branch %s",
			major.Path, major.NextPath, major.NextVersion, prepared.Branch,
		)
	case migrationBranchExists(err):
		// The provider refused to create a branch that is already there. The
		// migration exists; this run just did not make it, and saying it
		// compiles would be claiming a build it never ran.
		prepared.Prepared = true
		prepared.Note = "the branch already existed, so this run did not verify it"
		logger.Infof(
			"[golang] The migration branch %s already exists; it was left as it is",
			prepared.Branch,
		)
	default:
		prepared.Note = migrationNote(err.Error(), params.AuthToken)
		logger.Warnf(
			"[golang] Could not prepare the migration of %s to %s: %v",
			major.Path, major.NextPath, err,
		)
	}

	return prepared
}

// migrationGitConfig writes the git configuration the go commands of every
// migration in the run authenticate through, and returns its path together
// with the function that removes it. Without a token there is nothing to write
// and nothing to remove.
//
// The token reaches the commands through a file rather than through the
// operator's own gitconfig, exactly as the upgrade scripts do it: the file
// starts as a copy of that gitconfig, so the settings in it stay in force, and
// the run's rewrites are appended to the copy.
func migrationGitConfig(params migrationParams) (string, func()) {
	scratchDir := params.ToolingDir
	if scratchDir == "" {
		scratchDir = os.TempDir()
	}

	path, cleanup, err := support.FileGitAuthConfig(scratchDir, params.ProviderName, params.AuthToken)
	if err != nil {
		logger.Warnf(
			"[golang] Could not write the git credentials for the migration preparation: %v", err,
		)
		return "", func() {}
	}

	return path, cleanup
}

// --- the worktree ---

// addMigrationWorktree checks the base branch out into a directory of its own.
//
// A worktree rather than a clone of the run's checkout: the migration is built
// from the branch the pull request targets, not from the upgrade branch the run
// has been assembling, and the two share one object store, so the checkout is
// local, cheap, and cannot disturb what the updaters wrote.
func addMigrationWorktree(
	ctx context.Context, params migrationParams,
) (string, func(), error) {
	scratchDir := params.ToolingDir
	if scratchDir == "" {
		scratchDir = os.TempDir()
	}

	dir, err := os.MkdirTemp(scratchDir, migrationWorktreePattern)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create the worktree directory: %w", err)
	}
	// git worktree add creates the directory itself, and a path that already
	// holds files is the one case it refuses.
	_ = os.Remove(dir)

	remove := func() { removeMigrationWorktree(ctx, params.RepoDir, dir) }

	var lastErr error
	// Prefer the remote-tracking ref: the local branch can be stale while the
	// provider's CreateBranchWithChanges call always uses the remote base. The
	// local ref is only a fallback for standalone repositories without a remote
	// tracking branch.
	for _, ref := range []string{
		"refs/remotes/origin/" + params.BaseBranch,
		"refs/heads/" + params.BaseBranch,
	} {
		output, addErr := support.GitCommand(
			ctx, params.RepoDir, "worktree", "add", "--detach", dir, ref,
		).CombinedOutput()
		if addErr == nil {
			return dir, remove, nil
		}
		lastErr = fmt.Errorf(
			"failed to check %s out into a worktree: %w: %s",
			ref, addErr, strings.TrimSpace(string(output)),
		)
	}

	_ = os.Remove(dir)
	return "", nil, lastErr
}

// removeMigrationWorktree deletes the worktree and, when git will not, the
// directory and the metadata it left behind.
//
// The worktree is dirty by construction -- that is what it was for -- so the
// removal is forced. It also runs on a context that ignores the run's own
// cancellation: a cancelled run still has to clean up after itself, and the
// work is bounded by the command rather than by the run that started it.
func removeMigrationWorktree(ctx context.Context, repoDir, dir string) {
	cleanupCtx := context.WithoutCancel(ctx)

	output, err := support.GitCommand(
		cleanupCtx, repoDir, "worktree", "remove", "--force", dir,
	).CombinedOutput()
	if err == nil {
		return
	}

	logger.Warnf(
		"[golang] Could not remove the migration worktree %s: %v: %s",
		dir, err, strings.TrimSpace(string(output)),
	)

	if removeErr := support.RemoveTree(dir); removeErr != nil {
		logger.Warnf("[golang] Could not remove the migration worktree directory %s: %v", dir, removeErr)
		return
	}

	pruneOutput, pruneErr := support.GitCommand(cleanupCtx, repoDir, "worktree", "prune").CombinedOutput()
	if pruneErr != nil {
		logger.Warnf(
			"[golang] Could not prune the worktree metadata of %s: %v: %s",
			dir, pruneErr, strings.TrimSpace(string(pruneOutput)),
		)
	}
}

// --- what the migration consists of ---

// migrationBuild is what building the migration in the worktree produced.
type migrationBuild struct {
	// Skip explains why nothing should be pushed, empty when there is.
	Skip string
	// Compiles is whether the tree built in every module the migration touched.
	Compiles bool
	// Note is what a reader of the pull request should know about it.
	Note string
}

// prepareMigrationInWorktree performs the mechanical half of a major-version
// migration on the worktree: every import of the module is rewritten to the
// path the new major is published under, and every module that requires it is
// moved onto that path, tidied and -- where the repository vendors its
// dependencies -- re-vendored.
//
// The half it deliberately leaves out is the API changes, which are not
// mechanical and which no bot should be guessing at. The branch is worth having
// anyway: rewriting the imports and the module graph by hand is the part
// everybody puts off.
func prepareMigrationInWorktree(
	ctx context.Context,
	params migrationParams,
	major MajorAvailable,
	worktree string,
	env []string,
) (migrationBuild, error) {
	//nolint:exhaustruct // Compiles stays false until a build says otherwise
	build := migrationBuild{}

	if replacedIn := replacedModuleDir(worktree, major.Path); replacedIn != "" {
		build.Skip = fmt.Sprintf(
			"`%s` redirects it elsewhere, so the new major is not a drop-in replacement",
			goModPathFor(replacedIn),
		)
		return build, nil
	}

	rewritten, unparsed, err := rewriteModuleImports(worktree, major.Path, major.NextPath)
	if err != nil {
		return build, err
	}
	if len(unparsed) > 0 {
		build.Note = fmt.Sprintf(
			"%d file(s) could not be parsed and kept their old imports", len(unparsed),
		)
	}

	runner := &migrationRunner{
		ctx:      ctx,
		env:      env,
		goBinary: params.GoBinary,
		token:    params.AuthToken,
		runner:   params.Runner,
	}
	if params.HasConfigSH {
		// Sourced by every command rather than once, because the variables it
		// sets (GOPRIVATE among them) are what make the private fetches work.
		runner.configSH = filepath.Join(worktree, "config.sh")
	}
	if runner.runner == nil {
		return build, fmt.Errorf("go command runner is not configured")
	}

	touched, err := bumpMigrationModules(runner, worktree, major, rewritten)
	if err != nil {
		return build, err
	}
	if len(touched) == 0 {
		build.Skip = "no module in this repository requires or imports it"
		return build, nil
	}

	build.Compiles = true
	for _, dir := range touched {
		if err = runner.run(filepath.Join(worktree, filepath.FromSlash(dir)), "vet", "./..."); err != nil {
			// One module that does not build is the answer: the branch needs
			// work before it can merge. Not an error -- a migration branch that
			// still needs its API changes applied is the normal case.
			build.Compiles = false
			build.Note = joinNotes(build.Note, "it does not build yet: "+migrationDetail(err.Error()))
			break
		}
	}

	support.LocalChangelogUpdate(worktree, []string{migrationChangelogEntry(major)})

	return build, nil
}

// bumpMigrationModules moves every module that has something to do with the
// dependency onto the new path: `go get` for the requirement, `go mod tidy` for
// everything that follows from it, and a re-vendor where the repository keeps
// its dependencies in the tree.
//
// It returns the directories it touched -- the modules that require the
// dependency, and the modules whose imports it rewrote -- because those are
// exactly the modules the build check has to cover afterwards.
func bumpMigrationModules(
	runner *migrationRunner,
	worktree string,
	major MajorAvailable,
	rewritten []string,
) ([]string, error) {
	var touched []string

	for _, dir := range discoverLocalModuleDirs(worktree) {
		moduleDir := filepath.Join(worktree, filepath.FromSlash(dir))

		requires, err := moduleRequires(worktree, dir, major.Path)
		if err != nil {
			// A go.mod this program cannot read is a warning rather than a
			// stop: a module unrelated to the dependency must not cost the
			// migration that every other module can have.
			logger.Warnf("[golang] Skipping the module in %s while preparing the migration: %v", dir, err)
			continue
		}
		if !requires && !moduleContains(worktree, dir, rewritten) {
			continue
		}

		if err = runner.run(moduleDir, "get", major.NextPath+"@"+major.NextVersion); err != nil {
			return nil, err
		}
		if err = runner.run(moduleDir, "mod", "tidy"); err != nil {
			return nil, err
		}
		if isDirectory(filepath.Join(moduleDir, "vendor")) {
			if err = runner.run(moduleDir, "mod", "vendor"); err != nil {
				return nil, err
			}
		}

		touched = append(touched, dir)
	}

	return touched, nil
}

// moduleRequires reports whether the module in dir declares the given module
// path as a requirement -- indirect ones included, since go.mod is what
// declares them.
func moduleRequires(worktree, dir, modulePath string) (bool, error) {
	file, err := readWorktreeGoMod(worktree, dir)
	if err != nil {
		return false, err
	}

	for _, requirement := range file.Require {
		if requirement.Mod.Path == modulePath {
			return true, nil
		}
	}

	return false, nil
}

// replacedModuleDir returns the module directory whose go.mod replaces the
// given module path, or nothing.
//
// A replacement is what a repository writes when it needs the module to come
// from somewhere else: a fork, a local checkout, a patched version. Rewriting
// its imports to the new major would point the build at the published module
// the repository deliberately does not use, so the migration is declined rather
// than guessed at.
//
// The directives are read from the manifest's syntax rather than from its
// parsed form, and that is not an optimisation: [modfile.ParseLax] is *defined*
// to drop them. It parses a file as a dependency's manifest, and a main
// module's own statements -- `replace` among them -- are exactly what it
// ignores. Reading the tokens also keeps the check working on a manifest
// carrying a directive this build's Go does not know, which is exactly the
// manifest whose author is most likely to have redirected a module.
func replacedModuleDir(worktree, modulePath string) string {
	for _, dir := range discoverLocalModuleDirs(worktree) {
		file, err := readWorktreeGoMod(worktree, dir)
		if err != nil {
			continue
		}
		if replacesModule(file.Syntax, modulePath) {
			return dir
		}
	}

	return ""
}

// replacesModule reports whether a go.mod's syntax redirects the given module
// path, whether the directive sits on a line of its own or inside a
// `replace (...)` block. A pinned old path (`replace <path> v1.2.3 => ...`) is
// the same replacement.
func replacesModule(syntax *modfile.FileSyntax, modulePath string) bool {
	for _, statement := range syntax.Stmt {
		switch typed := statement.(type) {
		case *modfile.Line:
			if oldModulePath(typed.Token) == modulePath {
				return true
			}
		case *modfile.LineBlock:
			if len(typed.Token) != 1 || typed.Token[0] != replaceVerb {
				continue
			}
			for _, line := range typed.Line {
				if len(line.Token) > 0 && line.Token[0] == modulePath {
					return true
				}
			}
		}
	}

	return false
}

// oldModulePath returns the module path a `replace` line redirects, or nothing
// when the line is some other statement.
func oldModulePath(tokens []string) string {
	if len(tokens) < 2 || tokens[0] != replaceVerb {
		return ""
	}

	return tokens[1]
}

// readWorktreeGoMod reads and parses a module manifest of the worktree.
//
// The parse is lax on purpose: this reads a manifest in order to act on what
// the module *declares* it needs, and a directive this build's Go does not know
// must not stop it. The lax parser drops the main-module-only statements along
// with the unknown ones, which is why anything reading those -- `replace`, in
// [replacedModuleDir] -- reads the manifest's syntax instead.
func readWorktreeGoMod(worktree, dir string) (*modfile.File, error) {
	goModPath := goModPathFor(dir)

	data, err := os.ReadFile(filepath.Join(worktree, filepath.FromSlash(goModPath)))
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", goModPath, err)
	}

	file, err := modfile.ParseLax(goModPath, data, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", goModPath, err)
	}

	return file, nil
}

// moduleContains reports whether any of the given repo-relative paths lives in
// the module directory.
func moduleContains(worktree, dir string, paths []string) bool {
	moduleDirs := discoverLocalModuleDirs(worktree)

	for _, filePath := range paths {
		if owningModuleDir(worktree, filePath, moduleDirs) == dir {
			return true
		}
	}

	return false
}

// owningModuleDir returns the nearest enclosing module for a repository file.
// Nested modules own their files rather than the root module, so a migration
// does not run go get/tidy/vet in unrelated parent modules.
func owningModuleDir(worktree, filePath string, moduleDirs []string) string {
	absFile := filepath.Clean(filepath.Join(worktree, filepath.FromSlash(filePath)))
	owner := ""
	longest := -1

	for _, moduleDir := range moduleDirs {
		absModule := filepath.Clean(filepath.Join(worktree, filepath.FromSlash(moduleDir)))
		prefix := absModule + string(filepath.Separator)
		if strings.HasPrefix(absFile, prefix) && len(absModule) > longest {
			owner = moduleDir
			longest = len(absModule)
		}
	}

	return owner
}

// --- the import rewrite ---

// rewriteModuleImports rewrites every import of the module -- and of a package
// under it -- to the path the new major is published under.
//
// Each import literal is replaced at its own offsets, in a file parsed for its
// imports alone, so nothing else in the file can move: a repository with
// generated code, build tags or unusual formatting keeps it. The file is
// formatted afterwards, because a longer path can shift the alignment of the
// block it sits in.
//
// Files that do not parse are left exactly as they are and reported: a file
// with a syntax error does not compile today either, and it is most likely a
// template or a fixture that a rewrite would damage.
func rewriteModuleImports(root, oldPath, newPath string) ([]string, []string, error) {
	files, err := support.WalkFilesByExtension(root, ".go")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list the Go files: %w", err)
	}

	protectedModules := discoverRequiredModulePaths(root)
	var rewritten, unparsed []string
	for _, filePath := range files {
		// The walk skips the vendored and cached trees; this is what keeps the
		// fixtures and the hidden directories out of it as well, so a rewrite
		// never lands in a tree the repository does not author.
		if isSkippedModulePath(filePath) {
			continue
		}

		changed, parseFailed, rewriteErr := rewriteFileImports(
			filepath.Join(root, filepath.FromSlash(filePath)), oldPath, newPath, protectedModules...,
		)
		if rewriteErr != nil {
			logger.Warnf("[golang] Leaving %s alone while preparing the migration: %v", filePath, rewriteErr)
			continue
		}
		switch {
		case parseFailed:
			unparsed = append(unparsed, filePath)
		case changed:
			rewritten = append(rewritten, filePath)
		}
	}

	return rewritten, unparsed, nil
}

// discoverRequiredModulePaths returns module paths declared by every go.mod in
// the repository. A separately required module such as oldPath/v2 must not be
// treated as a package below oldPath while migrating oldPath to /v3.
func discoverRequiredModulePaths(root string) []string {
	seen := make(map[string]struct{})
	for _, dir := range discoverLocalModuleDirs(root) {
		file, err := readWorktreeGoMod(root, dir)
		if err != nil {
			continue
		}
		if file.Module != nil {
			seen[file.Module.Mod.Path] = struct{}{}
		}
		for _, requirement := range file.Require {
			seen[requirement.Mod.Path] = struct{}{}
		}
	}

	paths := make([]string, 0, len(seen))
	for modulePath := range seen {
		paths = append(paths, modulePath)
	}
	slices.Sort(paths)
	return paths
}

// rewriteFileImports rewrites the imports of one file in place, and reports
// whether the file was changed and whether it could be parsed at all.
func rewriteFileImports(filePath, oldPath, newPath string, protectedModules ...string) (bool, bool, error) {
	source, err := os.ReadFile(filePath)
	if err != nil {
		return false, false, fmt.Errorf("failed to read the file: %w", err)
	}

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, filePath, source, parser.ImportsOnly)
	if err != nil {
		return false, true, nil //nolint:nilerr // parse failure is an expected result, not an operation error
	}

	var edits []sourceEdit
	for _, imported := range file.Imports {
		importPath, unquoteErr := strconv.Unquote(imported.Path.Value)
		if unquoteErr != nil {
			continue
		}

		migrated, changed := migrateImportPathExcept(importPath, oldPath, newPath, protectedModules)
		if !changed {
			continue
		}
		edits = append(edits, sourceEdit{
			start: fileSet.Position(imported.Path.Pos()).Offset,
			end:   fileSet.Position(imported.Path.End()).Offset,
			text:  strconv.Quote(migrated),
		})
	}
	if len(edits) == 0 {
		return false, false, nil
	}

	updated := applyEdits(source, edits)
	if formatted, formatErr := format.Source(updated); formatErr == nil {
		updated = formatted
	}

	info, statErr := os.Stat(filePath)
	if statErr != nil {
		return false, false, fmt.Errorf("failed to read the file's mode: %w", statErr)
	}
	//nolint:gosec // filePath was found by the repository-rooted walk
	if writeErr := os.WriteFile(
		filePath,
		updated,
		info.Mode().Perm(),
	); writeErr != nil {
		return false, false, fmt.Errorf("failed to write the file: %w", writeErr)
	}

	return true, false, nil
}

func migrateImportPathExcept(importPath, oldPath, newPath string, protectedModules []string) (string, bool) {
	for _, modulePath := range protectedModules {
		if modulePath != oldPath && (importPath == modulePath || strings.HasPrefix(importPath, modulePath+"/")) {
			return "", false
		}
	}

	// A version-qualified module is a sibling module, not a package below an
	// unsuffixed module. Keep this guard for direct file-rewrite callers that do
	// not have a repository-wide go.mod set available.
	if suffix, found := strings.CutPrefix(importPath, oldPath+"/"); found {
		first, _, _ := strings.Cut(suffix, "/")
		if len(first) > 1 && first[0] == 'v' {
			if _, err := strconv.Atoi(first[1:]); err == nil {
				return "", false
			}
		}
	}

	return migrateImportPath(importPath, oldPath, newPath)
}

// migrateImportPath moves one import path onto the new major, subpackages
// included, and reports whether it had to.
func migrateImportPath(importPath, oldPath, newPath string) (string, bool) {
	// An import that already names the new major is left where it is: without
	// this, a file migrated by hand -- or an import that names the new
	// major's own subpackage -- would be rewritten into the new path twice.
	if importPath == newPath || strings.HasPrefix(importPath, newPath+"/") {
		return "", false
	}
	if importPath == oldPath {
		return newPath, true
	}
	if suffix, found := strings.CutPrefix(importPath, oldPath+"/"); found {
		return newPath + "/" + suffix, true
	}

	return "", false
}

// sourceEdit is one replacement of a byte range of a file.
type sourceEdit struct {
	start, end int
	text       string
}

// applyEdits rewrites the source at the given offsets. The edits are applied
// from the end backwards so that an earlier one cannot shift a later offset.
func applyEdits(source []byte, edits []sourceEdit) []byte {
	ordered := slices.Clone(edits)
	slices.SortFunc(ordered, func(a, b sourceEdit) int { return a.start - b.start })

	updated := source
	for _, edit := range slices.Backward(ordered) {
		if edit.start < 0 || edit.end > len(updated) || edit.start > edit.end {
			continue
		}
		replacement := make([]byte, 0, len(updated)-edit.end+edit.start+len(edit.text))
		replacement = append(replacement, updated[:edit.start]...)
		replacement = append(replacement, edit.text...)
		replacement = append(replacement, updated[edit.end:]...)
		updated = replacement
	}

	return updated
}

// --- running the go commands ---

// migrationRunner runs the go commands of one migration preparation.
type migrationRunner struct {
	ctx      context.Context
	env      []string
	goBinary string
	// configSH is the absolute path of the repository's config.sh, empty when
	// the repository has none.
	configSH string
	token    string
	runner   cmdrunner.Runner
}

// run executes one go command in dir.
//
// When the repository has a config.sh, every command is a shell that sources
// it first: that file is where a private module's GOPRIVATE and its proxies are
// set, and `go get` on a private module does not authenticate without it.
func (r *migrationRunner) run(dir string, args ...string) error {
	name, fullArgs := r.goBinary, args
	if r.configSH != "" {
		script := "source " + shellQuote(r.configSH) + " >/dev/null 2>&1; exec \"$@\""
		fullArgs = append([]string{"-c", script, "bash", name}, args...)
		name = "bash"
	}

	result, err := r.runner.Run(r.ctx, name, fullArgs, cmdrunner.RunOptions{Dir: dir, Env: r.env})

	output := ""
	if result != nil {
		output = result.Output
	}
	if err != nil {
		// The first line of the output is the reason; the rest is usually the
		// same error repeated per package. It goes into a note a reader sees,
		// so the token is scrubbed out of it first.
		detail := migrationDetail(support.RedactTokens(output, r.token))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("go %s failed: %s", strings.Join(args, " "), detail)
	}

	return nil
}

// --- collecting the result ---

// collectMigrationChanges reads the worktree's changes into the file changes
// the provider commits on the run's behalf.
//
// Deletions and renames are refused rather than approximated. The provider
// commit APIs express a set of added and edited files, and a migration that
// deleted one would be pushed with it still there. That happens for real --
// re-vendoring replaces a vendored tree wholesale -- so it is checked for and
// reported, not worked around.
func collectMigrationChanges(
	ctx context.Context, worktree string,
) ([]entities.FileChange, string, error) {
	output, err := support.GitCommand(ctx, worktree, "status", "--porcelain", "-z", "-uall").Output()
	if err != nil {
		return nil, "", fmt.Errorf("failed to read the changes of the migration: %w", err)
	}

	changes := make([]entities.FileChange, 0)
	for record := range strings.SplitSeq(string(output), "\x00") {
		// Two status characters, a space, and a path: anything shorter is the
		// trailing empty record of the NUL-terminated output.
		if len(record) < minimumStatusRecordLength {
			continue
		}

		status, name := record[:2], record[3:]
		if strings.ContainsAny(status, "DRC") {
			return nil, fmt.Sprintf(
				"`%s` is deleted or renamed by the migration, which the provider API cannot push",
				name,
			), nil
		}

		content, readErr := os.ReadFile(filepath.Join(worktree, filepath.FromSlash(name)))
		if readErr != nil {
			return nil, "", fmt.Errorf("failed to read %s: %w", name, readErr)
		}

		changeType := "edit"
		if status == "??" {
			changeType = "add"
		}
		changes = append(changes, entities.FileChange{
			Path: name, Content: string(content), ChangeType: changeType,
		})
	}

	if len(changes) == 0 {
		return nil, "the migration changed nothing", nil
	}

	return changes, "", nil
}

// --- the branch ---

// migrationBranchName derives the branch a migration lives on from the
// dependency it moves: `chore/migrate-<module>-v<N>`. It is deterministic, so
// the same finding produces the same branch on every run, and a second run
// links to the work of the first rather than proposing a duplicate.
func migrationBranchName(major MajorAvailable) string {
	majorNumber := strings.TrimPrefix(pathMajorSuffix(major.NextPath), "/v")
	if majorNumber == "" {
		majorNumber = versionMajorNumber(major.NextVersion)
	}
	if majorNumber == "" {
		majorNumber = "next"
	}

	return migrationBranchPrefix + migrationSlug(major.NextPath) + "-v" + majorNumber
}

// versionMajorNumber returns the leading number of a semantic version: "3" for
// "v3.0.1", and nothing when there is not one.
func versionMajorNumber(version string) string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(version), "v")
	trimmed = strings.TrimPrefix(trimmed, "V")

	major, _, _ := strings.Cut(trimmed, ".")
	if major == "" {
		return ""
	}
	if _, err := strconv.Atoi(major); err != nil {
		return ""
	}

	return major
}

// pathMajorSuffix returns the "/vN" a module path ends with, or nothing.
func pathMajorSuffix(modulePath string) string {
	_, after, ok := strings.CutLast(modulePath, "/")
	if !ok {
		return ""
	}

	suffix := after
	if len(suffix) < 2 || suffix[0] != 'v' {
		return ""
	}
	if _, err := strconv.Atoi(suffix[1:]); err != nil {
		return ""
	}

	return "/" + suffix
}

// migrationSlug reduces a module path to the piece of a branch name that names
// it. The host goes, because every dependency of one repository tends to share
// one, and the version goes with it -- both are elsewhere in the branch name,
// and a branch is read at a glance.
func migrationSlug(modulePath string) string {
	base := strings.TrimSuffix(modulePath, pathMajorSuffix(modulePath))
	if first, rest, found := strings.Cut(base, "/"); found && strings.Contains(first, ".") {
		base = rest
	}

	var sb strings.Builder
	dashed := true // also swallows a leading separator
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			sb.WriteRune(r)
			dashed = false
		default:
			if !dashed {
				sb.WriteRune('-')
				dashed = true
			}
		}
	}

	slug := strings.Trim(sb.String(), "-.")
	if slug == "" {
		return "module"
	}

	return slug
}

// migrationBranchExists reports whether the provider refused to create the
// branch because it is already there. The wording differs per provider -- a
// GitLab branch conflicts, a GitHub ref already exists, an Azure DevOps ref
// does -- so what they have in common is what is matched.
func migrationBranchExists(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "already exist") ||
		strings.Contains(message, "already exists") ||
		strings.Contains(message, "reference update failed")
}

// migrationCommitMessage is the commit the migration is proposed with, which is
// also the pull request title and body the provider prefills from it.
func migrationCommitMessage(major MajorAvailable, build migrationBuild) string {
	var sb strings.Builder

	sb.WriteString("chore(deps): prepare the migration of " + major.Path + " to " + major.NextPath + "\n\n")
	sb.WriteString(
		"This dependency was left at " + major.Path + " " + major.CurrentVersion +
			" by the upgrade: Go publishes " + major.NextVersion +
			" under a different module path, where `go get -u` cannot reach it.\n\n",
	)
	sb.WriteString("What this branch does:\n\n")
	sb.WriteString("- rewrites every import of `" + major.Path + "` to `" + major.NextPath + "`\n")
	sb.WriteString(
		"- moves every module that required it onto `" + major.NextPath + " " +
			major.NextVersion + "`, then tidies it\n",
	)
	sb.WriteString("\n")
	if build.Compiles {
		sb.WriteString("It builds as it is, but the new major's API changes are not applied:\n")
		sb.WriteString("review them and finish the migration before merging.\n")
	} else {
		sb.WriteString("It does not build yet. The remaining work is the new major's API changes,\n")
		sb.WriteString("which are a decision rather than an edit this tool should be making.\n")
	}
	if build.Note != "" {
		sb.WriteString("\n" + migrationNote(build.Note, "") + "\n")
	}

	return sb.String()
}

// migrationChangelogEntry is what the migration records in the repository's own
// changelog, in the same shape the upgrade's entries have.
func migrationChangelogEntry(major MajorAvailable) string {
	return fmt.Sprintf(
		"- changed the imports and the module requirement of `%s` to `%s`",
		major.Path, major.NextPath,
	)
}

// --- small helpers ---

// refsHeads makes a branch name into the ref a base branch is named by,
// leaving one that already is one alone.
func refsHeads(branch string) string {
	if strings.HasPrefix(branch, "refs/") {
		return branch
	}
	return "refs/heads/" + branch
}

// baseBranchName reduces a target ref to the short branch name a migration is
// proposed against.
func baseBranchName(targetRef string) string {
	return strings.TrimPrefix(strings.TrimSpace(targetRef), "refs/heads/")
}

// isDirectory reports whether the path is an existing directory.
func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// migrationNote renders a message for the pull request description: one line,
// redacted, and short enough for a table cell.
func migrationNote(message, token string) string {
	if message == "" {
		return ""
	}

	return truncateNote(support.RedactTokens(collapseWhitespace(message), token))
}

// migrationDetail reduces a command's output to the first meaningful line,
// which is where a toolchain puts the reason it failed.
func migrationDetail(output string) string {
	for line := range strings.SplitSeq(output, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return collapseWhitespace(trimmed)
		}
	}

	return ""
}

// joinNotes appends a note to another, so a row that has several things to say
// says all of them.
func joinNotes(notes ...string) string {
	var kept []string
	for _, note := range notes {
		if strings.TrimSpace(note) != "" {
			kept = append(kept, strings.TrimSpace(note))
		}
	}

	return strings.Join(kept, "; ")
}

// collapseWhitespace puts a message on one line, so a message from a toolchain
// cannot break the markdown it is rendered into.
func collapseWhitespace(message string) string {
	return strings.Join(strings.Fields(message), " ")
}

// truncateNote cuts a note at the limit, marking what it cut.
func truncateNote(note string) string {
	if len(note) <= migrationNoteLimit {
		return note
	}

	// Cut on a rune boundary, since the note is rendered rather than parsed.
	cut := migrationNoteLimit
	for cut > 0 && !utf8.RuneStart(note[cut]) {
		cut--
	}

	return strings.TrimSpace(note[:cut]) + "..."
}

// shellQuote quotes a value for bash, single quotes included.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
