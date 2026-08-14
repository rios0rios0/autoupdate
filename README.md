<h1 align="center">autoupdate</h1>
<p align="center">
    <a href="https://github.com/rios0rios0/autoupdate/releases/latest">
        <img src="https://img.shields.io/github/release/rios0rios0/autoupdate.svg?style=for-the-badge&logo=github" alt="Latest Release"/></a>
    <a href="https://github.com/rios0rios0/autoupdate/blob/main/LICENSE">
        <img src="https://img.shields.io/github/license/rios0rios0/autoupdate.svg?style=for-the-badge&logo=github" alt="License"/></a>
    <a href="https://github.com/rios0rios0/autoupdate/actions/workflows/default.yaml">
        <img src="https://img.shields.io/github/actions/workflow/status/rios0rios0/autoupdate/default.yaml?branch=main&style=for-the-badge&logo=github" alt="Build Status"/></a>
    <a href="https://sonarcloud.io/summary/overall?id=rios0rios0_autoupdate">
        <img src="https://img.shields.io/sonar/coverage/rios0rios0_autoupdate?server=https%3A%2F%2Fsonarcloud.io&style=for-the-badge&logo=sonarqubecloud" alt="Coverage"/></a>
    <a href="https://sonarcloud.io/summary/overall?id=rios0rios0_autoupdate">
        <img src="https://img.shields.io/sonar/quality_gate/rios0rios0_autoupdate?server=https%3A%2F%2Fsonarcloud.io&style=for-the-badge&logo=sonarqubecloud" alt="Quality Gate"/></a>
    <a href="https://www.bestpractices.dev/projects/12021">
        <img src="https://img.shields.io/cii/level/12021?style=for-the-badge&logo=opensourceinitiative" alt="OpenSSF Best Practices"/></a>
</p>

A self-hosted Dependabot alternative that automatically discovers repositories, detects outdated dependencies across multiple ecosystems, and creates Pull Requests to upgrade them.

## Features

- **Standalone Local Mode**: Run `autoupdate .` on any local repo -- auto-detects the Git provider from the remote URL, upgrades dependencies, and creates a PR
- **Multi-Provider**: Supports GitHub, GitLab, and Azure DevOps as Git hosting providers
- **API-Based Discovery**: Automatically discovers all repositories in an organization, group, or user account
- **Extensible Updaters**: Plugin-based architecture for dependency ecosystems -- see [Supported Ecosystems](#supported-ecosystems)
- **Changelog Integration**: Automatically updates `CHANGELOG.md` (Keep a Changelog format) when the target repository has one, or writes a [chlog](https://github.com/luizjhonata/chlog) fragment when the repository uses that format instead -- see [Changelog Formats](#changelog-formats)
- **Cronjob-Ready**: Designed to run unattended on a schedule for daily dependency updates
- **Dry Run Mode**: Preview all changes before creating any PRs
- **Flexible Filtering**: Run against a specific provider, organization, or updater

## Supported Ecosystems

| Ecosystem      | Detected by                            | What it does                                                                                                                                                    |
|----------------|----------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Terraform      | `*.tf`                                 | Detects Git-based module sources with `?ref=` tags, upgrades to latest tag                                                                                       |
| Go             | `go.mod`                               | Upgrades the Go version in every `go.mod` (root and nested modules), running `go get -u -t ./...` and `go mod tidy` in each                                       |
| Python         | `pyproject.toml`, `requirements.txt`   | Upgrades through the manager the repository already uses -- PDM or pip -- never migrating it from one to the other                                               |
| JavaScript     | `package.json`                         | Upgrades with the package manager the lockfile names (`pnpm`, `yarn`, or `npm`) and bumps `.nvmrc`/`.node-version`                                                |
| Dart/Flutter   | `pubspec.yaml`                         | Runs `dart pub upgrade --major-versions` (or `flutter pub` for a Flutter project), raising the constraints in `pubspec.yaml`, and bumps the `.fvmrc` SDK pin       |
| Ruby           | `Gemfile`, `*.gemspec`                 | Runs `bundle update` and bumps `.ruby-version`                                                                                                                   |
| Java           | `build.gradle`, `pom.xml`              | Upgrades dependencies through Gradle or Maven, whichever the repository builds with                                                                              |
| C#             | `*.csproj`, `*.sln`                    | Upgrades NuGet package references                                                                                                                                |
| Dockerfile     | `Dockerfile`                           | Upgrades base image tags, verifying each against the registry                                                                                                    |
| Pipeline/CI    | pipeline YAML                          | Upgrades pinned action and template versions                                                                                                                     |

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

# Number of repositories to process in parallel within an organization.
# Processing is I/O-bound (clone + remote API calls), so a small fan-out
# shortens large-organization runs significantly. Omit it (or set 0) to use
# the built-in default of 4; set 1 to process repositories sequentially. The
# --concurrency CLI flag overrides this value when provided.
concurrency: 4

# Skip specific repos globally without touching each project. Patterns
# are right-anchored against the canonical key:
#   - GitHub/GitLab: <org>/<repo>
#   - Azure DevOps:  <org>/<project>/<repo>
# Glob wildcards (*, ?, [...]) follow path.Match semantics and do not
# cross "/". A bare name matches the repo's trailing segment.
exclude_repos:
  - 'ContosoSecurity/frontend/opensearch-dashboards'  # exact ADO path
  - '*/oui'                                         # any org or org/project ending in /oui
  - 'rios0rios0/private-fork'                       # exact GitHub path

# The updaters section is optional. All 10 updaters (terraform, golang, python,
# javascript, dart, ruby, java, csharp, pipeline, dockerfile) are enabled by default.
# Default config is fetched from GitHub and merged with your overrides.
# Only specify what you want to change:
updaters:
  terraform:
    auto_complete: true
  python:
    enabled: false
```

### Skipping a Single Repository (Per-Repo Opt-Out)

Drop a `.autoupdate.yaml` in the **target repository's root** to opt that
project out of automated updates without touching the global config:

```yaml
# .autoupdate.yaml in the target repo
skip: true
reason: 'fork of upstream; rebase manually before any update'
```

The `reason` is optional but is logged when the skip fires, so reviewers
can tell at a glance why a repo is being passed over. The file is
honored in both `autoupdate run` (read via the provider API on the
default branch) and `autoupdate .` (read directly from disk). Use it for
forks you maintain by hand, frozen branches, or any project where
automated PRs would create more work than they save.

### Changelog Formats

AutoUpdate records what it changed in the target repository's changelog, and
picks the format from what that repository itself commits. Nothing has to be
configured on the AutoUpdate side.

**Keep a Changelog (default).** The entries are inserted into the
`## [Unreleased]` / `### Changed` section of `CHANGELOG.md`. A repository
without a `CHANGELOG.md` simply gets no changelog change.

**chlog.** [chlog](https://github.com/luizjhonata/chlog) replaces the shared
`CHANGELOG.md` with one small YAML file per change under `.changes/unreleased/`,
which is what removes changelog merge conflicts between concurrent pull
requests. Editing `[Unreleased]` in such a repository would put automated pull
requests straight back into conflict, so AutoUpdate writes a fragment instead
and leaves `CHANGELOG.md` alone:

```yaml
# .changes/unreleased/1748359200-a1b2.yaml
kind: Changed
body: changed the Go module dependencies to their latest versions
time: 2026-07-29T14:30:00Z
```

A repository is treated as a chlog user when it commits a `.chlog.yaml` (or
`.chlog.yml`), or when it merely carries a `.changes/unreleased/` directory --
chlog works without a configuration file. When the file is present, its
`changesDir`, `unreleasedDir` and `kinds` are honored, so a project that
renamed its directories or its `Changed` label still gets valid fragments. The
`chlog merge` step later compiles them into the changelog as usual.

Both formats work in every ecosystem and in both local and batch mode.

### Token Resolution

Tokens support three formats:

- **Inline**: `token: "ghp_abc123"`
- **Environment variable**: `token: "${GITHUB_TOKEN}"` (expanded at runtime)
- **File path**: `token: "/run/secrets/github_token"` (read from file if path exists)

## Usage

### Standalone Local Mode

Update a single local repository directly -- no config file needed. The provider is auto-detected from the `origin` remote URL:

```bash
# Update the current directory
autoupdate .

# Update a specific path
autoupdate /path/to/repo

# Dry run -- preview what would happen
autoupdate --dry-run .

# Use an explicit token (overrides env var detection)
autoupdate --token ghp_abc123 .
```

Auth tokens are read automatically from standard environment variables:

| Provider    | Environment Variables                          |
|-------------|------------------------------------------------|
| GitHub      | `GITHUB_TOKEN` or `GH_TOKEN`                  |
| Azure DevOps| `AZURE_DEVOPS_EXT_PAT` or `SYSTEM_ACCESSTOKEN` |
| GitLab      | `GITLAB_TOKEN` or `GL_TOKEN`                   |

### Batch Mode (Config-Driven)

Discover and update all repositories across providers using a config file:

```bash
# Run all configured providers and updaters
autoupdate run

# Dry run -- preview what would happen
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

## Command Reference

### Global Flags

| Flag        | Short | Description                                              |
|-------------|-------|----------------------------------------------------------|
| `--config`  | `-c`  | Path to config file (auto-detected)                      |
| `--token`   |       | Auth token for the Git provider (overrides env var)      |
| `--dry-run` |       | Preview changes without applying                         |
| `--verbose` | `-v`  | Enable verbose output                                    |
| `--skip-cleanup` |  | Keep the dated branches from previous runs instead of deleting them and closing their PRs |

### `autoupdate [path]`

Standalone local mode -- update a single repository in place.

### `autoupdate run`

Batch mode -- discover and update repositories using a config file.

| Flag            | Description                                                       |
|-----------------|------------------------------------------------------------------|
| `--provider`    | Only process this provider (github/gitlab/azuredevops)           |
| `--org`         | Only process this organization/group                             |
| `--updater`     | Only run this updater (terraform, golang, python, javascript, dart, ruby, java, csharp, pipeline, dockerfile) |
| `--concurrency` | Repositories processed in parallel (default 4; 1 = sequential)   |

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE) for details.
