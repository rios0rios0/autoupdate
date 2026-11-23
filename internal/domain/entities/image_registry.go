package entities

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// ErrInvalidImageRegistry is returned for an image_registries rule that cannot be used.
var ErrInvalidImageRegistry = errors.New("invalid image registry rule")

// globStar is the path segment that spans any number of directories in an image_registries
// path pattern.
const globStar = "**"

// registryHostPattern is a registry's host, with an optional port: what `docker login`
// takes, and what goes between `https://` and `/v2/`. No scheme and no path, because the
// check always speaks HTTPS and an image's path below the host is Namespace's to name.
var registryHostPattern = regexp.MustCompile(
	`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)*(?::[0-9]{1,5})?$`,
)

// namespacePattern is the repository path grammar of the OCI Distribution specification:
// lowercase components separated by `/`, each made of alphanumerics joined by `.`, `_`,
// `__` or runs of `-`.
var namespacePattern = regexp.MustCompile(
	`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*)*$`,
)

// ImageRegistryRule says which container registry the image pins in some files are pulled
// from.
//
// A Terragrunt file pins an image by its bare name -- `api_image = "api:1.4.0"` -- while the
// registry the cluster pulls it from is named somewhere else, so the tag a pin is moved to
// comes from the tags of the repository that builds the image. That repository can tag a
// release the registry never received: a build that failed, an image published only to
// another registry. A rule lets the run check: a pin in a file the rule covers is only moved
// to a tag the registry holds.
//
// The rules are the operator's alone. The check asks the registry with the operator's own
// registry credentials, so which hosts it talks to is not something a repository being
// updated may decide.
type ImageRegistryRule struct {
	// Registry is the registry's host, with an optional port: `registry.example.com` or
	// `registry.example.com:5000`. It is asked over HTTPS, through the OCI Distribution API.
	Registry string `yaml:"registry"`

	// Namespace is the path the registry keeps the images under, if any: with `team`, an
	// image pinned as `api:1.4.0` is looked up as `team/api`.
	Namespace string `yaml:"namespace"`

	// Repositories limits the rule to the repositories these patterns match, read the way
	// `exclude_repos` reads its patterns. Empty means every repository.
	Repositories []string `yaml:"repositories"`

	// Paths limits the rule to the files these patterns match, relative to the repository
	// root. `*`, `?` and `[...]` match within one directory, as [path.Match] does, and a `**`
	// segment matches any number of directories, none included. Empty means every file.
	Paths []string `yaml:"paths"`
}

// ImageRepository returns the repository path the registry keeps image under.
func (r ImageRegistryRule) ImageRepository(image string) string {
	namespace := strings.Trim(r.Namespace, "/")
	if namespace == "" {
		return image
	}
	return namespace + "/" + image
}

// Covers reports whether the rule applies to the file at filePath in repo.
func (r ImageRegistryRule) Covers(repo Repository, filePath string) bool {
	if len(r.Repositories) > 0 {
		if matched, _ := MatchesExcludePattern(repo, r.Repositories); !matched {
			return false
		}
	}
	if len(r.Paths) == 0 {
		return true
	}

	name := normalizeRepositoryPath(filePath)
	for _, pattern := range r.Paths {
		if matchPathGlob(normalizeRepositoryPath(pattern), name) {
			return true
		}
	}
	return false
}

// ImageRegistriesFor returns the rules covering the file at filePath in repo, in
// configuration order and with each registry and namespace named once.
//
// A file can pull from several registries -- one Terragrunt file holding the image pins of
// a development and a production environment -- and a tag is safe to pin there only when
// every one of them holds it, so a caller checks against all of them rather than the first.
func ImageRegistriesFor(rules []ImageRegistryRule, repo Repository, filePath string) []ImageRegistryRule {
	var covering []ImageRegistryRule
	seen := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		if !rule.Covers(repo, filePath) {
			continue
		}

		key := strings.ToLower(rule.Registry) + "/" + strings.Trim(rule.Namespace, "/")
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		covering = append(covering, rule)
	}
	return covering
}

// ConfiguredImageRegistries returns the operator's image_registries rules, or nothing when
// there are no settings at all.
func ConfiguredImageRegistries(settings *Settings) []ImageRegistryRule {
	if settings == nil {
		return nil
	}
	return settings.ImageRegistries
}

// ValidateImageRegistries checks every image_registries rule before a run relies on it.
//
// A rule that cannot be read is an error rather than a rule that matches nothing: the
// operator wrote it to stop pins from moving to tags a registry does not hold, and a typo
// that silently disabled it would leave them exactly as unprotected as before.
func ValidateImageRegistries(rules []ImageRegistryRule) error {
	for i, rule := range rules {
		if err := validateImageRegistryRule(rule); err != nil {
			return fmt.Errorf("image_registries[%d]: %w", i, err)
		}
	}
	return nil
}

// validateImageRegistryRule checks one rule.
func validateImageRegistryRule(rule ImageRegistryRule) error {
	if !registryHostPattern.MatchString(rule.Registry) {
		return fmt.Errorf(
			"%w: registry %q must be a host name with an optional port, without a scheme or a path",
			ErrInvalidImageRegistry, rule.Registry,
		)
	}

	if namespace := strings.Trim(rule.Namespace, "/"); namespace != "" && !namespacePattern.MatchString(namespace) {
		return fmt.Errorf(
			"%w: namespace %q must be lowercase path components the registry can name",
			ErrInvalidImageRegistry, rule.Namespace,
		)
	}

	for _, pattern := range rule.Repositories {
		if _, err := path.Match(strings.TrimSpace(pattern), "probe"); err != nil {
			return fmt.Errorf("%w: repositories pattern %q: %w", ErrInvalidImageRegistry, pattern, err)
		}
	}

	for _, pattern := range rule.Paths {
		if err := validatePathGlob(pattern); err != nil {
			return fmt.Errorf("%w: paths pattern %q: %w", ErrInvalidImageRegistry, pattern, err)
		}
	}

	return nil
}

// validatePathGlob checks every segment of a path pattern the way [path.Match] would read it.
func validatePathGlob(pattern string) error {
	normalized := normalizeRepositoryPath(pattern)
	if normalized == "" {
		return errors.New("the pattern is empty")
	}

	for segment := range strings.SplitSeq(normalized, "/") {
		if segment == globStar {
			continue
		}
		if _, err := path.Match(segment, "probe"); err != nil {
			return err
		}
	}
	return nil
}

// normalizeRepositoryPath turns a path relative to a repository's root into the one form the
// patterns are matched against: slash-separated, with no leading slash and no `.` or `..`
// segments. The forges list files with a leading slash (Azure DevOps) or without one, and a
// walk of a clone returns the platform's separator.
func normalizeRepositoryPath(name string) string {
	slashed := strings.ReplaceAll(strings.TrimSpace(name), `\`, "/")
	if slashed == "" {
		return ""
	}
	return strings.TrimPrefix(path.Clean("/"+slashed), "/")
}

// matchPathGlob reports whether name matches pattern, both slash-separated. A `**` segment
// matches any number of segments, none included; every other segment is matched by
// [path.Match], so `*` never crosses a `/`.
func matchPathGlob(pattern, name string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

// matchSegments matches a pattern's segments against a name's, consuming both from the left.
func matchSegments(pattern, name []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == globStar {
			rest := pattern[1:]
			for skipped := 0; skipped <= len(name); skipped++ {
				if matchSegments(rest, name[skipped:]) {
					return true
				}
			}
			return false
		}

		if len(name) == 0 {
			return false
		}
		if matched, err := path.Match(pattern[0], name[0]); err != nil || !matched {
			return false
		}
		pattern, name = pattern[1:], name[1:]
	}
	return len(name) == 0
}
