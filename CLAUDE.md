# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Does

AutoUpdate is a self-hosted Dependabot alternative. It discovers repositories across Git providers (GitHub, GitLab, Azure DevOps), detects outdated dependencies, and creates Pull Requests with version upgrades. Supports Terraform, Go, Python, and JavaScript ecosystems.

Two modes: **local** (`autoupdate [path]`) updates a single repo, **batch** (`autoupdate run`) reads a config file and processes multiple repos/providers.

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
- **Domain commands**: `internal/domain/commands/` — `LocalCommand` (single repo) and `RunCommand` (batch mode)
- **Domain ports**: `internal/domain/repositories/` — `UpdaterRepository` and `ProviderRepository` interfaces
- **Infrastructure adapters**: `internal/infrastructure/repositories/` — updater implementations per ecosystem
- **Registries**: `provider_registry.go` (abstract factory for Git providers) and `updater_registry.go` (holds all updater implementations)

### Key External Libraries

- **gitforge** (`rios0rios0/gitforge`): Abstraction over GitHub/GitLab/Azure DevOps APIs. Domain entities (`Repository`, `PullRequest`, `Dependency`) are re-exported as type aliases from gitforge.
- **langforge** (`rios0rios0/langforge`): Language/ecosystem detection (detects Go, Python, JS, Terraform from file patterns).
- **testkit** (`rios0rios0/testkit`): Base builder pattern for test data construction.

### Adding a New Updater

1. Create a new package under `internal/infrastructure/repositories/<ecosystem>/`
2. Implement `UpdaterRepository` interface (`Name()`, `Detect()`, `CreateUpdatePRs()`)
3. Register in `internal/infrastructure/repositories/container.go`

### Commit Signing

When `commit.gpgsign=true` is set in git config, commits are automatically signed using GPG or SSH (based on `gpg.format`). The signing key is read from `user.signingkey`. GPG passphrase is read from `GPG_PASSPHRASE` env var.

### Config System

Auto-discovery searches `.`, `.config`, `configs`, `$HOME`, `$HOME/.config` for `autoupdate.yaml` / `.autoupdate.yaml`. Tokens support inline values, `${ENV_VAR}` expansion, and file path resolution.

## Testing Conventions

- Build tag: `//go:build unit` on every test file
- External test packages (e.g., `commands_test` for package `commands`)
- BDD structure with `// given`, `// when`, `// then` comments
- Parallel execution via `t.Parallel()` and `t.Run()` subtests
- Test doubles in `test/domain/commanddoubles/` (stubs) and `test/domain/entitybuilders/` (builders)
- Uses `stretchr/testify` for assertions — prefer stubs over mocks
