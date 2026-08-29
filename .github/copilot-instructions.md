# AutoUpdate

AutoUpdate is a Go CLI tool that automatically discovers repositories across multiple Git providers (GitHub, GitLab, Azure DevOps), scans them for outdated dependencies, and creates Pull Requests with version upgrades. It supports Terraform, Go, Python, JavaScript, Dart/Flutter, Ruby, Java, C#, Dockerfile, and CI/CD Pipeline ecosystems, with an extensible updater plugin interface.

Always reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.

## Working Effectively

### Bootstrap, Build, and Test
- Install dependencies: `go mod download` -- takes <1 second (after first download)
- Build the binary: `make build` -- takes ~35 seconds first time, <1 second after. NEVER CANCEL. Set timeout to 60+ minutes.
- Run tests: `go test ./...` -- takes <1 second (cached), ~10 seconds clean. NEVER CANCEL. Set timeout to 30+ minutes.
- Format code: `go fmt ./...`
- Static analysis: `go vet ./...`
- Tidy dependencies: `go mod tidy`

### Linting and Testing with Pipeline Scripts
This project uses the [rios0rios0/pipelines](https://github.com/rios0rios0/pipelines) repository for linting and testing:

**To run tests:**
```bash
# Clone the pipelines repository if not already available
git clone https://github.com/rios0rios0/pipelines.git /tmp/pipelines

# Run tests using the pipeline script
/tmp/pipelines/global/scripts/GoLang/test/run.sh
```

**To run linting:**
```bash
# Clone the pipelines repository if not already available
git clone https://github.com/rios0rios0/pipelines.git /tmp/pipelines

# Run linting using GoLangCI-Lint script
/tmp/pipelines/global/scripts/GoLang/GoLangCI-Lint/run.sh
```

Note: The CI/CD pipeline automatically uses these scripts via the reusable workflow `rios0rios0/pipelines/.github/workflows/go-binary.yaml@main`.

### Running the Application
- ALWAYS run the bootstrapping steps first.
- Run via Makefile: `make run`
- Run directly: `go run ./cmd/autoupdate`
- Run built binary: `./bin/autoupdate`
- Test help command: `./bin/autoupdate --help`
- Test run command help: `./bin/autoupdate run --help`

### Usage Modes
- **Single repository**: `./bin/autoupdate .` or `./bin/autoupdate [path]` — updates dependencies in a local repository, detects project type, and creates a PR. The `local` subcommand was removed in `1.0.0` and is kept hidden and deprecated so the bare word is not read as a path
- **Batch (run) mode**: `./bin/autoupdate run` — reads config file, discovers repositories from all configured providers and orgs, and creates update PRs
- **Self-update**: `./bin/autoupdate self-update` — downloads and installs the latest release from GitHub
- **Version**: `./bin/autoupdate version` — prints the current build version

### Installation
- Build first: `make build`
- Install to user bin: `make install` (copies to `~/.local/bin/autoupdate`)

## Validation

### CRITICAL: Manual Validation Requirements
- ALWAYS test the built binary with `./bin/autoupdate --help` to ensure it works
- ALWAYS run the tool in dry-run mode to validate functionality: `./bin/autoupdate run --dry-run`
- ALWAYS exercise the run command help: `./bin/autoupdate run --help`

### Testing Scenarios
After making changes, ALWAYS run through these validation steps:
1. `make build` - must complete successfully
2. `go test ./...` - all tests must pass
3. `./bin/autoupdate --help` - must show help text with available commands
4. `./bin/autoupdate run --dry-run` - should process config and discover repos in dry-run mode
5. `go fmt ./...` and `go vet ./...` - must pass clean

### Pre-commit Validation
- Always run `go fmt ./...` before committing or CI will fail
- Always run `go vet ./...` before committing
- Always run `go test ./...` to ensure no regressions
- For full linting validation, use the pipeline script: `/tmp/pipelines/global/scripts/GoLang/GoLangCI-Lint/run.sh`
- CI pipeline uses the rios0rios0/pipelines repository scripts which will fail if code style or quality issues exist

## Build and Test Timing Expectations
- **Build**: ~35 seconds first time, <1 second subsequent builds. NEVER CANCEL. Set timeout to 60+ minutes.
- **Tests**: <1 second (cached), ~7 seconds clean run. NEVER CANCEL. Set timeout to 30+ minutes.
- **Go mod operations**: <1 second after first download. Set timeout to 15+ minutes.

## Architecture

Clean Architecture with `domain`/`infrastructure` split, wired via `go.uber.org/dig` dependency injection.

### Layer Flow

```
Cobra CLI (controllers) -> Commands (domain logic) -> Repositories (ports/adapters)
```

- **Entry point**: `cmd/autoupdate/main.go` builds Cobra commands, `cmd/autoupdate/dig.go` wires the DI container
- **DI registration**: `internal/container.go` registers all layers bottom-up (repos -> entities -> commands -> controllers)
- **Domain commands**: `internal/domain/commands/` — `LocalCommand`, `RunCommand`, `SelfUpdateCommand`, `VersionCommand`
- **Domain ports**: `internal/domain/repositories/` — `UpdaterRepository`, `LocalUpdater`, `ProviderRepository`, `SelfUpdateRepository`
- **Infrastructure adapters**: `internal/infrastructure/repositories/` — updater implementations per ecosystem (terraform, golang, python, javascript, dart, ruby, java, csharp, dockerfile, pipeline), plus `cmdrunner` (shared command execution), `gitlocal` (go-git operations), and `selfupdate`
- **Support utilities**: `internal/support/` — filesystem helpers, remote file checker bridging `langforge` with `gitforge`, and the shared changelog writer (`changelog.go`, `chlog.go`) every updater goes through
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

### Configuration System
- **Configuration is layered.** Four sources, each overriding only the keys its document declares: built-in defaults (`configs/autoupdate.yaml`, embedded via `configs/embed.go`) → published defaults (the same file fetched from `entities.DefaultConfigURL`, best effort) → the operator's file (`--config`, else `~/` then `~/.config/`, names `.autoupdate.{yml,yaml}` and `autoupdate.{yml,yaml}`) → the target repository's own `.autoupdate.yaml`
- `internal/domain/entities/config_layers.go` is the engine (`ConfigLayer`, `ApplyLayer`, `ApplyRepoOverlay`, `ResolveSettings`, `FinalizeSettings`); `configLoader` in `internal/infrastructure/controllers/config_helpers.go` assembles the first three (its `fetch` field is the seam that keeps the tests offline)
- The mechanism is `yaml.v3` decoding into a *non-zero* struct: absent keys are never assigned, so absent-versus-`false` needs no pointer fields. Maps are the exception — each value decodes into a fresh zero element — so `Updaters` is blanked before the decode and re-merged with `MergeUpdatersConfig` afterwards
- The working directory is never searched for the operator's configuration, and finding none is not an error: the built-in defaults are the base of every run (`resolveOperatorConfigPath` returns `""`)
- Only the operator's layer is `ScopeOperator`. The other three decode through `RestrictedConfig`, which has **no field** for `providers`, any credential, `aggregate_branch_prefix` or `concurrency` — enforcement by schema, not by a check that has to run at the right moment
- `ValidateSettings(settings, batch)`: `batch=true` requires a provider, `batch=false` does not, because `autoupdate .` takes its provider from the repository's own `origin` remote
- `aggregate_branch_prefix` is operator-only *and* validated (`entities.ValidateAggregateBranchPrefix`): it aims a destructive operation, so it must name a branch git accepts, contain a `/`, and not stop at one — `chore/` would match AutoBump's `chore/bump-*` branches
- Supports multiple providers with organizations and tokens
- Token resolution: inline values, `${ENV_VAR}` expansion, file paths
- Updaters can be enabled/disabled with per-updater `auto_complete` and `target_branch`
- Concurrency: `autoupdate run` processes repos within an org in parallel via `errgroup` (default 4), tuned by the `concurrency` config field or `--concurrency` flag (`< 1` clamps to sequential). `RunCommand` guards shared accumulators with a `sync.Mutex` — keep any new shared state goroutine-safe
- Stale branch cleanup: `cleanup_stale_branches` (opt-out, default enabled) and the persistent `--skip-cleanup` flag control it. `cleanupStaleAggregateBranches` in `internal/domain/commands/cleanup.go` runs in `processLocalUpdaters` after the clone and before `CreateBranchFromDefault`, deleting every remote branch carrying the aggregate prefix and closing its PR via `ForgeProvider.ClosePullRequest`. `entities.CleanupEnabled` resolves the toggle; `applySkipCleanupFlag` in `internal/infrastructure/controllers/config_helpers.go` lets the flag override the config
- `aggregate_branch_prefix` (default `chore/autoupdate-`, via `entities.ResolveAggregateBranchPrefix`) feeds both `buildAggregateBranchName` and the cleanup filter, so the branch a run creates and the branches cleanup removes can never diverge
- Repository exclusions:
  - **Global**: `exclude_repos` in the operator's config — right-anchored glob list matched against `<org>/<repo>` (or `<org>/<project>/<repo>` for ADO).
  - **Per-repo**: `.autoupdate.yaml` in the target repository's root. `skip: true` (with an optional `reason`) opts out entirely; the same file is *also* the project settings layer, pruned by `narrowToProjectSchema` before it is applied. Read via `support.LoadLocalRepoConfig` (local mode) and `support.LoadRemoteRepoConfig` (batch mode). Remote fetch and parse failures fail open so a flaky API does not silently disable all updates.
  - `entities.ExcludesSelf` is the shared predicate. `filterRepositories` calls it organization-wide, and `loadRepoSettings`/`resolveLocalSettings` call it again once the repository's own file has been folded in — the only pass in which a repository's own `exclude_*` keys can act, since the first ran before that file existed to be read.

### Commit Signing
When `commit.gpgsign=true` is set in git config, commits are automatically signed using GPG or SSH (based on `gpg.format`). The signing key is read from `user.signingkey`. GPG passphrase is read from `GPG_PASSPHRASE` env var.

### Push Transport (Local Mode)
Push transport is auto-detected from the origin remote URL:
- **SSH** (`git@...`): Uses system SSH keys via gitforge's `PushChangesSSH`
- **HTTPS** (`https://...`): Uses gitforge's adapter pattern with auth method retry
- The `PushAuthResolver` interface in `gitlocal/` abstracts the `ProviderRegistry` to avoid import cycles

### Changelog Writing
- Every updater records entries through `internal/support/changelog.go` — never call `entities.InsertChangelogEntry` or write `CHANGELOG.md` from an updater. The helper detects the target repo's format and picks the destination so the formats cannot drift.
- Seven script-driven ecosystems stage the changelog (`StageLocalChangelog`/`StageRemoteChangelog` → `StagedChangelog`) and copy it inside the generated bash via `support.ChangelogUpdateScript()` (`CHANGELOG_FILE`/`CHANGELOG_DEST` from `StagedChangelog.Env()`); `terraform`, `dockerfile`, `pipeline` append `FileChange` values directly. On abandon, call `StagedChangelog.Discard(repoDir)` — a chlog fragment is a *new untracked* file that `git checkout -- .` would leave behind.
- Entries are de-duplicated before anything is written: `newChangelogEntries` (`internal/support/changelog_dedupe.go`) drops any entry the repository already records as pending — a bullet under `[Unreleased]` (folding wrapped continuation lines), or the body of a fragment under the chlog unreleased directory (`internal/support/chlog_pending.go`). Every `CHANGELOG.md` edit goes through `insertChangelogEntries`, never `entities.InsertChangelogEntry` directly: gitforge appends whatever it is handed, so an unattended daily run restated yesterday's entry verbatim until the next release moved the section away. Matching is exact after normalizing (marker, backticks, whitespace, case) — deliberately not fuzzy, since an entry naming a different version is a second real upgrade. Only `[Unreleased]` is compared; released sections are history.
- chlog (`internal/support/chlog.go`, `internal/domain/entities/chlog.go`) is auto-detected from `.chlog.yaml`/`.chlog.yml` or a `.changes/unreleased/` directory. When present, each entry becomes one fragment file (`<unixnano>-<hex>.yaml` with `kind`/`body`/`time`) and `CHANGELOG.md` is left untouched. The `.chlog.yaml` is untrusted repo input: honor its `changesDir`/`unreleasedDir`/`kinds`, validate paths against escaping the repo root, and fail loudly on a broken file rather than falling back to `CHANGELOG.md`.

### Python Package Manager Selection
- A repo is upgraded with the dependency manager it already uses; a run must never migrate it. `newPythonProject` (`internal/infrastructure/repositories/python/toolchain.go`) is the single decision point, reachable only via `detectRemoteProject`/`detectLocalProject` — never re-derive the toolchain at a call site.
- PDM is selected **only** when a `pyproject.toml` is present (running `pdm update` without one would write a fresh `pyproject.toml`, converting a pip repo to PDM). A `pdm.lock` with no `pyproject.toml` beside it stays on pip, and a `pyproject.toml` that only *declares* PDM (`[tool.pdm]` is also how a pip project states its package layout) stays on pip too when a `requirements.txt` is present and no `pdm.lock` was ever committed — a PDM run there would resolve a lock file from scratch and leave the `requirements.txt` the build installs from untouched. `pdmMarkers` keeps the lock and the declaration apart so the two cannot be confused. The resolved `pythonProject` flows through `upgradeParams`/`localUpgradeParams` into the script, changelog entry, PR description, and dry-run report so they cannot disagree. On the pip path the script brackets the upgrade with `writeManifestSnapshot`/`writeManifestRestore`, deleting any `pyproject.toml`/`pdm.lock` that *appeared* during the run — never one the repo already owned.

### Version Pins Only Move Forwards
- A version pin is rewritten **only when the fetched release is strictly newer** than the one already there. Route every comparison through `support.IsNewerVersion` (`internal/support/version.go`) — never `current != latest`, which downgraded repos that had moved past the release feed (`nodejs.org/dist` reports newest LTS, the Java feed newest LTS JDK, Python/Ruby newest stable, so a repo on Node 26 or JDK 25 is *ahead* of "latest").
- The rule lives on both sides of the Go/bash seam: Go decides `NeedsVersionUpgrade` (branch, commit, changelog, PR title); the generated script re-reads the pin and decides whether the file is actually rewritten. `support.VersionGuardScript()` emits the bash counterpart (`autoupdate_version_is_newer`, plus `autoupdate_image_tag_is_older` for Dockerfile base images). `internal/support/version_test.go` runs one table through both implementations so they cannot drift — a new comparison rule needs rows that actually distinguish string from semver ordering.
- Comparison follows SemVer precedence: a final release outranks any pre-release of the same version; build metadata (`+build`) is dropped so `1.0.0+build.1` and `1.0.0` compare equal. Anything not dotted-numeric (`lts/*`, `system`, `jruby-9.4.0.0`, `3.13t`) fails the comparison and is left alone.
- Shared emitters, not per-ecosystem copies: `support.VersionPinUpdateScript` for the three single-line pins (`.java-version`/`.python-version`/`.ruby-version`), `support.DockerfileTagUpdateScript` for the five ecosystems that rewrite Dockerfile base images (it emits the guard itself so a caller can't get the walk without the comparison). `terraform`, `dockerfile`, and `golang/dockerfile_tags.go` compare registry tags via `golang.org/x/mod/semver` directly.

### Digest-Pinned Base Images
- A `FROM image:tag@sha256:...` clause is only rewritten by code that can re-resolve the digest, because the digest — not the tag — is what Docker pulls. Moving the tag alone yields a diff that reads as an upgrade and builds the previous image, and nothing downstream catches it because the version pin really did move.
- The `dockerfile` updater owns them: `fromPattern` captures the digest, the Docker Hub listing that answers "does this tag exist?" also carries each tag's digest (`registryTag`), and `applyUpgrades` rewrites `image:tag@digest` as one unit via `upgradeTask.currentRef`/`newRef` and `replaceRef` (which requires a whole-reference match, so `python:3.13-slim` is not matched inside `python:3.13-slim@sha256:...`). A tag the registry reports no digest for means the clause is left untouched — never write the tag alone, and never drop the digest to make the rewrite apply.
- Everything that rewrites base images without registry access skips digest-pinned clauses: `support.DockerfileTagUpdateScript` (the `sed` address and `autoupdate_image_tag_is_older` both key on `digestPinMarker`, one constant so they cannot disagree) and `golang/dockerfile_tags.go`. In batch mode the `dockerfile` updater runs on the same aggregate branch and picks them up.

### Repository Walking
- `WalkFilesByExtension`/`WalkFilesByPredicate` (`internal/support/filesystem.go`) both delegate to one `walkFiles`, so the directory-skip rule lives in exactly one place. That rule is a **deny list** (`skippedWalkDirs` in `internal/support/walk_skip.go`), not an allow list — these walkers feed dependency *discovery*, where a default of "skip" fails silently (nothing found, no error, success with zero upgrades). A dot-prefix rule once hid `.github/workflows/`, so the pipeline updater could never upgrade a GitHub Actions workflow on the clone path. Do not swap in an allow list or add an opt-in walker variant.
- Entries name trees the repo does not author (VCS metadata, `vendor`/`node_modules`, tool caches like `.terraform`/`.venv`/`.gradle`). `.git` is why the list exists: results feed `support.WriteFileChanges`, which writes back under the root. Editor state (`.idea`, `.vscode`) is deliberately absent — it is committed. Go module discovery filters further (`moduleDirsFromPaths` in `golang/modules.go` drops hidden/vendored/`testdata`) to stay in sync with the script's `find ... -not -path '*/.*/*'`.

### Testing Infrastructure
- Unit tests carry no build tag, so `go test ./...` runs them; `//go:build integration` is reserved for tests needing real infrastructure
- Uses testify for assertions (`assert`/`require`) — prefer stubs over mocks
- Test doubles in `test/domain/commanddoubles/` (stubs), `test/domain/entitybuilders/` (builders), and `test/infrastructure/repositorydoubles/` (stubs, spies, builders)
- Uses `github.com/rios0rios0/testkit` for additional test helpers
- BDD-style tests with Given/When/Then comments
- Parallel test execution via `t.Run` subtests

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
