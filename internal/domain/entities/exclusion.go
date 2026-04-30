package entities

import (
	"path"
	"strings"
)

// RepoKey returns the canonical lookup key used by exclusion patterns.
// Azure DevOps repos are addressed as <org>/<project>/<name> because the
// project segment is part of the URL path; everything else is <org>/<name>.
// All segments are lowercased so patterns are case-insensitive.
func RepoKey(repo Repository) string {
	parts := []string{repo.Organization}
	if repo.Project != "" {
		parts = append(parts, repo.Project)
	}
	parts = append(parts, repo.Name)

	for i, p := range parts {
		parts[i] = strings.ToLower(p)
	}
	return strings.Join(parts, "/")
}

// MatchesExcludePattern reports whether the repository's canonical key
// matches any of the supplied glob patterns. Each pattern is matched
// right-anchored against the segments of the key, so:
//
//   - `opensearch-dashboards` matches the repo named `opensearch-dashboards`
//     regardless of org/project prefix.
//   - `*/oui` matches `<anything>/oui`, including the trailing two
//     segments of an Azure DevOps `org/project/oui` key.
//   - `zestsecurity/frontend/opensearch-dashboards` only matches that
//     exact ADO path.
//
// `*` follows `path.Match` semantics (it does not cross `/`). The first
// matching pattern is returned alongside the boolean so callers can log
// which rule excluded the repository.
func MatchesExcludePattern(repo Repository, patterns []string) (bool, string) {
	if len(patterns) == 0 {
		return false, ""
	}

	keyParts := strings.Split(RepoKey(repo), "/")
	for _, pattern := range patterns {
		trimmed := strings.TrimSpace(pattern)
		if trimmed == "" {
			continue
		}
		normalized := strings.ToLower(trimmed)
		patternParts := strings.Split(normalized, "/")

		if len(patternParts) > len(keyParts) {
			continue
		}

		suffix := strings.Join(keyParts[len(keyParts)-len(patternParts):], "/")
		if matched, err := path.Match(normalized, suffix); err == nil && matched {
			return true, trimmed
		}
	}
	return false, ""
}

// IsRepoExcluded is a convenience wrapper that reads the exclude list off
// the global Settings struct. A nil Settings or empty list disables the
// check, mirroring the rest of the configuration surface.
func (s *Settings) IsRepoExcluded(repo Repository) (bool, string) {
	if s == nil {
		return false, ""
	}
	return MatchesExcludePattern(repo, s.ExcludeRepos)
}
