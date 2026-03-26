package javascript

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	logger "github.com/sirupsen/logrus"
)

// hasOnlyLockfileVersionChanges returns true when the only uncommitted
// modifications in repoDir are cosmetic lockfile version-field syncs
// (e.g., npm update syncing the root "version" in package-lock.json to
// match package.json) with zero actual dependency changes.
func hasOnlyLockfileVersionChanges(ctx context.Context, repoDir string) bool {
	changedFiles := gitChangedFiles(ctx, repoDir)
	if len(changedFiles) == 0 {
		return false
	}

	hasLockfile := false
	for _, f := range changedFiles {
		switch f {
		case "package-lock.json":
			hasLockfile = true
			if !isPackageLockOnlyVersionSync(ctx, repoDir) {
				return false
			}
		case "CHANGELOG.md":
			// Tolerate auto-generated changelog updates alongside cosmetic
			// lockfile syncs — writeChangelogUpdate copies the changelog
			// whenever git status is non-empty, even for cosmetic-only changes.
		default:
			// Any non-lockfile change (or yarn.lock / pnpm-lock.yaml which
			// do not carry a project version field) is a real change.
			return false
		}
	}

	return hasLockfile
}

// gitChangedFiles returns the list of modified (unstaged) file paths
// relative to the repository root.
func gitChangedFiles(ctx context.Context, repoDir string) []string {
	cmd := exec.CommandContext(ctx, "git", "diff", "--name-only")
	cmd.Dir = repoDir
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	raw := strings.TrimSpace(string(output))
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}

// isPackageLockOnlyVersionSync compares the current package-lock.json
// against the HEAD version. It returns true when the only differences
// are the root-level "version" field and the packages[""]["version"]
// field — i.e., no dependency versions, resolved URLs, or integrity
// hashes changed.
func isPackageLockOnlyVersionSync(ctx context.Context, repoDir string) bool {
	original, err := gitShowHEAD(ctx, repoDir, "package-lock.json")
	if err != nil {
		return false
	}

	current, err := os.ReadFile(filepath.Clean(filepath.Join(repoDir, "package-lock.json")))
	if err != nil {
		return false
	}

	var origMap, currMap map[string]json.RawMessage
	if json.Unmarshal(original, &origMap) != nil || json.Unmarshal(current, &currMap) != nil {
		return false
	}

	// Zero-out the root "version" field in both maps.
	delete(origMap, "version")
	delete(currMap, "version")

	// Zero-out packages[""]["version"] (lockfileVersion >= 2).
	clearPackagesRootVersion(origMap)
	clearPackagesRootVersion(currMap)

	origNorm, err1 := json.Marshal(origMap)
	currNorm, err2 := json.Marshal(currMap)
	if err1 != nil || err2 != nil {
		return false
	}

	return string(origNorm) == string(currNorm)
}

// clearPackagesRootVersion removes the "version" key from the
// packages[""] entry in a package-lock.json map.
func clearPackagesRootVersion(m map[string]json.RawMessage) {
	pkgsRaw, ok := m["packages"]
	if !ok {
		return
	}

	var pkgs map[string]json.RawMessage
	if json.Unmarshal(pkgsRaw, &pkgs) != nil {
		return
	}

	rootPkgRaw, ok := pkgs[""]
	if !ok {
		return
	}

	var rootPkg map[string]json.RawMessage
	if json.Unmarshal(rootPkgRaw, &rootPkg) != nil {
		return
	}

	delete(rootPkg, "version")

	rootPkgBytes, err := json.Marshal(rootPkg)
	if err != nil {
		return
	}
	pkgs[""] = rootPkgBytes

	pkgsBytes, err := json.Marshal(pkgs)
	if err != nil {
		return
	}
	m["packages"] = pkgsBytes
}

// gitShowHEAD returns the content of a file at HEAD.
func gitShowHEAD(ctx context.Context, repoDir, filePath string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "show", "HEAD:"+filePath) //nolint:gosec // filePath is always a hardcoded constant from internal callers
	cmd.Dir = repoDir
	return cmd.Output()
}

// revertWorkingTreeChanges discards all unstaged changes in the
// working tree, restoring it to the HEAD state.
func revertWorkingTreeChanges(ctx context.Context, repoDir string) {
	cmd := exec.CommandContext(ctx, "git", "checkout", "--", ".")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		logger.Warnf("[javascript] Failed to revert working tree changes: %v", err)
	}
}
