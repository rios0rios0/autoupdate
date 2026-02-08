# autoupdate

A self-hosted Dependabot alternative that automatically discovers repositories, detects outdated dependencies across multiple ecosystems, and creates Pull Requests to upgrade them.

## Features

- **Multi-Provider**: Supports GitHub, GitLab, and Azure DevOps as Git hosting providers
- **API-Based Discovery**: Automatically discovers all repositories in an organization, group, or user account
- **Extensible Updaters**: Plugin-based architecture for dependency ecosystems (Terraform modules, Go projects, and more coming)
- **Cronjob-Ready**: Designed to run unattended on a schedule for daily dependency updates
- **Dry Run Mode**: Preview all changes before creating any PRs
- **Flexible Filtering**: Run against a specific provider, organization, or updater

## Supported Ecosystems

| Ecosystem | What it does                                                               |
|-----------|----------------------------------------------------------------------------|
| Terraform | Detects Git-based module sources with `?ref=` tags, upgrades to latest tag |
| Go        | Upgrades Go version in `go.mod`, runs `go get -u ./...` and `go mod tidy`  |

## Installation

### Quick Install (Recommended)

Install `autoupdate` with a single command:

```bash
curl -fsSL https://raw.githubusercontent.com/rios0rios0/autoupdate/main/install.sh | sh
```

Or using wget:

```bash
wget -qO- https://raw.githubusercontent.com/rios0rios0/autoupdate/main/install.sh | sh
```

#### Installation Options

```bash
# Install specific version
curl -fsSL https://raw.githubusercontent.com/rios0rios0/autoupdate/main/install.sh | sh -s -- --version v1.0.0

# Install to custom directory
curl -fsSL https://raw.githubusercontent.com/rios0rios0/autoupdate/main/install.sh | sh -s -- --install-dir /usr/local/bin

# Show what would be installed without doing it
curl -fsSL https://raw.githubusercontent.com/rios0rios0/autoupdate/main/install.sh | sh -s -- --dry-run

# Force reinstallation
curl -fsSL https://raw.githubusercontent.com/rios0rios0/autoupdate/main/install.sh | sh -s -- --force
```

### Download Pre-built Binaries

Download pre-built binaries from the [releases page](https://github.com/rios0rios0/autoupdate/releases).

### Build from Source

```bash
git clone https://github.com/rios0rios0/autoupdate.git
cd autoupdate
make build
```

## Configuration

Create an `autoupdate.yaml` (or `.autoupdate.yaml`) in the current directory, `~/.config/`, or pass it with `--config`.

```yaml
providers:
  - type: github
    token: "${GITHUB_TOKEN}"
    organizations:
      - "my-org"

  - type: azuredevops
    token: "${AZURE_DEVOPS_PAT}"
    organizations:
      - "https://dev.azure.com/MyOrg"

  - type: gitlab
    token: "${GITLAB_TOKEN}"
    organizations:
      - "my-group"

updaters:
  terraform:
    enabled: true
    auto_complete: false
    target_branch: "main"
  golang:
    enabled: true
    target_branch: "main"
```

### Token Resolution

Tokens support three formats:
- **Inline**: `token: "ghp_abc123"`
- **Environment variable**: `token: "${GITHUB_TOKEN}"` (expanded at runtime)
- **File path**: `token: "/run/secrets/github_token"` (read from file if path exists)

## Usage

### Run the Update Engine

```bash
# Run all configured providers and updaters
autoupdate run

# Dry run — preview what would happen
autoupdate run --dry-run

# Only process GitHub repos
autoupdate run --provider github

# Only process a specific organization
autoupdate run --provider github --org my-org

# Only run the Terraform updater
autoupdate run --updater terraform

# Verbose logging
autoupdate run -v
```

### CI/CD Integration (Cronjob)

```yaml
# GitHub Actions example
name: Dependency Updates
on:
  schedule:
    - cron: '0 6 * * 1-5'  # Weekdays at 6 AM

jobs:
  update:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: 'Download Autoupdate'
        run: curl -fsSL https://raw.githubusercontent.com/rios0rios0/autoupdate/main/install.sh | sh -s -- --install-dir .
      - run: ./autoupdate run
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

```yaml
# Azure Pipelines example
schedules:
  - cron: "0 6 * * 1"
    displayName: Weekly dependency check
    branches:
      include:
        - main

steps:
  - script: curl -fsSL https://raw.githubusercontent.com/rios0rios0/autoupdate/main/install.sh | sh -s -- --install-dir .
    displayName: 'Download Autoupdate'
  - script: ./autoupdate run
    env:
      AZURE_DEVOPS_PAT: $(System.AccessToken)
```

## Architecture

```
autoupdate/
├── cmd/                             # CLI layer (Cobra commands)
│   ├── root.go                      # Global flags: --config, --verbose, --dry-run
│   └── run.go                       # Main "run" command
├── domain/                          # Interfaces and models (no dependencies)
│   ├── models.go                    # Repository, Dependency, PullRequest, etc.
│   ├── provider.go                  # Provider interface
│   └── updater.go                   # Updater interface
├── infrastructure/                  # Implementations
│   ├── provider/
│   │   ├── registry.go              # Provider registry
│   │   ├── github/github.go         # GitHub provider
│   │   ├── gitlab/gitlab.go         # GitLab provider
│   │   └── azuredevops/azuredevops.go # Azure DevOps provider
│   └── updater/
│       ├── registry.go              # Updater registry
│       ├── terraform/terraform.go   # Terraform module updater
│       └── golang/golang.go         # Go dependency updater
├── application/
│   └── service.go                   # Orchestration service
├── config/
│   └── config.go                    # YAML config loading
├── main.go                          # Entry point
└── autoupdate.yaml                  # Config template
```

### Adding a New Provider

Implement the `domain.Provider` interface and register it in `cmd/run.go`:

```go
reg.Register("bitbucket", bitbucket.New)
```

### Adding a New Updater

Implement the `domain.Updater` interface and register it in `cmd/run.go`:

```go
reg.Register(npmUpdater.New())
```

## Command Reference

### Global Flags

| Flag        | Short | Description                         |
|-------------|-------|-------------------------------------|
| `--config`  | `-c`  | Path to config file (auto-detected) |
| `--dry-run` |       | Preview changes without applying    |
| `--verbose` | `-v`  | Enable verbose output               |

### run

Run the dependency update engine.

| Flag         | Description                                            |
|--------------|--------------------------------------------------------|
| `--provider` | Only process this provider (github/gitlab/azuredevops) |
| `--org`      | Only process this organization/group                   |
| `--updater`  | Only run this updater (terraform/golang)               |

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see [LICENSE](LICENSE) for details.
