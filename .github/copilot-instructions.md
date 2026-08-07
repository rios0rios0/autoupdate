# AutoUpdate

AutoUpdate is a Go CLI tool that automatically discovers repositories across multiple Git providers (GitHub, GitLab, Azure DevOps), scans them for outdated dependencies, and creates Pull Requests with version upgrades. It supports Terraform, Go, Python, JavaScript, Ruby, Java, C#, Dockerfile, and CI/CD Pipeline ecosystems, with an extensible updater plugin interface.

Always reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.

## Working Effectively

### Bootstrap, Build, and Test
- Install dependencies: `go mod download` -- takes <1 second (after first download)
- Build the binary: `make build` -- takes ~35 seconds first time, <1 second after. NEVER CANCEL. Set timeout to 60+ minutes.
- Run tests: `go test -tags unit ./...` -- takes <1 second (cached), ~7 seconds clean. NEVER CANCEL. Set timeout to 30+ minutes.
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
- **Standalone (local) mode**: `./bin/autoupdate [path]` — updates dependencies in a local repository, detects project type, and creates a PR
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
2. `go test -tags unit ./...` - all tests must pass
3. `./bin/autoupdate --help` - must show help text with available commands
4. `./bin/autoupdate run --dry-run` - should process config and discover repos in dry-run mode
5. `go fmt ./...` and `go vet ./...` - must pass clean

### Pre-commit Validation
- Always run `go fmt ./...` before committing or CI will fail
- Always run `go vet ./...` before committing
- Always run `go test -tags unit ./...` to ensure no regressions
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
- **Infrastructure adapters**: `internal/infrastructure/repositories/` — updater implementations per ecosystem (terraform, golang, python, javascript, ruby, java, csharp, dockerfile, pipeline), plus `cmdrunner` (shared command execution), `gitlocal` (go-git operations), and `selfupdate`
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
- Config auto-discovery searches: `.`, `.config`, `configs`, `$HOME`, `$HOME/.config` for `.autoupdate.{yml,yaml}` and `autoupdate.{yml,yaml}`
- Override with `--config` flag or `-c`
- Supports multiple providers with organizations and tokens
- Token resolution: inline values, `${ENV_VAR}` expansion, file paths
- Updaters can be enabled/disabled with per-updater `auto_complete` and `target_branch`
- Concurrency: `autoupdate run` processes repos within an org in parallel via `errgroup` (default 4), tuned by the `concurrency` config field or `--concurrency` flag (`< 1` clamps to sequential). `RunCommand` guards shared accumulators with a `sync.Mutex` — keep any new shared state goroutine-safe
- Stale branch cleanup: `cleanup_stale_branches` (opt-out, default enabled) and the persistent `--skip-cleanup` flag control it. `cleanupStaleAggregateBranches` in `internal/domain/commands/cleanup.go` runs in `processLocalUpdaters` after the clone and before `CreateBranchFromDefault`, deleting every remote branch carrying the aggregate prefix and closing its PR via `ForgeProvider.ClosePullRequest`. `entities.CleanupEnabled` resolves the toggle; `applySkipCleanupFlag` in `internal/infrastructure/controllers/config_helpers.go` lets the flag override the config
- `aggregate_branch_prefix` (default `chore/autoupdate-`, via `entities.ResolveAggregateBranchPrefix`) feeds both `buildAggregateBranchName` and the cleanup filter, so the branch a run creates and the branches cleanup removes can never diverge
- Repository exclusions:
  - **Global**: `exclude_repos` in user config — right-anchored glob list matched against `<org>/<repo>` (or `<org>/<project>/<repo>` for ADO). Filtered by `filterRepositories` in `RunCommand`; honored in `LocalCommand` when settings load successfully.
  - **Per-repo**: `.autoupdate.yaml` in the target repository's root with `skip: true` (and optional `reason`). Read via `support.LoadLocalRepoConfig` (local mode) and `support.LoadRemoteRepoConfig` (batch mode, fetched through `FileAccessProvider.GetFileContent`). Remote fetch failures fail open so a flaky API does not silently disable all updates.

### Commit Signing
When `commit.gpgsign=true` is set in git config, commits are automatically signed using GPG or SSH (based on `gpg.format`). The signing key is read from `user.signingkey`. GPG passphrase is read from `GPG_PASSPHRASE` env var.

### Push Transport (Local Mode)
Push transport is auto-detected from the origin remote URL:
- **SSH** (`git@...`): Uses system SSH keys via gitforge's `PushChangesSSH`
- **HTTPS** (`https://...`): Uses gitforge's adapter pattern with auth method retry
- The `PushAuthResolver` interface in `gitlocal/` abstracts the `ProviderRegistry` to avoid import cycles

### Changelog Writing
- Every updater records entries through `internal/support/changelog.go` — never call `entities.InsertChangelogEntry` or write `CHANGELOG.md` from an updater. The helper detects the target repo's format and picks the destination so the formats cannot drift.
- Six script-driven ecosystems stage the changelog (`StageLocalChangelog`/`StageRemoteChangelog` → `StagedChangelog`) and copy it inside the generated bash via `support.ChangelogUpdateScript()` (`CHANGELOG_FILE`/`CHANGELOG_DEST` from `StagedChangelog.Env()`); `terraform`, `dockerfile`, `pipeline` append `FileChange` values directly. On abandon, call `StagedChangelog.Discard(repoDir)` — a chlog fragment is a *new untracked* file that `git checkout -- .` would leave behind.
- chlog (`internal/support/chlog.go`, `internal/domain/entities/chlog.go`) is auto-detected from `.chlog.yaml`/`.chlog.yml` or a `.changes/unreleased/` directory. When present, each entry becomes one fragment file (`<unixnano>-<hex>.yaml` with `kind`/`body`/`time`) and `CHANGELOG.md` is left untouched. The `.chlog.yaml` is untrusted repo input: honor its `changesDir`/`unreleasedDir`/`kinds`, validate paths against escaping the repo root, and fail loudly on a broken file rather than falling back to `CHANGELOG.md`.

### Python Package Manager Selection
- A repo is upgraded with the dependency manager it already uses; a run must never migrate it. `newPythonProject` (`internal/infrastructure/repositories/python/toolchain.go`) is the single decision point, reachable only via `detectRemoteProject`/`detectLocalProject` — never re-derive the toolchain at a call site.
- PDM is selected **only** when a `pyproject.toml` is present (running `pdm update` without one would write a fresh `pyproject.toml`, converting a pip repo to PDM). A `pdm.lock` with no `pyproject.toml` beside it stays on pip, and a `pyproject.toml` that only *declares* PDM (`[tool.pdm]` is also how a pip project states its package layout) stays on pip too when a `requirements.txt` is present and no `pdm.lock` was ever committed — a PDM run there would resolve a lock file from scratch and leave the `requirements.txt` the build installs from untouched. `pdmMarkers` keeps the lock and the declaration apart so the two cannot be confused. The resolved `pythonProject` flows through `upgradeParams`/`localUpgradeParams` into the script, changelog entry, PR description, and dry-run report so they cannot disagree. On the pip path the script brackets the upgrade with `writeManifestSnapshot`/`writeManifestRestore`, deleting any `pyproject.toml`/`pdm.lock` that *appeared* during the run — never one the repo already owned.

### Testing Infrastructure
- All unit tests are tagged with `//go:build unit` and must be run with `-tags unit`
- Uses testify for assertions (`assert`/`require`) — prefer stubs over mocks
- Test doubles in `test/domain/commanddoubles/` (stubs), `test/domain/entitybuilders/` (builders), and `test/infrastructure/repositorydoubles/` (stubs, spies, builders)
- Uses `github.com/rios0rios0/testkit` for additional test helpers
- BDD-style tests with Given/When/Then comments
- Parallel test execution via `t.Run` subtests
