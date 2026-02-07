package golang

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GoRelease represents a Go release from the download API
type GoRelease struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

// UpgradeParams contains parameters for upgrading a Go project
type UpgradeParams struct {
	CloneURL      string // HTTPS clone URL with PAT embedded
	DefaultBranch string // e.g., "refs/heads/main"
	BranchName    string // New branch name for the upgrade
	GoVersion     string // Target Go version (e.g., "1.25")
	PAT           string // Azure DevOps PAT (passed to config.sh via env)
	HasConfigSH   bool   // Whether config.sh exists at root
	Verbose       bool
}

// UpgradeResult contains the result of a Go project upgrade
type UpgradeResult struct {
	BranchName   string
	HasChanges   bool
	Output       string
	ChangedFiles []string
}

// FetchLatestGoVersion fetches the latest stable Go version from go.dev
// Returns the major.minor version string (e.g., "1.25")
func FetchLatestGoVersion() (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("https://go.dev/dl/?mode=json")
	if err != nil {
		return "", fmt.Errorf("failed to fetch Go versions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var releases []GoRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", fmt.Errorf("failed to parse Go versions: %w", err)
	}

	// Find the first stable release
	for _, release := range releases {
		if release.Stable {
			// Version format: "go1.25.7" -> "1.25"
			version := strings.TrimPrefix(release.Version, "go")
			parts := strings.Split(version, ".")
			if len(parts) >= 2 {
				return parts[0] + "." + parts[1], nil
			}
			return version, nil
		}
	}

	return "", fmt.Errorf("no stable Go version found")
}

// UpgradeGoRepo clones a Go repository, upgrades its dependencies, and pushes changes
func UpgradeGoRepo(ctx context.Context, params UpgradeParams) (*UpgradeResult, error) {
	result := &UpgradeResult{
		BranchName: params.BranchName,
	}

	// Create temp directory for the clone
	tmpDir, err := os.MkdirTemp("", "autoupdate-go-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "repo")

	// Build the upgrade script
	script := buildUpgradeScript(params, repoDir)

	// Write script to temp file
	scriptPath := filepath.Join(tmpDir, "upgrade.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return nil, fmt.Errorf("failed to write upgrade script: %w", err)
	}

	// Find the go binary
	goBinary, err := findGoBinary()
	if err != nil {
		return nil, fmt.Errorf("Go not found: %w", err)
	}

	// Prepare environment
	env := append(os.Environ(),
		"AZURE_DEVOPS_PAT="+params.PAT,
		"CLONE_URL="+params.CloneURL,
		"BRANCH_NAME="+params.BranchName,
		"GO_VERSION="+params.GoVersion,
		"REPO_DIR="+repoDir,
		"GO_BINARY="+goBinary,
		"DEFAULT_BRANCH="+cleanBranchName(params.DefaultBranch),
	)

	// Execute the upgrade script
	cmd := exec.CommandContext(ctx, "bash", scriptPath)
	cmd.Dir = tmpDir
	cmd.Env = env

	output, err := cmd.CombinedOutput()
	result.Output = string(output)

	if err != nil {
		return result, fmt.Errorf("upgrade script failed: %w\nOutput:\n%s", err, result.Output)
	}

	// Check if there were changes (look for "CHANGES_PUSHED" marker in output)
	result.HasChanges = strings.Contains(result.Output, "CHANGES_PUSHED=true")

	return result, nil
}

func buildUpgradeScript(params UpgradeParams, repoDir string) string {
	var sb strings.Builder

	sb.WriteString("#!/bin/bash\n")
	sb.WriteString("set -euo pipefail\n\n")

	// Create a temporary gitconfig to avoid modifying global config
	sb.WriteString("# Set up isolated git config for Azure DevOps auth\n")
	sb.WriteString("TEMP_GITCONFIG=$(mktemp)\n")
	sb.WriteString("cp ~/.gitconfig \"$TEMP_GITCONFIG\" 2>/dev/null || true\n")
	sb.WriteString("echo '[url \"https://pat:'\"${AZURE_DEVOPS_PAT}\"'@dev.azure.com/\"]' >> \"$TEMP_GITCONFIG\"\n")
	sb.WriteString("echo '    insteadOf = https://dev.azure.com/' >> \"$TEMP_GITCONFIG\"\n")
	sb.WriteString("# Also handle SSH-style URLs used in some Azure DevOps setups\n")
	sb.WriteString("echo '[url \"https://pat:'\"${AZURE_DEVOPS_PAT}\"'@dev.azure.com/\"]' >> \"$TEMP_GITCONFIG\"\n")
	sb.WriteString("echo '    insteadOf = git@ssh.dev.azure.com:v3/' >> \"$TEMP_GITCONFIG\"\n")
	sb.WriteString("export GIT_CONFIG_GLOBAL=\"$TEMP_GITCONFIG\"\n")
	sb.WriteString("trap 'rm -f \"$TEMP_GITCONFIG\"' EXIT\n\n")

	// Clone the repository
	sb.WriteString("# Clone the repository\n")
	sb.WriteString("echo \"Cloning repository...\"\n")
	sb.WriteString("git clone --depth=1 --branch \"$DEFAULT_BRANCH\" \"$CLONE_URL\" \"$REPO_DIR\" 2>&1\n")
	sb.WriteString("cd \"$REPO_DIR\"\n\n")

	// Create a new branch
	sb.WriteString("# Create upgrade branch\n")
	sb.WriteString("git checkout -b \"$BRANCH_NAME\" 2>&1\n\n")

	// Source config.sh if it exists
	if params.HasConfigSH {
		sb.WriteString("# Source config.sh to set up private package settings\n")
		sb.WriteString("echo \"Running config.sh...\"\n")
		sb.WriteString("if [ -f \"./config.sh\" ]; then\n")
		sb.WriteString("    source ./config.sh\n")
		sb.WriteString("fi\n\n")
	}

	// Update Go version
	sb.WriteString("# Update Go version in go.mod\n")
	sb.WriteString("echo \"Updating Go version to $GO_VERSION...\"\n")
	sb.WriteString("\"$GO_BINARY\" mod edit -go=\"$GO_VERSION\" 2>&1\n\n")

	// Run go get -u ./...
	sb.WriteString("# Update all dependencies\n")
	sb.WriteString("echo \"Running go get -u ./...\"\n")
	sb.WriteString("\"$GO_BINARY\" get -u ./... 2>&1 || echo \"WARNING: go get -u had some errors (continuing anyway)\"\n\n")

	// Run go mod tidy
	sb.WriteString("# Tidy up\n")
	sb.WriteString("echo \"Running go mod tidy...\"\n")
	sb.WriteString("\"$GO_BINARY\" mod tidy 2>&1 || echo \"WARNING: go mod tidy had some errors (continuing anyway)\"\n\n")

	// Check if vendor directory exists, if so run go mod vendor
	sb.WriteString("# If vendor directory exists, update it\n")
	sb.WriteString("if [ -d \"vendor\" ]; then\n")
	sb.WriteString("    echo \"Running go mod vendor...\"\n")
	sb.WriteString("    \"$GO_BINARY\" mod vendor 2>&1 || echo \"WARNING: go mod vendor had some errors\"\n")
	sb.WriteString("fi\n\n")

	// Check for changes and commit/push if any
	sb.WriteString("# Check for changes\n")
	sb.WriteString("if [ -n \"$(git status --porcelain)\" ]; then\n")
	sb.WriteString("    echo \"Changes detected, committing and pushing...\"\n")
	sb.WriteString("    git add -A\n")
	sb.WriteString("    git commit -m \"chore(deps): upgrade Go version to $GO_VERSION and update dependencies\"\n")
	sb.WriteString("    git push origin \"$BRANCH_NAME\" 2>&1\n")
	sb.WriteString("    echo \"CHANGES_PUSHED=true\"\n")
	sb.WriteString("else\n")
	sb.WriteString("    echo \"No changes detected.\"\n")
	sb.WriteString("    echo \"CHANGES_PUSHED=false\"\n")
	sb.WriteString("fi\n")

	return sb.String()
}

// findGoBinary locates the Go binary on the system
func findGoBinary() (string, error) {
	// First, try the standard PATH lookup
	if path, err := exec.LookPath("go"); err == nil {
		return path, nil
	}

	// Try common locations
	commonPaths := []string{
		"/usr/local/go/bin/go",
		"/usr/bin/go",
		"/snap/bin/go",
	}

	// Also check GVM locations
	home, _ := os.UserHomeDir()
	if home != "" {
		// Check gvm directories
		gvmDir := filepath.Join(home, ".gvm", "gos")
		if entries, err := os.ReadDir(gvmDir); err == nil {
			for i := len(entries) - 1; i >= 0; i-- { // reverse to get latest first
				entry := entries[i]
				if entry.IsDir() && strings.HasPrefix(entry.Name(), "go") {
					goBin := filepath.Join(gvmDir, entry.Name(), "bin", "go")
					if _, err := os.Stat(goBin); err == nil {
						return goBin, nil
					}
				}
			}
		}

		// Check goenv
		goenvDir := filepath.Join(home, ".goenv", "shims")
		goenvBin := filepath.Join(goenvDir, "go")
		commonPaths = append(commonPaths, goenvBin)
	}

	for _, p := range commonPaths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("go binary not found in PATH or common locations")
}

// cleanBranchName removes refs/heads/ prefix if present
func cleanBranchName(branch string) string {
	return strings.TrimPrefix(branch, "refs/heads/")
}
