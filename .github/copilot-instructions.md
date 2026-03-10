# AutoUpdate

AutoUpdate is a Go CLI tool that automatically discovers repositories across multiple Git providers (GitHub, GitLab, Azure DevOps), scans them for outdated dependencies, and creates Pull Requests with version upgrades. It supports Terraform modules, Go, Python, and JavaScript projects, with an extensible updater plugin interface for future ecosystems.

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

## Common Tasks

### Repository Structure (Clean Architecture)
```
autoupdate/
├── cmd/
│   └── autoupdate/
│       ├── main.go                # Entry point; builds Cobra root command and subcommands
│       └── dig.go                 # DI container wiring (go.uber.org/dig)
├── configs/
│   └── autoupdate.yaml            # Configuration template
├── internal/
│   ├── app.go                     # AppInternal: aggregates all controllers
│   ├── container.go               # Top-level DIG provider registration (bottom-up)
│   ├── domain/
│   │   ├── commands/              # Domain use-case commands
│   │   │   ├── local_command.go   # LocalCommand: standalone local-repo update
│   │   │   └── run_command.go     # RunCommand: batch multi-provider update
│   │   ├── entities/              # Domain entities and configuration
│   │   │   ├── controller.go      # Controller interface and ControllerBind
│   │   │   ├── dependency.go      # Dependency entity (re-exported from gitforge)
│   │   │   ├── pull_request.go    # PullRequest entity (re-exported from gitforge)
│   │   │   ├── repository.go      # Repository entity (re-exported from gitforge)
│   │   │   ├── settings.go        # Settings: YAML config, env var expansion, auto-discovery
│   │   │   └── update_options.go  # UpdateOptions for updater invocations
│   │   └── repositories/          # Repository interfaces (ports)
│   │       ├── provider_repository.go  # ProviderRepository interface
│   │       └── updater_repository.go   # UpdaterRepository interface
│   └── infrastructure/
│       ├── controllers/           # Cobra CLI controllers (adapters)
│       │   ├── local_controller.go  # LocalController: handles root path argument
│       │   └── run_controller.go    # RunController: handles "run" subcommand
│       └── repositories/          # Infrastructure implementations
│           ├── provider_registry.go  # ProviderRegistry: maps provider type -> factory
│           ├── updater_registry.go   # UpdaterRegistry: holds all updater implementations
│           ├── container.go          # Registers providers/updaters with DIG
│           ├── gitlocal/             # Local Git operations (go-git)
│           ├── golang/               # Go dependency updater
│           ├── javascript/           # JavaScript (npm) dependency updater
│           ├── python/               # Python (pip/pyproject) dependency updater
│           └── terraform/            # Terraform module updater
├── test/
│   └── domain/
│       ├── commanddoubles/        # Stub commands for unit tests
│       │   ├── stub_local_command.go
│       │   └── stub_run_command.go
│       └── entitybuilders/        # Test builders for domain entities
│           ├── dependency_builder.go
│           └── repository_builder.go
├── Makefile                       # Build automation
├── go.mod                         # Go module definition
└── .github/workflows/             # CI/CD pipeline
```

### Key Files and Their Purpose
- `cmd/autoupdate/main.go` - Entry point; builds Cobra root command with `--config`, `--dry-run`, `--verbose`, `--token` flags
- `cmd/autoupdate/dig.go` - DI container wiring using `go.uber.org/dig`
- `internal/app.go` - `AppInternal` aggregates all registered controllers
- `internal/container.go` - Registers all layers bottom-up (repos -> entities -> commands -> controllers)
- `internal/domain/commands/run_command.go` - Orchestrates: discover repos -> detect ecosystems -> create PRs
- `internal/domain/commands/local_command.go` - Standalone mode: detect project type, upgrade deps, create PR
- `internal/domain/entities/settings.go` - YAML config loading, env var expansion (`${VAR}`), token file resolution, auto-discovery
- `internal/infrastructure/controllers/run_controller.go` - Handles `run` subcommand flags and delegates to `RunCommand`
- `internal/infrastructure/controllers/local_controller.go` - Handles root path argument and delegates to `LocalCommand`
- `internal/infrastructure/repositories/container.go` - Wires gitforge providers and all updater repositories
- `configs/autoupdate.yaml` - Configuration template with provider/updater examples

### Configuration System
- Config auto-discovery searches: `.`, `.config`, `configs`, `$HOME`, `$HOME/.config` for `.autoupdate.{yml,yaml}` and `autoupdate.{yml,yaml}`
- Override with `--config` flag or `-c`
- Supports multiple providers with organizations and tokens
- Token resolution: inline values, `${ENV_VAR}` expansion, file paths
- Updaters can be enabled/disabled with per-updater `auto_complete` and `target_branch`

### Commit Signing
When `commit.gpgsign=true` is set in git config (local or global), commits are automatically signed:
- **SSH signing**: detected when `gpg.format=ssh`; reads the signing key path from `user.signingkey`
- **GPG signing**: default when `gpg.format` is unset or `openpgp`; reads the GPG key ID from `user.signingkey` and passphrase from `GPG_PASSPHRASE` environment variable
- Signing is transport-agnostic (embedded in the commit object) and works with HTTP push

### Provider Support
All Git provider implementations come from the `github.com/rios0rios0/gitforge` library:
- **GitHub**: via `gitforge/pkg/providers/infrastructure/github`
- **GitLab**: via `gitforge/pkg/providers/infrastructure/gitlab`
- **Azure DevOps**: via `gitforge/pkg/providers/infrastructure/azuredevops`

### Updater Support
- **Terraform**: Scans `.tf` files for Git module sources, checks tags for newer versions
- **Go**: Detects `go.mod` files, runs `go get -u` and `go mod tidy`
- **Python**: Detects `requirements.txt` or `pyproject.toml`, upgrades pip dependencies
- **JavaScript**: Detects `package.json`, upgrades npm dependencies

### CLI Flags
**Root / global (persistent) flags:**
- `--config`, `-c` — Path to config file (default: auto-detect)
- `--token` — Auth token for the Git provider (overrides env var detection)
- `--dry-run` — Show what would be done without making changes
- `--verbose`, `-v` — Enable verbose output

**`run` subcommand flags:**
- `--provider` — Only process this provider (`github`, `gitlab`, `azuredevops`)
- `--org` — Only process this organization/group
- `--updater` — Only run this updater (`terraform`, `golang`, `python`, `javascript`)

### Dependency Injection
The project uses `go.uber.org/dig` for dependency injection. Registration happens bottom-up in `internal/container.go`: infrastructure repositories -> domain entities -> domain commands -> controllers -> `AppInternal`.

### Entity Types
Core entity types (`Repository`, `File`, `BranchInput`, `PullRequestInput`, `PullRequest`, `FileChange`) are re-exported as type aliases from the `gitforge` library in `internal/domain/entities/`.

### Testing Infrastructure
- All unit tests are tagged with `//go:build unit` and must be run with `-tags unit`
- Uses testify for assertions (`assert`/`require`) — no mock framework
- Test stubs in `test/domain/commanddoubles/` (`StubLocalCommand`, `StubRunCommand`)
- Test builders in `test/domain/entitybuilders/` (`DependencyBuilder`, `RepositoryBuilder`)
- Uses `github.com/rios0rios0/testkit` for additional test helpers
- BDD-style tests with Given/When/Then comments
- Parallel test execution via `t.Run` subtests

### Development Workflow
1. Make code changes
2. Run `go fmt ./...` to format
3. Run `go vet ./...` to check for issues
4. Run `go test -tags unit ./...` to verify tests pass
5. Run `make build` to ensure clean build
6. Test binary with `./bin/autoupdate --help`
7. Test functional operation with `./bin/autoupdate run --dry-run`
8. (Optional) Run full linting with pipeline script for final validation

### Common Development Commands
```bash
# Full development cycle
go mod download && make build && go test -tags unit ./... && ./bin/autoupdate --help

# Quick test cycle
go test -tags unit ./... && make build

# Format and lint (quick)
go fmt ./... && go vet ./...

# Full lint using pipeline scripts
git clone https://github.com/rios0rios0/pipelines.git /tmp/pipelines 2>/dev/null || true
/tmp/pipelines/global/scripts/GoLang/GoLangCI-Lint/run.sh

# Clean rebuild
rm -rf bin && make build

# Run directly without building
go run ./cmd/autoupdate --help
```

Always validate any changes by building and testing the actual binary functionality, not just unit tests.
