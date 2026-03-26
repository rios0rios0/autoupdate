//go:build unit

package javascript

import "context"

// HasOnlyLockfileVersionChanges is exported for testing.
func HasOnlyLockfileVersionChanges(ctx context.Context, repoDir string) bool {
	return hasOnlyLockfileVersionChanges(ctx, repoDir)
}

// IsPackageLockOnlyVersionSync is exported for testing.
func IsPackageLockOnlyVersionSync(ctx context.Context, repoDir string) bool {
	return isPackageLockOnlyVersionSync(ctx, repoDir)
}

// RevertWorkingTreeChanges is exported for testing.
func RevertWorkingTreeChanges(ctx context.Context, repoDir string) {
	revertWorkingTreeChanges(ctx, repoDir)
}
