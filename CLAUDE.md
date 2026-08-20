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

go test -tags unit ./...      # Run all unit tests (requires -tags unit)
go test -tags unit -run TestFunctionName ./internal/domain/commands/  # Run a single test
go fmt ./...                  # Format code
go vet ./...                  # Static analysis
go mod tidy                   # Clean up dependencies
```

All unit tests require the `unit` build tag: `//go:build unit`. Running without `-tags unit` will find no tests.

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
- **Support utilities**: `internal/support/` — filesystem helpers, remote file checker bridging `langforge` with `gitforge`, repo config loader for per-repo opt-out, and the shared changelog writer (see below)
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

Auto-discovery searches `.`, `.config`, `configs`, `$HOME`, `$HOME/.config` for `autoupdate.yaml` / `.autoupdate.yaml`. Tokens support inline values, `${ENV_VAR}` expansion, and file path resolution.

**Repository exclusions:**
- **Global**: `exclude_repos` in user config — right-anchored glob list matched against `<org>/<repo>` (or `<org>/<project>/<repo>` for ADO). Honored in batch mode and in local mode when a config file is loadable.
- **Per-repo**: `.autoupdate.yaml` in the target repository's root with `skip: true` (and optional `reason`). Checked in both `autoupdate run` (fetched via provider API) and `autoupdate .` (read from disk).

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

chlog (`internal/support/chlog.go` + `internal/domain/entities/chlog.go`) is detected from a
`.chlog.yaml`/`.chlog.yml` or a `.changes/unreleased/` directory. When detected, entries become one
fragment file per entry (`<unixnano>-<hex>.yaml` with `kind`/`body`/`time`) and `CHANGELOG.md` is
left untouched — editing it would recreate exactly the merge conflicts chlog exists to remove. The
configured `changesDir`/`unreleasedDir`/`kinds` are honored, and paths are validated against escaping
the repository root because `.chlog.yaml` is untrusted input from a repo autoupdate does not own. A
broken or unreadable `.chlog.yaml` fails loudly rather than falling back to `CHANGELOG.md`. The
per-ecosystem `changelogEntries(vCtx)` helper is all that remains ecosystem-specific.

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

### Package Manager Selection (Python)

A repository is upgraded with the dependency manager it already uses; an update run must never migrate it to a different one. `newPythonProject` (`internal/infrastructure/repositories/python/toolchain.go`) is the single place that makes that decision, and `detectRemoteProject`/`detectLocalProject` are the only ways to reach it — do not re-derive the toolchain at a call site. PDM is selected **only** when a `pyproject.toml` is present: that file is PDM's project definition, so running `pdm update` without one makes PDM write a fresh `pyproject.toml`, converting a pip/`requirements.txt` repository into a PDM repository inside what was meant to be a dependency bump. A `pdm.lock` with no `pyproject.toml` beside it therefore stays on pip. A `pyproject.toml` naming PDM is not enough on its own either: `[tool.pdm]` is also how a pip project declares its package layout, so when a `requirements.txt` is present and no `pdm.lock` has ever been committed, the repository keeps pip. Upgrading it through PDM would resolve a lock file from scratch — the whole of the resulting pull request, since `pdm update` leaves the pyproject's own constraints alone — while never touching the `requirements.txt` the build installs from. A committed `pdm.lock` is what settles the question the other way, which is why `pdmMarkers` keeps the two signals apart instead of collapsing them into one boolean. The resolved `pythonProject` flows through `upgradeParams`/`localUpgradeParams` into the generated script, the changelog entry, the PR description and the dry-run report, so those cannot disagree about what ran. On the pip path the script additionally brackets the upgrade with `writeManifestSnapshot`/`writeManifestRestore`, which delete a `pyproject.toml` or `pdm.lock` that appeared during the run — never one the repository already owned.

### Batch Mode Concurrency

`autoupdate run` processes repositories within an org in parallel via `errgroup` (default 4), set by the `concurrency` config field or `--concurrency` flag; values `< 1` are clamped to 1 (sequential). `RunCommand` guards its shared accumulators with a `sync.Mutex` — keep new shared state goroutine-safe when editing the per-repo loop.

## Testing Conventions

- Build tag: `//go:build unit` on every test file
- External test packages (e.g., `commands_test` for package `commands`)
- BDD structure with `// given`, `// when`, `// then` comments
- Parallel execution via `t.Parallel()` and `t.Run()` subtests
- Test doubles in `test/domain/commanddoubles/` (stubs), `test/domain/entitybuilders/` (builders), and `test/infrastructure/repositorydoubles/` (stubs, spies, builders)
- Uses `stretchr/testify` for assertions — prefer stubs over mocks
