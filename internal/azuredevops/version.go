package azuredevops

import (
	"sort"
	"strings"

	"golang.org/x/mod/semver"
)

// sortVersionsDescending sorts version strings in descending order (newest first)
func sortVersionsDescending(versions []string) {
	sort.Slice(versions, func(i, j int) bool {
		v1 := normalizeVersion(versions[i])
		v2 := normalizeVersion(versions[j])

		// Use semver comparison if both are valid semver
		if semver.IsValid(v1) && semver.IsValid(v2) {
			return semver.Compare(v1, v2) > 0
		}

		// Fall back to string comparison
		return versions[i] > versions[j]
	})
}

// normalizeVersion ensures version has 'v' prefix for semver compatibility
func normalizeVersion(version string) string {
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}
