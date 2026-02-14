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

### Changed

- changed the Terraform updater PR description to show a compact summary instead of a full table when there are more than 5 dependency upgrades
- refactored entire project to follow DDD/Clean Architecture patterns matching the terra project structure
- moved all code under `internal/` package for proper Go encapsulation
- restructured domain layer into `entities/`, `commands/`, and `repositories/` packages
- restructured infrastructure layer into `controllers/` and `repositories/` packages
- replaced manual registry-based dependency injection with `go.uber.org/dig` container
- introduced `Controller` interface with `GetBind()` and `Execute()` following terra's pattern
- introduced `AppInternal` to aggregate all controllers via DIG injection
- moved entry point from `main.go` to `cmd/autoupdate/main.go` with separate `dig.go` for DI bootstrap
- refactored config loading from `config/` package into `internal/domain/entities/settings.go`
- split `domain/models.go` into per-entity files under `internal/domain/entities/`
- extracted `RunCommand` from `application/service.go` and `LocalCommand` from `cmd/local.go` into `internal/domain/commands/`
- created `RunController` and `LocalController` as cobra CLI adapters in `internal/infrastructure/controllers/`

### Added

- added `github.com/rios0rios0/testkit` dependency for test builders
- added entity builders (`RepositoryBuilder`, `DependencyBuilder`) following testkit `BaseBuilder` pattern in `test/domain/entitybuilders/`
- added organized test doubles in `test/domain/commanddoubles/` and `test/infrastructure/repositorydoubles/`
- added build tags (`//go:build unit`) to all test double files

### Fixed

- fixed a potential nil pointer dereference in the Terraform HCL parser when `ParseHCL` returned a nil file without diagnostics errors

## [0.4.0] - 2026-02-12

### Added

- added JavaScript updater supporting npm, yarn, and pnpm projects (auto-detected via `lockfiles`), with automatic `.nvmrc`/`.node-version` and Dockerfile `node:` image tag updates
- added Python and JavaScript support to the standalone local mode (`autoupdate .`), with automatic project type detection
- added Python updater supporting `requirements.txt` and `pyproject.toml` projects, with automatic `.python-version` and Dockerfile `python:` image tag updates
- added container image reference scanning in `.hcl` (Terragrunt) files, detecting patterns like `relayer_http_image = "relayer-http:0.7.0"` and upgrading them to the latest Git tag from the same organisation

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

- changed the Go updater to re-apply the Go version after `go mod tidy` in case it normalises three-part versions
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
