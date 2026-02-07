# AutoUpdate

AutoUpdate is a Go CLI tool that automatically discovers repositories across multiple Git providers (GitHub, GitLab, Azure DevOps), scans them for outdated dependencies, and creates Pull Requests with version upgrades. It currently supports Terraform modules and Go projects, with an extensible updater plugin interface for future ecosystems.

Always reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.

## Working Effectively

### Bootstrap, Build, and Test
- Install dependencies: `go mod download` -- takes <1 second (after first download)
- Build the binary: `make build` -- takes ~35 seconds first time, <1 second after. NEVER CANCEL. Set timeout to 60+ minutes.
- Run tests: `go test ./...` -- takes <1 second (cached), ~7 seconds clean. NEVER CANCEL. Set timeout to 30+ minutes.
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
- Run directly: `go run .`
- Run built binary: `./bin/autoupdate`
- Test help command: `./bin/autoupdate --help`
- Test run command help: `./bin/autoupdate run --help`

### Installation
- Build first: `make build`
- Install system-wide: `make install` (copies to `/usr/local/bin/autoupdate`)

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

## Common Tasks

### Repository Structure (Clean Architecture)
```
autoupdate/
├── main.go                        # Entry point
├── cmd/                           # CLI layer (Cobra commands)
│   ├── root.go                    # Root command with global flags
│   └── run.go                     # Main "run" command
├── domain/                        # Domain layer (interfaces + models)
│   ├── models.go                  # Core data structures
│   ├── provider.go                # Provider interface
│   └── updater.go                 # Updater interface
├── application/                   # Application layer (use cases)
│   ├── service.go                 # UpdateService orchestrator
│   └── service_test.go
├── config/                        # Configuration loading
│   ├── config.go                  # YAML config, env var expansion
│   └── config_test.go
├── infrastructure/                # Infrastructure layer (implementations)
│   ├── provider/                  # Git provider implementations
│   │   ├── registry.go            # Provider registry
│   │   ├── github/github.go       # GitHub provider
│   │   ├── gitlab/gitlab.go       # GitLab provider
│   │   └── azuredevops/azuredevops.go  # Azure DevOps provider
│   └── updater/                   # Dependency updater implementations
│       ├── registry.go            # Updater registry
│       ├── terraform/terraform.go # Terraform module updater
│       └── golang/golang.go       # Go dependency updater
├── test/                          # Test doubles (spies, stubs, dummies)
│   └── doubles.go
├── autoupdate.yaml                # Configuration template
├── Makefile                       # Build automation
├── go.mod                         # Go module definition
└── .github/workflows/             # CI/CD pipeline
```

### Key Files and Their Purpose
- `main.go` - Entry point, calls `cmd.Execute()`
- `cmd/root.go` - CLI root command with `--config`, `--dry-run`, `--verbose` flags
- `cmd/run.go` - Main command: loads config, builds registries, runs update service
- `domain/provider.go` - `Provider` interface for Git hosting services
- `domain/updater.go` - `Updater` interface for dependency ecosystems
- `application/service.go` - Orchestrates: discover repos -> detect ecosystems -> create PRs
- `config/config.go` - YAML config with env var expansion and token file reading
- `autoupdate.yaml` - Configuration template with provider/updater examples

### Configuration System
- Config file location: `./autoupdate.yaml` or specified via `--config` flag
- Supports multiple providers with organizations and tokens
- Token resolution: inline values, `${ENV_VAR}` expansion, file paths
- Updaters can be enabled/disabled with per-updater `auto_complete` and `target_branch`

### Provider Support
- **GitHub**: API-based repo discovery and PR creation via `go-github`
- **GitLab**: API-based repo discovery and PR creation via `gitlab-org/api/client-go`
- **Azure DevOps**: API-based repo discovery and PR creation via REST API

### Updater Support
- **Terraform**: Scans `.tf` files for Git module sources, checks tags for newer versions
- **Go**: Detects `go.mod` files, clones repos, runs `go get -u` and `go mod tidy`

### Testing Infrastructure
- Comprehensive unit tests in `*_test.go` files across all layers
- Uses testify for assertions (assert/require) — no mock framework
- Hand-crafted test doubles in `test/doubles.go` (SpyProvider, SpyUpdater, DummyProvider, DummyUpdater)
- BDD-style tests with Given/When/Then comments
- Parallel test execution via `t.Run` subtests

### Development Workflow
1. Make code changes
2. Run `go fmt ./...` to format
3. Run `go vet ./...` to check for issues
4. Run `go test ./...` to verify tests pass
5. Run `make build` to ensure clean build
6. Test binary with `./bin/autoupdate --help`
7. Test functional operation with `./bin/autoupdate run --dry-run`
8. (Optional) Run full linting with pipeline script for final validation

### Common Development Commands
```bash
# Full development cycle
go mod download && make build && go test ./... && ./bin/autoupdate --help

# Quick test cycle
go test ./... && make build

# Format and lint (quick)
go fmt ./... && go vet ./...

# Full lint using pipeline scripts
git clone https://github.com/rios0rios0/pipelines.git /tmp/pipelines 2>/dev/null || true
/tmp/pipelines/global/scripts/GoLang/GoLangCI-Lint/run.sh

# Clean rebuild
rm -rf bin && make build
```

Always validate any changes by building and testing the actual binary functionality, not just unit tests.
