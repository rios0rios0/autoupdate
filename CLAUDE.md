# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Does

AutoUpdate is a self-hosted Dependabot alternative. It discovers repositories across Git providers (GitHub, GitLab, Azure DevOps), detects outdated dependencies, and creates Pull Requests with version upgrades. Supports Terraform, Go, Python, JavaScript, Dart/Flutter, Ruby, Java, C#, Dockerfile, and CI/CD Pipeline ecosystems.

Three modes: **local** (`autoupdate [path]`) updates a single repo, **batch** (`autoupdate run`) reads a config file and processes multiple repos/providers, **self-update** (`autoupdate self-update`) downloads the latest release. A `version` command prints the current build version.

## Build and Test Commands

```bash
make build                    # Build binary to bin/autoupdate (~35s first time, <1s cached)
make debug                    # Debug build (no optimizations)
make install                  # Build and copy to ~/.local/bin/autoupdate
make run                      # Run via go run
make lint                     # Lint via pipelines scripts
make test                     # Test via pipelines scripts
make sast                     # Security scanning via pipelines scripts

go test ./...                 # Run all unit tests
go test -run TestFunctionName ./internal/domain/commands/  # Run a single test
go fmt ./...                  # Format code
go vet ./...                  # Static analysis
go mod tidy                   # Clean up dependencies
```

Unit tests carry **no build tag**. `//go:build unit` only hid them from plain
`go test ./...` and from IDE runs while buying nothing, because an untagged file compiles
under a `-tags` build too — the shared pipeline still runs every one of them. Reserve
`//go:build integration` for tests that need real infrastructure; this repository has none
today.

A test that cannot run in parallel says so in a comment above it, and the reason is usually
`t.Setenv` or `t.Chdir` — which Go refuses anywhere under a parallel ancestor.

## Architecture

Clean Architecture with `domain`/`infrastructure` split, wired via `go.uber.org/dig` dependency injection.

### Layer Flow

```
Cobra CLI (controllers) -> Commands (domain logic) -> Repositories (ports/adapters)
```

- **Entry point**: `cmd/autoupdate/main.go` builds Cobra commands, `cmd/autoupdate/dig.go` wires the DI container
- **DI registration**: `internal/container.go` registers all layers bottom-up (repos -> entities -> commands -> controllers)
- **Domain commands**: `internal/domain/commands/` — `LocalCommand` (single repo), `RunCommand` (batch mode), `SelfUpdateCommand`, and `VersionCommand`
- **Domain ports**: `internal/domain/repositories/` — `UpdaterRepository`, `LocalUpdater`, `ProviderRepository`, and `SelfUpdateRepository` interfaces
- **Infrastructure adapters**: `internal/infrastructure/repositories/` — updater implementations per ecosystem, plus `cmdrunner` (shared command execution), `gitlocal` (go-git operations for local and batch modes), and `selfupdate`
- **Support utilities**: `internal/support/` — filesystem helpers (including the shared repository walkers, see below), remote file checker bridging `langforge` with `gitforge`, repo config loader for per-repo opt-out, and the shared changelog writer (see below)
- **Registries**: `provider_registry.go` (abstract factory for Git providers) and `updater_registry.go` (holds all updater implementations)

### Key External Libraries

- **gitforge** (`rios0rios0/gitforge`): Abstraction over GitHub/GitLab/Azure DevOps APIs. Domain entities (`Repository`, `PullRequest`, `Dependency`) are re-exported as type aliases from gitforge.
- **langforge** (`rios0rios0/langforge`): Language/ecosystem detection and shared version fetchers.
- **cliforge** (`rios0rios0/cliforge`): Shared CLI utilities including the self-update mechanism.
- **testkit** (`rios0rios0/testkit`): Base builder pattern for test data construction.

### Adding a New Updater

1. Create a new package under `internal/infrastructure/repositories/<ecosystem>/`
2. Implement `UpdaterRepository` interface (`Name()`, `Detect()`, `CreateUpdatePRs()`)
3. Register in `internal/infrastructure/repositories/container.go`

### Commit Signing

When `commit.gpgsign=true` is set in git config, commits are automatically signed using GPG or SSH (based on `gpg.format`). The signing key is read from `user.signingkey`. GPG passphrase is read from `GPG_PASSPHRASE` env var.

### Push Transport (Local Mode)

Push transport is auto-detected from the origin remote URL:
- **SSH** (`git@...`): Uses system SSH keys via gitforge's `PushChangesSSH`
- **HTTPS** (`https://...`): Uses gitforge's adapter pattern — resolves the provider from the URL, creates a token-enabled instance, and pushes with auth method retry

The `PushAuthResolver` interface in `gitlocal` abstracts the `ProviderRegistry` to avoid import cycles. The auth token (from `--token` flag or env vars) is only needed for HTTPS push and API calls.

### Config System

Configuration is **layered**: built-in defaults (`configs/autoupdate.yaml`, embedded via
`go:embed` in `configs/embed.go`) -> published defaults (the same file fetched from `main`,
best effort) -> the operator's file (`--config`, else `~/` or `~/.config/`) -> the target
repository's own `.autoupdate.yaml`. Each layer overrides only the keys its document
declares. `internal/domain/entities/config_layers.go` is the engine; `configLoader` in
`internal/infrastructure/controllers/config_helpers.go` assembles the first three, and
`ApplyRepoOverlay` folds the fourth per repository.

The mechanism is `yaml.v3` decoding into a *non-zero* struct: only the keys the document
carries are assigned, so absent-versus-`false` needs no pointer fields -- `ExcludeForks`,
`ExcludeArchived` and `Concurrency` stay value types. Maps are the exception (each value
decodes into a fresh zero element), so `Updaters` is blanked before the decode and re-merged
with `MergeUpdatersConfig` afterwards. Apply is atomic, and a comments-only document returns
`io.EOF`, which is a layer with nothing to say rather than a broken one.

Only the operator's layer is `ScopeOperator`. The other three decode through
`RestrictedConfig`, which has **no field** for `providers`, any credential,
`aggregate_branch_prefix` or `concurrency` -- enforcement by schema, not by a check that has
to run at the right moment. `operatorOnlyKeys` only reports what was ignored. One field it *does*
have is accepted in one direction only: `cleanup_stale_branches` may be switched **off** by
a restricted layer and never on (`acceptSwitchOff`). Off can only ever remove an action, and
`applySkipCleanupFlag` is applied in the controller *before* this layer is folded, so
honouring an enable would override `--skip-cleanup`. The working directory is never
searched, and finding no configuration is not an error: the built-in defaults are the base
of every run. `ApplyRepoOverlay` also tolerates nil settings, which is the state
`LocalController` deliberately keeps working in.

`ValidateSettings(settings, batch)` mirrors AutoBump's: `batch=true` requires a provider,
`batch=false` does not, because `autoupdate .` takes its provider from the repository's own
`origin` remote. `aggregate_branch_prefix` is operator-only *and* validated
(`ValidateAggregateBranchPrefix`): it aims a destructive operation, so it must name a branch
git accepts, contain a `/`, and not stop at one -- `chore/` would match AutoBump's
`chore/bump-*` branches.

**Repository exclusions:**
- **Global**: `exclude_repos` in the operator's config -- right-anchored glob list matched
  against `<org>/<repo>` (or `<org>/<project>/<repo>` on Azure DevOps).
- **Per-repo**: `.autoupdate.yaml` in the target repository's root. `skip: true` (with an
  optional `reason`) opts out entirely; the same file is also the project settings layer.
  `narrowToProjectSchema` prunes it to the keys the layer may carry before it is applied.
- `entities.ExcludesSelf` is the shared predicate. `filterRepositories` calls it
  organization-wide, and `loadRepoSettings`/`resolveLocalSettings` call it again once the
  repository's own file has been folded in -- which is the only pass in which a repository's
  own `exclude_*` keys can act, since the first ran before that file existed to be read.

### Changelog Writing (Keep a Changelog and chlog)

Every updater records its entries through `internal/support/changelog.go` — never call
`entities.InsertChangelogEntry` or write `CHANGELOG.md` directly from an updater. The helper detects
the target repository's format and picks the destination, so the two formats cannot drift apart:

- `LocalChangelogUpdate(repoDir, entries)` — writes on disk (local mode).
- `RemoteChangelogChanges(ctx, provider, repo, entries, fileChanges)` — appends `FileChange` values
  for the provider API (batch mode), used by `terraform`, `dockerfile`, and `pipeline`.
- `StageLocalChangelog` / `StageRemoteChangelog` — return a `StagedChangelog` (temp file plus
  repository-relative destination) for the seven ecosystems whose upgrade runs through a generated bash
  script. The script gets `CHANGELOG_FILE` and `CHANGELOG_DEST` from `StagedChangelog.Env()` and
  performs the copy with the shared snippet `support.ChangelogUpdateScript()`. Call
  `StagedChangelog.Discard(repoDir)` when abandoning a run: a chlog fragment is a *new untracked*
  file, so `git checkout -- .` would leave it behind (this is why the JavaScript cosmetic-lockfile
  revert threads the staged value).

No entry is ever restated. `newChangelogEntries` (`internal/support/changelog_dedupe.go`) drops any
entry the repository already records as pending, and every `CHANGELOG.md` edit goes through
`insertChangelogEntries` rather than calling `entities.InsertChangelogEntry` directly — gitforge
appends whatever it is handed, so the check has to happen on this side of the call, in the one place
every updater funnels through. It matters because autoupdate runs unattended on a schedule against the
same repositories: yesterday's entry is merged into the default branch by the time it looks again, and
without the check it was restated verbatim on every run until the next release moved the section away.
The pending set is read from `[Unreleased]` for a Keep a Changelog repository (wrapped continuation
lines folded back into their bullet) and from the fragment bodies under the unreleased directory for a
chlog one (`internal/support/chlog_pending.go`), so the two formats cannot drift. Matching is exact
after normalizing the bullet marker, backticks, whitespace and case — deliberately nothing fuzzier: an
entry naming a different version ("from `3.13` to `3.14`" after "from `3.12` to `3.13`") is a second,
real upgrade, and a similarity threshold that collapsed those would drop a change the repository took.
Released sections are never compared against; they are history, and the same dependency moving again
after a release is a new fact.

chlog (`internal/support/chlog.go` + `internal/domain/entities/chlog.go`) is detected from a
`.chlog.yaml`/`.chlog.yml` or a `.changes/unreleased/` directory. When detected, entries become one
fragment file per entry (`<unixnano>-<hex>.yaml` with `kind`/`body`/`time`) and `CHANGELOG.md` is
left untouched — editing it would recreate exactly the merge conflicts chlog exists to remove. The
configured `changesDir`/`unreleasedDir`/`kinds` are honored, and paths are validated against escaping
the repository root because `.chlog.yaml` is untrusted input from a repo autoupdate does not own. A
broken or unreadable `.chlog.yaml` fails loudly rather than falling back to `CHANGELOG.md`. The
per-ecosystem `changelogEntries(vCtx)` helper is all that remains ecosystem-specific.

A fragment is emitted byte for byte the way `chlog new` emits one, which is why
`ChlogFragment.MarshalYAML` builds the mapping by hand instead of letting the encoder marshal the
struct. Marshalling the struct leaves the style to the encoder: `kind` and `body` come out as plain
scalars whenever they happen not to need quoting, and `time` comes out as a bare YAML timestamp — a
`!!timestamp` where chlog wrote a `!!str`. A repository that files entries through both tools then
holds fragments in two shapes with two types under the same key, and anything reading them more
strictly than a Go struct decoder trips on autoupdate's alone. Every scalar is therefore
single-quoted and the timestamp is pre-formatted as RFC3339Nano in UTC. `chlog`'s optional
`breaking` key is deliberately never written: a dependency bump is not a breaking change, and chlog
omits the key when it is unset. The golden assertions in `internal/domain/entities/chlog_test.go`
are what keep the two writers in step, so a change to the shape needs the golden strings updated
against real `chlog new` output rather than against what the encoder happens to produce.

### Repository Walking (what an on-disk scan can see)

`WalkFilesByExtension` and `WalkFilesByPredicate` (`internal/support/filesystem.go`) are how every
updater finds files in a clone. Both delegate to one unexported `walkFiles`, so the rule about which
directories are entered lives in exactly one place — two hand-copied loops would drift, and a caller
cannot tell which of the two it is getting.

The rule is a **deny list**, `skippedWalkDirs` in `internal/support/walk_skip.go`, and the direction
matters. These walkers feed dependency *discovery*, where a default of "skip" fails silently: nothing
is found, no error is raised, and the run reports success with zero upgrades. The previous rule
skipped every directory whose name began with a dot, conflating "hidden" with "not the repository's
own source"; `.github/workflows/ci.yaml` is both hidden and the repository's own source, so the
pipeline updater could never upgrade a GitHub Actions workflow on the clone-based path even though
`langforge`'s detector had just matched the repository through the provider API. Do **not** replace
the deny list with an allow list of hidden directories that matter, and do not add an opt-in
parameter or a second walker variant: the bug *was* a missing entry in an implicit allow list, and an
opt-in leaves the blind behaviour as the default every future caller inherits. A forgotten deny-list
entry costs a slower walk and at worst a match inside a derived tree — loud, and visible in the diff.

Every entry names a tree the repository does not author: version-control metadata, vendored code
(`vendor`, `node_modules`) and tool caches (`.terraform`, `.venv`, `.gradle`, …). `.git` is the
reason the list exists at all — callers feed results straight into `support.WriteFileChanges`, which
writes back under the root, so a match there would rewrite repository internals. Editor state
(`.idea`, `.vscode`) is deliberately absent: it is committed by the repository, so excluding it would
need exactly the case-by-case reasoning the deny list exists to avoid.

Go module discovery is the one caller that filters further on its own: `moduleDirsFromPaths`
(`internal/infrastructure/repositories/golang/modules.go`) drops hidden, vendored and `testdata`
segments, and must keep doing so — the generated upgrade script discovers modules with
`find ... -not -path '*/.*/*'`, and the two sets must not disagree about which modules get upgraded.

### Stale Branch Cleanup

Because the aggregate branch is dated (`chore/autoupdate-YYYY-MM-DD`), every run on a new day creates a new branch, so unattended runs stack up abandoned branches. `cleanupStaleAggregateBranches` (`internal/domain/commands/cleanup.go`) runs in `processLocalUpdaters` right after the clone and before `CreateBranchFromDefault`: it lists the remote branches, keeps those carrying the aggregate prefix (never the target branch), closes each one's PR via `ForgeProvider.ClosePullRequest`, then deletes the branch through `BatchGitContext.DeleteBranch` (remote **and** local — a leftover local branch would make the branch still look like it exists). It sits after the same-day `PullRequestExists` check, so a PR is never closed without a replacement being opened. It is opt-out: `entities.CleanupEnabled` treats an unset `cleanup_stale_branches` as enabled, and the persistent `--skip-cleanup` flag (applied by `applySkipCleanupFlag` in `controllers/config_helpers.go`) overrides the config per run. `entities.ResolveAggregateBranchPrefix` (`aggregate_branch_prefix`, default `chore/autoupdate-`) feeds both `buildAggregateBranchName` and the cleanup filter so they cannot diverge. Cleanup is best-effort: failures are logged and skipped, never aborting the update run.

### Version Pins Only Move Forwards

A version pin is rewritten **only when the fetched release is strictly newer** than the one already
there. `support.IsNewerVersion` (`internal/support/version.go`) is the single answer to that question
and every updater goes through it — never `current != latest`, which is what made autoupdate downgrade
a repository that had moved past the release feed. The feeds answer a narrower question than they look:
nodejs.org/dist reports the newest *LTS* line, the Java feed the newest *LTS* JDK, the Python and Ruby
feeds the newest *stable* series, so a repository on Node 26 or JDK 25 is ahead of "latest".

The rule exists on both sides of the Go/bash seam, because both sides act on it. Go decides
`NeedsVersionUpgrade`, which picks the branch name, the commit message, the changelog entry and the PR
title; the generated script re-reads the pin from the clone and decides whether the file is actually
rewritten. `support.VersionGuardScript()` emits the bash counterpart —
`autoupdate_version_is_newer <candidate> <current>`, plus `autoupdate_image_tag_is_older <file> <image>
<version> [tag-pattern]` for Dockerfile base images, where one file may pin an image several times and
the `sed` that rewrites them compares nothing. `internal/support/version_test.go` runs the same table
through both implementations, so they cannot drift. That table is the only thing keeping the two
halves honest, so a new comparison rule needs rows that would actually tell them apart — the
single-digit `rc.1`/`rc.2` rows it started with agreed under both string and semver ordering, which is
how a lexical pre-release comparison survived in the bash half. Any script fragment that calls a guard must emit
`VersionGuardScript()` itself rather than inheriting the definition from whatever ran before it.

Comparison follows Semantic Versioning precedence on both sides: a final release outranks any
pre-release of the same version, and build metadata is excluded entirely. `parseVersion` captures what
follows a `+` only to keep the version recognisable and then drops it, so `1.0.0+build.1` and `1.0.0`
compare equal and neither is rewritten to the other — folding it into the pre-release slot instead got
that wrong in both directions.

The three single-line pin files — `.java-version`, `.python-version`, `.ruby-version` — share
`support.VersionPinUpdateScript` for the same reason. JavaScript and C# are not callers: one keeps
two pin files in step, the other reads its pin out of `global.json`.

The five ecosystems that rewrite `Dockerfile` base images share one emitter,
`support.DockerfileTagUpdateScript`, which takes the image names, the shell variable holding the
version, and any prelude deriving it (Java pins a bare major, .NET a major.minor). It emits the
guard itself, so a caller cannot end up with the walk but not the comparison — which is exactly how
the blanket `sed` survived in some of them. Add an ecosystem by adding a call, not a sixth copy of
the loop.

Anything not shaped like a dotted numeric version — `lts/*`, `system`, `jruby-9.4.0.0`, `3.13t` — fails
the comparison and the pin is left alone: those are deliberate choices by the repository being updated,
and a release number from an unrelated channel is not a comparable replacement. `terraform`,
`dockerfile` and the Go Dockerfile tag rewriter (`golang/dockerfile_tags.go`) reach the same conclusion
through `golang.org/x/mod/semver` directly, since they compare registry tags rather than pin files.

### Only Finished Releases, and Only Within a Major

"Newest" is two questions, and a version pin is only moved when both answer yes: is the
candidate *finished*, and is it on the *same major line*. `support.IsNewerVersion` answers
neither -- it orders versions, and `3.0.0-beta3` really does outrank `2.26.1`.

**Pre-releases.** `prereleaseQualifiers` (`internal/support/prerelease.go`) holds the
vocabulary -- alpha, beta, milestone, cr, rc, preview, pre, dev, snapshot. Both
`prereleaseShape`, which `support.IsPrereleaseVersion` matches, and
`support.MavenVersionIgnore()`, which supplies `-Dmaven.version.ignore`, are *derived* from
that slice rather than restating it, so neither can drift by being edited alone.

`IsPrereleaseVersion` has **no production caller**, and nothing in the Go path filters
pre-releases: `go get -u` already declines them, and the Java path delegates the decision to
Maven. It exists so the vocabulary has one readable, directly testable definition, and so
`TestMavenVersionIgnore` can check the Maven regexes against something other than a
restatement of themselves. Do not read it as a filter that runs, and do not add a caller on
the assumption one is missing.

Maven is where this bites. It has no concept of a pre-release at all: `3.0.0-beta3`,
`7.1.0-M1`, `5.5-beta2` and `5.7-alpha1` are ordinary releases to it, `allowSnapshots=false`
excludes none of them, and `maven-metadata.xml` reports the newest of them as `<release>`. One
run moved four security pins onto pre-releases, and the log4j2 one silently dropped
`log4j-api` back to a transitive 2.24.1 -- reintroducing the three CVEs the pin existed to
remediate, in a pull request titled as a dependency update. Both goals now also pass
`-DallowMajorUpdates=false`: a security pin belongs on its own major line, and a new major
arriving unreviewed inside a routine bump is the same failure by another route.

**Majors.** `support.GoMajorGuardScript()` emits the bash counterpart for Go. `go get -u` is
documented as taking the latest *minor or patch*, and for most modules it does -- semantic
import versioning puts v2 and above behind a `/v2` suffix, which is a different module path
`-u` will never reach for. The exception is the boundary below that suffix: v0 and v1 share
the unsuffixed path, so a module tagging v1.0.0 after years on v0.x is simply the highest
version of the same module, and `-u` takes it. `+incompatible` modules cross the same way.
Go's own convention says v0 promises no compatibility, which is exactly why that jump breaks
things: `gobwas/glob` v1.0.0 removed the `Glob` interface every published `gocolly/colly`
still declares, and an indirect dependency nobody had named stopped the module compiling.

The guard snapshots the requirements before the upgrade and compares after `go mod tidy` --
not before, because tidy can settle a requirement on a different version than `go get` first
wrote. Anything whose major moved is put back with `go get <path>@<old>`; everything else in
the run is kept, so the pull request still carries every safe upgrade.

`autoupdate_go_report_unheld` then re-reads `go.mod` and reconciles against the before
snapshot, because the hold is two commands away from the state that gets committed and both
outcomes it used to miss are worse than the jump. A `go get <old>` can *fail* -- precisely
when an upgraded dependency now requires the major being held -- and the jump would ship under
a description saying it was held. And a successful hold cascades: `go get <path>@<older>`
downgrades everything that required the higher version, which can drag an unrelated
requirement below where the run started. That second one is the failure this page already
names under "Version Pins Only Move Forwards", reached by a new route.

It reports rather than restores -- restoring is another `go get`, which cascades the same way
-- and returns non-zero when anything is unresolved, so the caller branches rather than
letting `set -e` abort before the safe part of the upgrade is committed. The PR description
says the run *checked and held*, never that every hold succeeded, because the Go side that
writes it has no access to the script's result. Both halves are exercised in
`internal/support/`, the bash one against a stub `go` binary that records what it was asked
for.

### Digest-Pinned Base Images

A `Dockerfile` clause pinning both halves — `FROM python:3.13-slim@sha256:...` — is rewritten **only by
code that can re-resolve the digest**, because the digest is what Docker pulls. Moving the tag and
leaving the digest behind produces a diff that reads as an upgrade and builds the previous image, and
nothing downstream catches it: the version pin really did move, so the branch, the commit message, the
changelog entry and the PR title are all consistent with an upgrade that never reaches the build.

The `dockerfile` updater owns them. `fromPattern` captures the digest, the Docker Hub listing that
already answers "does this tag exist?" carries each tag's digest (`registryTag`), and `applyUpgrades`
rewrites `image:tag@digest` as one unit through `upgradeTask.currentRef`/`newRef`. The substitution
goes through `replaceRef`, which requires the match to end the reference: a plain `python:3.13-slim`
also occurs inside `python:3.13-slim@sha256:...`, so a substring replace would rewrite the
digest-pinned clause it was told to leave alone (and `python:3.1` inside `python:3.13` is the same trap
one tag narrower). When the registry reports no digest for the tag being moved to, the clause is left
exactly as it is — writing the tag alone pins the previous manifest beside a version it is not, and
dropping the digest silently un-pins an image the repository pinned on purpose.

Every other rewrite path has no registry, and therefore skips a digest-pinned clause instead of
half-rewriting it: `support.DockerfileTagUpdateScript`, where the `sed` address and
`autoupdate_image_tag_is_older` both key on the single `digestPinMarker` constant so the guard and the
substitution cannot disagree about which lines are in play, and `golang/dockerfile_tags.go`, which has
skipped them from the start. In batch mode the `dockerfile` updater runs against the same aggregate
branch, so those clauses are still upgraded — correctly. Do not "fix" a skipped clause by dropping its
digest to make the substitution apply.

### Package Manager Selection (Python)

A repository is upgraded with the dependency manager it already uses; an update run must never migrate it to a different one. `newPythonProject` (`internal/infrastructure/repositories/python/toolchain.go`) is the single place that makes that decision, and `detectRemoteProject`/`detectLocalProject` are the only ways to reach it — do not re-derive the toolchain at a call site. PDM is selected **only** when a `pyproject.toml` is present: that file is PDM's project definition, so running `pdm update` without one makes PDM write a fresh `pyproject.toml`, converting a pip/`requirements.txt` repository into a PDM repository inside what was meant to be a dependency bump. A `pdm.lock` with no `pyproject.toml` beside it therefore stays on pip. A `pyproject.toml` naming PDM is not enough on its own either: `[tool.pdm]` is also how a pip project declares its package layout, so when a `requirements.txt` is present and no `pdm.lock` has ever been committed, the repository keeps pip. Upgrading it through PDM would resolve a lock file from scratch — the whole of the resulting pull request, since `pdm update` leaves the pyproject's own constraints alone — while never touching the `requirements.txt` the build installs from. A committed `pdm.lock` is what settles the question the other way, which is why `pdmMarkers` keeps the two signals apart instead of collapsing them into one boolean. The resolved `pythonProject` flows through `upgradeParams`/`localUpgradeParams` into the generated script, the changelog entry, the PR description and the dry-run report, so those cannot disagree about what ran. On the pip path the script additionally brackets the upgrade with `writeManifestSnapshot`/`writeManifestRestore`, which delete a `pyproject.toml` or `pdm.lock` that appeared during the run — never one the repository already owned.

### Batch Mode Concurrency

`autoupdate run` processes repositories within an org in parallel via `errgroup` (default 4), set by the `concurrency` config field or `--concurrency` flag; values `< 1` are clamped to 1 (sequential). `RunCommand` guards its shared accumulators with a `sync.Mutex` — keep new shared state goroutine-safe when editing the per-repo loop.

## Testing Conventions

- No build tag on unit tests; `//go:build integration` is reserved for tests needing real infrastructure
- External test packages (e.g., `commands_test` for package `commands`)
- BDD structure with `// given`, `// when`, `// then` comments
- Parallel execution via `t.Parallel()` and `t.Run()` subtests
- Test doubles in `test/domain/commanddoubles/` (stubs), `test/domain/entitybuilders/` (builders), and `test/infrastructure/repositorydoubles/` (stubs, spies, builders)
- Uses `stretchr/testify` for assertions — prefer stubs over mocks

<!-- chlog:start -->
## Changelog (chlog) — MANDATORY

If the repository you are working in uses chlog (a `.chlog.yaml` or `.chlog.yml`
config file, or a `.changes/` directory, exists at the project root), the
following is binding and ALWAYS applies: whenever you make ANY change, you MUST
create a changelog fragment as part of the same change — automatically, without
being asked, before committing.

- Do NOT edit CHANGELOG.md directly; it is generated from fragments.
- Create the fragment with:
  `chlog new --kind <Kind> --body "<imperative description>"`
- Valid kinds: Added, Changed, Deprecated, Removed, Fixed, Security
- Choose the kind that best matches the change (e.g., new feature → Added,
  bug fix → Fixed, behavior change → Changed, removal → Removed, security fix → Security).
- If the change is backward-INCOMPATIBLE with the public API (a breaking
  change), you MUST add the `--breaking` flag:
  `chlog new --kind <Kind> --breaking --body "<description>"`.
  This is the ONLY thing that triggers a major version bump — the kind alone
  never does (per SemVer, major = incompatible change). When unsure whether a
  change breaks compatibility, ask the user instead of guessing.
- Fragments are YAML files in `.changes/unreleased/`; stage them with your commit.
- `chlog check` fails the build when a fragment is missing — never skip it.
<!-- chlog:end -->
