package upgrader

import (
	"regexp"
	"strings"

	"github.com/rios0rios0/autoupdate/internal/azuredevops"
	"github.com/rios0rios0/autoupdate/internal/scanner"
	"golang.org/x/mod/semver"
)

// UpgradeTask represents a single dependency upgrade to be performed
type UpgradeTask struct {
	Project     azuredevops.Project
	Repository  azuredevops.Repository
	FilePath    string
	Dependency  scanner.ModuleDependency
	CurrentVer  string
	NewVersion  string
	FileContent string
}

// IsNewerVersion compares two version strings and returns true if newVersion is newer
func IsNewerVersion(currentVersion, newVersion string) bool {
	// Normalize versions
	current := normalizeVersion(currentVersion)
	new := normalizeVersion(newVersion)

	// If both are valid semver, use semver comparison
	if semver.IsValid(current) && semver.IsValid(new) {
		return semver.Compare(new, current) > 0
	}

	// Fall back to string comparison for non-semver versions
	return newVersion > currentVersion
}

// normalizeVersion ensures version has 'v' prefix for semver compatibility
func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

// ApplyUpgrade modifies the file content to use the new version
func ApplyUpgrade(content string, dep scanner.ModuleDependency, newVersion string) string {
	// Build pattern to match the specific module's source line
	// We need to be careful to only replace the specific version in the specific module

	// First, try to find and replace using the full source URL
	oldSource := buildSourceWithVersion(dep.Source, dep.Version)
	newSource := buildSourceWithVersion(dep.Source, newVersion)

	// Simple string replacement
	if strings.Contains(content, oldSource) {
		return strings.Replace(content, oldSource, newSource, 1)
	}

	// Try regex-based replacement for more flexible matching
	// Match: source = "...?ref=oldVersion" within the module block
	pattern := regexp.MustCompile(
		`(module\s+"` + regexp.QuoteMeta(dep.Name) + `"\s*\{[^}]*source\s*=\s*"[^"]*\?ref=)` +
			regexp.QuoteMeta(dep.Version) + `([^"]*")`)

	if pattern.MatchString(content) {
		return pattern.ReplaceAllString(content, "${1}"+newVersion+"${2}")
	}

	// Fallback: try to replace just the ref value
	refPattern := regexp.MustCompile(`(\?ref=)` + regexp.QuoteMeta(dep.Version) + `([^&"\s]*)`)

	// Find all matches and replace only the one in the correct context
	// This is a simplified approach - in production you might want more precise matching
	return refPattern.ReplaceAllStringFunc(content, func(match string) string {
		return strings.Replace(match, dep.Version, newVersion, 1)
	})
}

// buildSourceWithVersion creates a source URL with a specific version
func buildSourceWithVersion(source, version string) string {
	if strings.Contains(source, "?ref=") {
		// Replace existing ref
		pattern := regexp.MustCompile(`\?ref=[^&"\s]+`)
		return pattern.ReplaceAllString(source, "?ref="+version)
	}

	// Add ref parameter
	if strings.Contains(source, "?") {
		return source + "&ref=" + version
	}
	return source + "?ref=" + version
}

// VersionDiff represents the difference between current and available version
type VersionDiff struct {
	Current   string
	Available string
	IsMajor   bool
	IsMinor   bool
	IsPatch   bool
}

// AnalyzeVersionDiff determines the type of version change
func AnalyzeVersionDiff(current, new string) VersionDiff {
	diff := VersionDiff{
		Current:   current,
		Available: new,
	}

	// Normalize versions
	currentNorm := normalizeVersion(current)
	newNorm := normalizeVersion(new)

	if !semver.IsValid(currentNorm) || !semver.IsValid(newNorm) {
		// Can't determine diff type for non-semver versions
		return diff
	}

	currentMajor := semver.Major(currentNorm)
	newMajor := semver.Major(newNorm)

	if currentMajor != newMajor {
		diff.IsMajor = true
		return diff
	}

	// Extract minor versions (semver package doesn't have Minor function)
	currentParts := strings.Split(strings.TrimPrefix(currentNorm, "v"), ".")
	newParts := strings.Split(strings.TrimPrefix(newNorm, "v"), ".")

	if len(currentParts) >= 2 && len(newParts) >= 2 {
		if currentParts[1] != newParts[1] {
			diff.IsMinor = true
			return diff
		}
	}

	diff.IsPatch = true
	return diff
}

// FilterUpgradesByType filters upgrade tasks by version change type
func FilterUpgradesByType(tasks []UpgradeTask, allowMajor, allowMinor, allowPatch bool) []UpgradeTask {
	var filtered []UpgradeTask

	for _, task := range tasks {
		diff := AnalyzeVersionDiff(task.CurrentVer, task.NewVersion)

		if diff.IsMajor && allowMajor {
			filtered = append(filtered, task)
		} else if diff.IsMinor && allowMinor {
			filtered = append(filtered, task)
		} else if diff.IsPatch && allowPatch {
			filtered = append(filtered, task)
		} else if !diff.IsMajor && !diff.IsMinor && !diff.IsPatch {
			// Unknown version type, include by default
			filtered = append(filtered, task)
		}
	}

	return filtered
}
