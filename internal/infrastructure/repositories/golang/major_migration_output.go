package golang

import (
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// maxNamedMissingPackages is how many of the packages a new major no longer
// has a note names before it only counts the rest.
const maxNamedMissingPackages = 2

// toolchainProgressPrefixes start the lines the go command prints about what
// it is doing rather than about what went wrong.
//
// They come first -- a module is downloaded, or looked for, before anything can
// fail on it -- and that is how a note came to read `go: downloading ...` or
// `go: finding module for package ...` where the reason belonged.
//
//nolint:gochecknoglobals // read-only lookup table
var toolchainProgressPrefixes = []string{
	"go: downloading ",
	"go: finding module for package ",
	"go: found ",
	"go: extracting ",
	"go: added ",
	"go: upgraded ",
	"go: downgraded ",
	"go: removed ",
	"go: warning: ",
}

// missingPackagePattern matches the two ways the go command says the module a
// package path points into has no such package: `go mod tidy`, after looking
// the module up ("does not contain package"), and `go mod vendor`, reading the
// module graph tidy left behind ("no required module provides package").
//
// Both are an answer rather than a failure to get one. "cannot find module
// providing package" is deliberately absent: it is also what a lookup the
// registry did not answer looks like, and a transient error read as a moved
// package would be baked into a branch that is never rebuilt.
var missingPackagePattern = regexp.MustCompile(
	`(?:does not contain package|no required module provides package) ([^\s;]+)`,
)

// toolchainErrors reduces a go command's output to the errors it reported, one
// string per error.
//
// The go command wraps one error over several lines and indents every line
// after the first -- an import chain, or `parsing go.mod:` followed by what it
// found there -- so an indented line belongs to the error above it. Progress
// lines, and the `# <package>` header a build prints above a package's
// diagnostics, are not errors, and neither is anything indented under them.
func toolchainErrors(output string) []string {
	reports := make([]string, 0)
	current := -1

	for line := range strings.SplitSeq(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if line[0] == ' ' || line[0] == '\t' {
			if current >= 0 {
				reports[current] += " " + trimmed
			}
			continue
		}

		if isToolchainProgress(trimmed) {
			current = -1
			continue
		}

		reports = append(reports, trimmed)
		current = len(reports) - 1
	}

	for i, report := range reports {
		reports[i] = collapseWhitespace(report)
	}

	return reports
}

// isToolchainProgress reports whether a line says what the go command was
// doing rather than what went wrong.
func isToolchainProgress(line string) bool {
	if strings.HasPrefix(line, "# ") {
		return true
	}
	if strings.HasPrefix(line, "go: module ") && strings.Contains(line, " is deprecated") {
		return true
	}

	for _, prefix := range toolchainProgressPrefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}

	return false
}

// migrationDetail reduces a command's output to the first error in it, which
// is the reason it failed.
func migrationDetail(output string) string {
	reports := toolchainErrors(output)
	if len(reports) == 0 {
		return ""
	}

	return withoutGoPrefix(reports[0])
}

// withoutGoPrefix drops the `go: ` the go command opens an error with: the
// note it ends up in already says which go command failed.
func withoutGoPrefix(report string) string {
	return strings.TrimPrefix(report, "go: ")
}

// missingNewMajorPackages splits a go command's errors into the packages under
// the new major's path that it could not find, and the first error that is
// anything else -- a registry that did not answer, a checksum mismatch, a
// package outside the new major. The second is empty when there is none.
func missingNewMajorPackages(output, nextPath string) ([]string, string) {
	missing := make([]string, 0)

	for _, report := range toolchainErrors(output) {
		match := missingPackagePattern.FindStringSubmatch(report)
		if match == nil || !underModulePath(match[1], nextPath) {
			return nil, report
		}
		if !slices.Contains(missing, match[1]) {
			missing = append(missing, match[1])
		}
	}

	slices.Sort(missing)
	return missing, ""
}

// underModulePath reports whether a package path is the module path itself or
// a package below it.
func underModulePath(packagePath, modulePath string) bool {
	return packagePath == modulePath || strings.HasPrefix(packagePath, modulePath+"/")
}

// nestedModuleCandidates lists the module paths that could provide a package
// below the new major's path other than the new major itself: the package path,
// and every path between it and the module, longest first.
func nestedModuleCandidates(packagePath, modulePath string) []string {
	candidates := make([]string, 0)
	for candidate := packagePath; candidate != modulePath && underModulePath(candidate, modulePath); {
		candidates = append(candidates, candidate)
		candidate = path.Dir(candidate)
	}

	return candidates
}

// moduleListsVersions reports whether `go list -m -versions` named any version
// of the module, which is how it says the module exists. A module that does not
// is answered with its path alone, and exit status zero.
func moduleListsVersions(output, modulePath string) bool {
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[0] == modulePath {
			return true
		}
	}

	return false
}

// missingPackagesNote says which packages the new major no longer has: the
// reason a migration that had to leave them unresolved does not build, said
// more plainly than the compiler would say it.
func missingPackagesNote(nextPath string, missing []string) string {
	names := make([]string, 0, maxNamedMissingPackages)
	for _, pkg := range missing[:min(len(missing), maxNamedMissingPackages)] {
		name := strings.TrimPrefix(pkg, nextPath+"/")
		names = append(names, "`"+name+"`")
	}

	var listed string
	switch {
	case len(missing) == 1:
		listed = "package " + names[0]
	case len(missing) == maxNamedMissingPackages:
		listed = "packages " + names[0] + " and " + names[1]
	default:
		listed = "packages " + strings.Join(names, ", ") + " and " +
			strconv.Itoa(len(missing)-maxNamedMissingPackages) + " more"
	}

	return "it does not build yet: `" + nextPath + "` has no " + listed
}
