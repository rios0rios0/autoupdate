# autoupdate

A CLI tool that automatically scans Azure DevOps repositories for Terraform module dependencies, detects outdated versions, and creates Pull Requests to upgrade them.

## Features

- **Project Discovery**: Automatically discovers all Azure DevOps projects you have access to
- **Dependency Scanning**: Parses Terraform files to find Git-based module dependencies
- **Version Detection**: Identifies available versions (tags) for module repositories
- **Automatic Upgrades**: Creates PRs to upgrade outdated dependencies
- **Dry Run Mode**: Preview changes before applying them
- **Flexible Filtering**: Filter by project, repository, or specific modules

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/rios0rios0/autoupdate.git
cd autoupdate

# Build
go build -o autoupdate .

# Or install globally
go install .
```

### Binary Releases

Download pre-built binaries from the [Releases](https://github.com/rios0rios0/autoupdate/releases) page.

## Configuration

### Authentication

The tool requires a Personal Access Token (PAT) with the following permissions:
- **Code**: Read & Write (to scan repos and create branches)
- **Pull Request Contribute**: Read & Write (to create PRs)

Set your credentials via environment variables or CLI flags:

```bash
# Environment variables (recommended)
export AZURE_DEVOPS_ORG="https://dev.azure.com/MyOrganization"
export AZURE_DEVOPS_PAT="your-personal-access-token"

# Or use CLI flags
autoupdate --organization "https://dev.azure.com/MyOrg" --pat "your-pat" scan
```

## Usage

### Scan for Dependencies

Discover all Terraform module dependencies across your repositories:

```bash
# Scan all projects
autoupdate scan

# Scan a specific project
autoupdate scan --project "MyProject"

# Scan a specific repository
autoupdate scan --project "MyProject" --repo "infrastructure"

# Verbose output
autoupdate scan -v
```

### List Dependencies with Status

View all dependencies and their upgrade status:

```bash
# List all dependencies
autoupdate list

# Show only outdated dependencies
autoupdate list --outdated

# Output as markdown (great for documentation)
autoupdate list --output markdown

# Output as JSON (for automation)
autoupdate list --output json
```

### Upgrade Dependencies

Create PRs to upgrade outdated dependencies:

```bash
# Preview what would be upgraded (dry run)
autoupdate upgrade --dry-run

# Create upgrade PRs for all outdated dependencies
autoupdate upgrade

# Upgrade a specific module
autoupdate upgrade --module "terraform-module-networking"

# Upgrade to a specific version
autoupdate upgrade --module "terraform-module-networking" --version "v2.0.0"

# Filter by project/repository
autoupdate upgrade --project "MyProject" --repo "infrastructure"

# Set auto-complete on created PRs
autoupdate upgrade --auto-complete

# Custom PR settings
autoupdate upgrade --pr-title "Custom title" --target-branch "develop"
```

## How It Works

### Dependency Detection

The tool scans for Terraform module blocks with Git-based sources:

```hcl
module "networking" {
  source = "git::https://dev.azure.com/MyOrg/Modules/_git/terraform-module-networking?ref=v1.2.3"
  # ... module configuration
}
```

Supported source formats:
- `git::https://dev.azure.com/org/project/_git/repo?ref=tag`
- `git::ssh://git@ssh.dev.azure.com/v3/org/project/repo?ref=tag`
- GitHub, GitLab, Bitbucket URLs with `?ref=` parameter

### Module Repository Discovery

The tool identifies module repositories by:
1. Naming conventions (`terraform-module-*`, `tf-module-*`, `module-*`)
2. Repository structure (presence of `main.tf` and `variables.tf` at root)

### Version Comparison

Versions are compared using semantic versioning when possible:
- `v1.0.0` < `v1.1.0` < `v2.0.0`
- Falls back to string comparison for non-semver tags

## Examples

### Example Workflow

```bash
# 1. First, scan to see what dependencies exist
autoupdate scan

# Output:
# 📁 MyProject/app-infrastructure
#    └─ networking @ v1.0.0 (from /terraform/main.tf)
#    └─ storage @ v2.1.0 (from /terraform/main.tf)

# 2. List with status to see what needs upgrading
autoupdate list --outdated

# Output:
# Project     Repository          Module      Source      Current  Latest   Status
# MyProject   app-infrastructure  networking  networking  v1.0.0   v1.2.0   🟡 Minor update

# 3. Preview the upgrades
autoupdate upgrade --dry-run

# Output:
# [DRY RUN] Would create PR for MyProject/app-infrastructure:
#    - networking: v1.0.0 -> v1.2.0

# 4. Create the PRs
autoupdate upgrade

# Output:
# ✅ Created PR #123 for MyProject/app-infrastructure: https://dev.azure.com/...
```

### CI/CD Integration

Run as a scheduled job to keep dependencies up to date:

```yaml
# Azure Pipelines example
schedules:
  - cron: "0 6 * * 1"  # Every Monday at 6 AM
    displayName: Weekly dependency check
    branches:
      include:
        - main

steps:
  - script: |
      autoupdate upgrade --auto-complete
    env:
      AZURE_DEVOPS_ORG: $(System.CollectionUri)
      AZURE_DEVOPS_PAT: $(System.AccessToken)
    displayName: Create upgrade PRs
```

## Command Reference

### Global Flags

| Flag             | Short | Description                      |
|------------------|-------|----------------------------------|
| `--organization` | `-o`  | Azure DevOps organization URL    |
| `--pat`          | `-p`  | Personal Access Token            |
| `--dry-run`      |       | Preview changes without applying |
| `--verbose`      | `-v`  | Enable verbose output            |

### scan

Scan repositories for Terraform dependencies.

| Flag        | Description               |
|-------------|---------------------------|
| `--project` | Filter by project name    |
| `--repo`    | Filter by repository name |

### list

List dependencies with their versions.

| Flag         | Description                          |
|--------------|--------------------------------------|
| `--outdated` | Show only outdated dependencies      |
| `--output`   | Output format: table, json, markdown |
| `--project`  | Filter by project name               |
| `--repo`     | Filter by repository name            |

### upgrade

Create PRs to upgrade dependencies.

| Flag               | Description                           |
|--------------------|---------------------------------------|
| `--target-branch`  | Target branch for PRs (default: main) |
| `--commit-message` | Custom commit message                 |
| `--pr-title`       | Custom PR title                       |
| `--pr-description` | Custom PR description                 |
| `--auto-complete`  | Set auto-complete on PRs              |
| `--module`         | Upgrade specific module only          |
| `--version`        | Upgrade to specific version           |
| `--project`        | Filter by project name                |
| `--repo`           | Filter by repository name             |

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see [LICENSE](LICENSE) for details.
