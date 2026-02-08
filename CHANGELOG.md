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
