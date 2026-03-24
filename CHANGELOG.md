# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

When a new release is proposed:

1. Create a new branch `bump/x.x.x` (this isn't a long-lived branch!!!);
2. The Unreleased section on `CHANGELOG.md` gets a version number and date;
3. Open a Pull Request with the bump version changes targeting the `main` branch;
4. When the Pull Request is merged, a new Git tag must be created using [GitHub environment](https://github.com/rios0rios0/autoupdate/tags).

Releases to productive environments should run from a tagged version.
Exceptions are acceptable depending on the circumstances (critical bug fixes that can be cherry-picked, etc.).

## [Unreleased]

## [0.11.1] - 2026-03-24

### Changed

- changed the Go module dependencies to their latest versions

### Fixed

- fixed the Python updater creating PRs without real dependency changes due to `pip freeze` capturing `file://` local path references from temp clone directories
- fixed the pipeline updater leaving stale version numbers in `displayName` fields when updating `versionSpec` in CI/CD pipeline files

## [0.11.0] - 2026-03-22

### Added

- added default config download and merge for the `updaters` section, following the autobump pattern of fetching defaults from GitHub and merging user overrides on top
- added `MergeUpdatersConfig()` function for field-level deep merge of updater configurations
- added `DecodeSettings()` function for parsing YAML settings with optional strict mode

### Changed

- changed `UpdaterConfig.Enabled` field to default to `true` when omitted from config, preventing updaters from being silently disabled when only `target_branch` or `auto_complete` is set
- changed `UpdaterConfig.AutoComplete` field from `bool` to `*bool` for proper field-level merge support
- changed the default `configs/autoupdate.yaml` to include all 6 registered updaters with sensible defaults
- changed the Go module dependencies to their latest versions

### Fixed

- fixed the Python updater creating empty PRs when the upgrade script did not modify any files
- fixed the pipeline updater replacing `displayName` instead of `versionSpec` in Azure DevOps pipeline files when both contained the same version string
- fixed stale temporary directories and changelog files not being cleaned up after process termination
- fixed Terraform, Dockerfile, and Pipeline updaters generating changelog entries without backticks around code identifiers and version numbers, violating the CHANGELOG formatting standard
- fixed the Terraform updater using non-production tags by validating that upgrade targets appear in the dependency repo's CHANGELOG.md

## [0.10.2] - 2026-03-19

### Changed

- changed the Go module dependencies to their latest versions

### Fixed

- fixed batch mode silently losing upgrade changes because `CreateBranchFromDefault` force-checkout wiped uncommitted `go.mod`/`go.sum` modifications
- fixed batch mode stash/pop safety by tracking whether a stash was created and verifying the stash ref before popping
- fixed potential auth token leak in upgrade script debug logs by redacting tokens from output

## [0.10.1] - 2026-03-18

### Changed

- changed local mode to auto-stash uncommitted changes instead of refusing to run on dirty worktrees, restoring to the original branch after the upgrade completes

### Fixed

- fixed Go dependency updater using deprecated `go get -u all` pattern that fails to detect updates in modern Go versions, replaced with `go get -u -t ./...`
- fixed local mode stash restore that could pop an unrelated stash entry or restore onto the wrong branch

## [0.10.0] - 2026-03-17

### Added

- added `exclude_forks` and `exclude_archived` settings to filter out fork and archived repositories during discovery

### Changed

- changed `gitforge` dependency from `v0.6.2` to `v0.7.0`, which includes sanitized clone URL logging, `IsFork`/`IsArchived` fields on `Repository`, and improved org-to-user discovery fallback

### Fixed

- fixed Go and JavaScript updaters running `CHANGELOG.md` updates and creating branches even when no dependency changes were detected
- fixed pipeline updater proceeding with file writes and `CHANGELOG.md` updates when version replacements produced no actual file changes

## [0.9.3] - 2026-03-17

### Fixed

- fixed pipeline updater failing to fetch latest Java version (HTTP 404) by upgrading `langforge` to `v0.4.0` which now uses the Amazon Corretto endpoint on `endoflife.date`

## [0.9.2] - 2026-03-17

### Changed

- changed `gitforge` dependency to `v0.6.2`, adding support for SSH config aliases in GitHub URL parsing
- changed `langforge` dependency to `v0.3.1`

## [0.9.1] - 2026-03-16

### Changed

- changed `gitforge` dependency to `v0.6.0`, picking up fixes for branch checkout with unstaged changes and GPG passphrase prompt in CI

## [0.9.0] - 2026-03-13

### Added

- added GPG/SSH commit signing support in batch mode (`run` command) via `BatchGitContext`
- added `LocalUpdater` interface for updaters that work on locally cloned repositories
- added `gpg_key_path`, `gpg_key_passphrase`, `github_access_token`, `gitlab_access_token`, and `azure_devops_access_token` configuration fields
- added multi-token authentication retry for batch mode git push operations
- added transport auto-detection (SSH vs HTTPS) for batch mode push

### Changed

- changed Go, Python, and JavaScript updater batch scripts to contain only language-specific operations (removed git clone, commit, and push from batch bash scripts)
- changed all six updaters (Terraform, Pipeline, Dockerfile, Go, Python, JavaScript) to implement the `LocalUpdater` interface for local filesystem operations in the batch pipeline
- changed batch mode (`run` command) to use a clone-based pipeline with centralized git operations (clone, branch, commit, push) instead of per-updater git management
- changed remote file checker to use `langforge`'s shared `fileutil.NewFileChecker()`, `IsGlobPattern()`, and `ExtractExtension()` utilities
- changed the Go module dependencies to their latest versions
- changed token resolution to use `gitforge`'s shared `ResolveTokenFromEnv()` and `TokenEnvHint()`, eliminating duplicated env var mapping logic
- changed version fetchers to use `langforge`'s shared `pkg/infrastructure/versions` package, eliminating duplicated HTTP fetch logic

### Removed

- removed `internal/infrastructure/repositories/versions/` package (moved to `langforge`)

## [0.8.0] - 2026-03-12

### Added

- added GPG and SSH commit signing support in local mode (reads from git config `commit.gpgsign` and `gpg.format`)
- added SSH push support in local mode (auto-detected from remote URL)

### Changed

- changed `serviceTypeToProviderName()` to use `gitforge`'s shared `ServiceTypeToProviderName()`, eliminating cross-CLI duplication
- changed commit signing resolution to use `gitforge`'s shared `ResolveSignerFromGitConfig()`, eliminating cross-CLI duplication
- changed local mode push to use `gitforge`'s adapter pattern instead of hardcoded provider-username map, supporting SSH and HTTPS with auth method retry
- changed push transport detection and auth retry to use `gitforge`'s shared `PushWithTransportDetection()`, eliminating cross-CLI duplication
- changed the Go version to `1.26.1` and updated all module dependencies

## [0.7.0] - 2026-03-09

### Added

- added `Dockerfile` updater for detecting and upgrading base image versions in `FROM` clauses via Docker Hub API
- added pipeline updater for detecting and upgrading hardcoded language versions in CI/CD configuration files (GitHub Actions and Azure DevOps YAML templates)
- added shared version fetcher package for Go, Python, Node.js, Java, and Terraform latest version resolution

### Changed

- upgraded `gitforge` dependency from `v0.1.1` to `v0.2.0`, bringing Azure DevOps PR creation fixes and GPG signing improvements
- upgraded `langforge` dependency to `v0.2.0` and removed local `replace` directive

### Fixed

- fixed the Go updater deps-only branch name to use `chore/upgrade-go-deps` instead of embedding the Go version number

## [0.6.0] - 2026-03-06

### Added

- added `LocalGitContext` wrapper in `internal/infrastructure/repositories/gitlocal/` using go-git for branch creation, clean check, staging, committing, and pushing (replacing bash-generated git commands in local mode)
- added `RemoteFileChecker` and `DetectRemote` utilities in `internal/support/` bridging `langforge`'s `FileChecker` abstraction with `gitforge`'s's remote provider API
- added `github.com/rios0rios0/langforge` dependency for centralized language detection
- added unit tests for `LocalGitContext` covering all public methods with BDD pattern

### Changed

- changed Go, Python, and JavaScript local-mode updaters to use `LocalGitContext` (go-git) for git operations instead of generating bash scripts for branch creation, clean check, commit, and push
- changed all `gitforge`'s import paths to the new DDD `pkg/` structure (e.g. `domain/entities` → `pkg/global/domain/entities`, `infrastructure/providers/github` → `pkg/providers/infrastructure/github`)
- changed local-mode bash scripts to contain only language-specific operations (auth setup, dependency upgrades, Dockerfile updates, changelog updates)
- changed the Go module dependencies to their latest versions
- replaced `projectType` enum and `switch`/`case` dispatch in `runLocalUpgrade` and `generatePRContent` with mapper pattern using `langforge`'s `Language` constants
- replaced custom `ProviderRegistry` with a thin wrapper around `gitforge`'s's `ProviderRegistry`, delegating factory registration and provider creation while adding `FileAccessProvider` type assertion
- replaced duplicated per-updater `Detect()` logic with `langforge`'s `DetectWith` + `RemoteFileChecker` abstraction across all 4 updaters (Go, Python, JavaScript, Terraform)
- replaced hardcoded `detectProjectType()` in local mode with `langforge`'s `LanguageRegistry.Detect()` for centralized language detection
- replaced inline `parseRemoteURL`, `parseAzureDevOpsURL`, and `parseStandardGitURL` with `gitforge`'s's `ParseRemoteURL` to consolidate duplicated code
- replaced local `ProviderConfig` struct, `ResolveToken()`, and `FindConfigFile()` with `gitforge`'s's shared implementations
- replaced raw struct literals in tests with `testkit`'s builders for consistent test data construction

### Fixed

- fixed `exhaustive` findings by adding missing `Language` and `ServiceType` keys to mapper functions in local command
- fixed `gochecknoglobals` finding by converting `InsertChangelogEntry` from function variable to regular function
- fixed `gochecknoglobals` findings by converting global map variables to function returns
- fixed `revive` `context-as-argument` finding by reordering `DetectRemote` parameters so `context.Context` is first

## [0.5.0] - 2026-02-14

### Added

- added `github.com/rios0rios0/testkit`'s dependency for test builders
- added build tags (`//go:build unit`) to all test double files
- added entity builders (`RepositoryBuilder`, `DependencyBuilder`) following `testkit`'s `BaseBuilder` pattern in `test/domain/entitybuilders/`
- added organized test doubles in `test/domain/commanddoubles/` and `test/infrastructure/repositorydoubles/`

### Changed

- changed the Terraform updater PR description to show a compact summary instead of a full table when there are more than 5 dependency upgrades
- created `RunController` and `LocalController` as cobra CLI adapters in `internal/infrastructure/controllers/`
- extracted `RunCommand` from `application/service.go` and `LocalCommand` from `cmd/local.go` into `internal/domain/commands/`
- introduced `AppInternal` to aggregate all controllers via DIG injection
- introduced `Controller` interface with `GetBind()` and `Execute()` following separation of concerns principles
- moved all code under `internal/` package for proper Go encapsulation
- moved entry point from `main.go` to `cmd/autoupdate/main.go` with separate `dig.go` for DI bootstrap
- refactored config loading from `config/` package into `internal/domain/entities/settings.go`
- refactored entire project to follow DDD/Clean Architecture patterns
- replaced manual registry-based dependency injection with `go.uber.org/dig` container
- restructured domain layer into `entities/`, `commands/`, and `repositories/` packages
- restructured infrastructure layer into `controllers/` and `repositories/` packages
- split `domain/models.go` into per-entity files under `internal/domain/entities/`

### Fixed

- fixed a potential nil pointer dereference in the Terraform HCL parser when `ParseHCL` returned a nil file without diagnostics errors

## [0.4.0] - 2026-02-12

### Added

- added JavaScript updater supporting npm, yarn, and pnpm projects (auto-detected via `lockfiles`), with automatic `.nvmrc`/`.node-version` and Dockerfile `node:` image tag updates
- added Python and JavaScript support to the standalone local mode (`autoupdate .`), with automatic project type detection
- added Python updater supporting `requirements.txt` and `pyproject.toml` projects, with automatic `.python-version` and Dockerfile `python:` image tag updates
- added container image reference scanning in `.hcl` (Terragrunt) files, detecting patterns like `relayer_http_image = "relayer-http:0.7.0"` and upgrading them to the latest Git tag from the same organization

### Changed

- changed the Terraform updater to scan both `.tf` and `.hcl` files, supporting mixed Terraform module and container image dependency upgrades in a single PR
- changed the local mode to auto-detect Go, Python, and JavaScript projects instead of requiring `go.mod`

## [0.3.0] - 2026-02-12

### Added

- added automatic `Dockerfile` image tag update when the Go version is upgraded, searching all `Dockerfiles` in the project tree (`Dockerfile`, `Dockerfile.*`, `*.Dockerfile`)

## [0.2.2] - 2026-02-10

### Fixed

- fixed a bug that created empty PRs where only `CHANGELOG.md` changed

## [0.2.1] - 2026-02-09

### Changed

- corrected Markdown formatting of Go-updater generated changelog entries

## [0.2.0] - 2026-02-09

### Added

- added `--token` flag for explicit auth token override in local mode
- added automatic CHANGELOG.md entry insertion when target repositories contain a changelog following the Keep a Changelog format (both Go and Terraform updaters)
- added dual branch naming patterns for the Go updater (`chore/upgrade-go-X.Y.Z` for version bumps, `chore/upgrade-deps-X.Y.Z` for dependency-only updates)
- added shared `InsertChangelogEntry` domain helper for Keep a Changelog manipulation
- added standalone local mode (`autoupdate .`) to update a local repository directly, auto-detecting the Git provider from the remote URL

### Changed

- changed the Go updater to re-apply the Go version after `go mod tidy` in case it normalizes three-part versions
- changed the Go updater to use portable `sed` with redirect-and-move instead of `sed -i` for cross-platform compatibility (GNU/BSD)
- changed the Go updater to verify `sed` modifications and handle missing `go` directives before setting version-update status flags
- changed the Terraform updater branch naming to use `chore/upgrade-` prefix format

## [0.1.3] - 2026-02-09

### Fixed

- fixed the bug with the GoLang PRs removing the minor version while updating

## [0.1.2] - 2026-02-07

### Changed

- changed the code to ensure Git user identity is configured for committing

## [0.1.1] - 2026-02-07

### Fixed

- fixed the bug with the Azure DevOps formatting wrong URLs

## [0.1.0] - 2026-02-07

### Added

- added Clean Architecture project structure (`domain/`, `application/`, `infrastructure/`, `cmd/`)
- added YAML-based configuration with environment variable expansion for tokens
- added comprehensive test suite with hand-crafted test doubles (spies, stubs, dummies)
- added extensible updater plugin interface with Terraform and Go implementations
- added multi-provider support (GitHub, GitLab, Azure DevOps) for repository discovery and PR creation
- added project boilerplate (`CHANGELOG.md`, `Makefile`, `LICENSE`, `.editorconfig`, `.gitignore`, `.github/`)

### Changed

- changed CLI to use a single `run` command with `--config`, `--dry-run`, and `--verbose` flags
- changed dependency management to use interface-based design for providers and updaters
- redesigned from single Azure DevOps provider to multi-provider architecture

### Removed

- removed separate `scan`, `list`, and `upgrade` CLI commands
- removed tightly coupled Azure DevOps-only implementation
