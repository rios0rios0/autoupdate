package entities

import "strings"

// DependencySubject names what a changelog statement about upgraded dependencies
// is about, in the singular: "Go module", "Docker base image", or, for a version
// pin, "Go version". It is the noun phrase the statement is written around, so
// the constant's value is exactly the text that appears in the changelog.
type DependencySubject string

// Version pins. Each is a single nameless statement: the toolchain a repository
// builds with is one fact, however many files spell it.
const (
	SubjectGoVersion      DependencySubject = "Go version"
	SubjectPythonVersion  DependencySubject = "Python version"
	SubjectNodeVersion    DependencySubject = "Node.js version"
	SubjectFlutterVersion DependencySubject = "Flutter SDK version"
	SubjectRubyVersion    DependencySubject = "Ruby version"
	SubjectJavaVersion    DependencySubject = "Java version"
	SubjectGradleVersion  DependencySubject = "Gradle version"
	SubjectDotnetVersion  DependencySubject = ".NET SDK version"
)

// Named dependencies, listed several to a statement.
//
// The Terraform module, container image and Docker base image phrases are the
// ones the per-upgrade statements already used, which is what lets a statement
// written before this grammar existed be read, and merged, as a one-item list.
// The pipeline subjects deliberately avoid the word "version": autobump's
// release-time de-duplication compares entries by their words with the versions
// stripped, and "changed the Python version" would vanish into a line about the
// Python version a pipeline uses.
const (
	SubjectGoModule          DependencySubject = "Go module"
	SubjectPythonPackage     DependencySubject = "Python package"
	SubjectJavaScriptPackage DependencySubject = "JavaScript package"
	SubjectDartPackage       DependencySubject = "Dart package"
	SubjectRubyGem           DependencySubject = "Ruby gem"
	SubjectMavenDependency   DependencySubject = "Maven dependency"
	SubjectNuGetPackage      DependencySubject = "NuGet package"
	SubjectTerraformModule   DependencySubject = "Terraform module"
	SubjectContainerImage    DependencySubject = "container image"
	SubjectDockerBaseImage   DependencySubject = "Docker base image"
	SubjectPipelineRuntime   DependencySubject = "pipeline runtime"
	SubjectGitHubAction      DependencySubject = "GitHub Action"
)

// dependencyKeyStyle selects how a dependency name is reduced to the key two
// statements are matched on.
type dependencyKeyStyle int

const (
	// keyByName matches names case-insensitively, which suits every ecosystem
	// whose registry treats names that way or never issues two that differ
	// only in case.
	keyByName dependencyKeyStyle = iota
	// keyByPythonName applies the PEP 503 normalization, under which
	// "Foo_Bar", "foo-bar" and "foo.bar" are the same distribution.
	keyByPythonName
	// keyByImageVariant adds the tag variant to the name, so that
	// `golang:1.22` and `golang:1.22-alpine` stay two images rather than
	// folding into one statement that moves from one variant to the other.
	keyByImageVariant
)

// subjectDefinition is one entry of the subject registry.
type subjectDefinition struct {
	subject DependencySubject
	// plural is the phrase for several items; it is empty for a version pin,
	// which is always a single nameless statement.
	plural string
	key    dependencyKeyStyle
}

// dependencySubjects is the closed set of subjects a dependency statement can be
// about, in the order new statements are written: version pins first, then
// named dependencies. Parsing accepts only these, which is what keeps a
// maintainer's sentence such as "changed the default timeout from `30s` to
// `60s`" from ever being read, and rewritten, as a version pin.
//
//nolint:gochecknoglobals // read-only lookup table
var dependencySubjects = []subjectDefinition{
	{subject: SubjectGoVersion},
	{subject: SubjectPythonVersion},
	{subject: SubjectNodeVersion},
	{subject: SubjectFlutterVersion},
	{subject: SubjectRubyVersion},
	{subject: SubjectJavaVersion},
	{subject: SubjectGradleVersion},
	{subject: SubjectDotnetVersion},
	{subject: SubjectGoModule, plural: "Go modules"},
	{subject: SubjectPythonPackage, plural: "Python packages", key: keyByPythonName},
	{subject: SubjectJavaScriptPackage, plural: "JavaScript packages"},
	{subject: SubjectDartPackage, plural: "Dart packages"},
	{subject: SubjectRubyGem, plural: "Ruby gems"},
	{subject: SubjectMavenDependency, plural: "Maven dependencies"},
	{subject: SubjectNuGetPackage, plural: "NuGet packages"},
	{subject: SubjectTerraformModule, plural: "Terraform modules"},
	{subject: SubjectContainerImage, plural: "container images", key: keyByImageVariant},
	{subject: SubjectDockerBaseImage, plural: "Docker base images", key: keyByImageVariant},
	{subject: SubjectPipelineRuntime, plural: "pipeline runtimes"},
	{subject: SubjectGitHubAction, plural: "GitHub Actions"},
}

// IsPin reports whether the subject is a version pin, which is written as a
// single nameless statement.
func (s DependencySubject) IsPin() bool {
	definition, _ := s.definition()
	return definition.plural == ""
}

// phrase returns the noun phrase for the given number of items.
func (s DependencySubject) phrase(count int) string {
	definition, _ := s.definition()
	if count > 1 && definition.plural != "" {
		return definition.plural
	}
	return string(s)
}

// order returns the position new statements about this subject are written at.
// A subject outside the registry sorts last.
func (s DependencySubject) order() int {
	_, index := s.definition()
	return index
}

// key reduces a dependency name, and the version it is on where the subject
// needs it, to the text two statements are matched on.
func (s DependencySubject) key(name, version string) string {
	definition, _ := s.definition()
	switch definition.key {
	case keyByPythonName:
		return normalizePythonName(name)
	case keyByImageVariant:
		return strings.ToLower(name) + "\x00" + tagVariant(version)
	case keyByName:
		return strings.ToLower(name)
	}
	return strings.ToLower(name)
}

// definition looks the subject up in the registry, returning its definition and
// its position. A subject outside the registry is treated as a named one, after
// every registered subject.
func (s DependencySubject) definition() (subjectDefinition, int) {
	for index, definition := range dependencySubjects {
		if definition.subject == s {
			return definition, index
		}
	}
	return subjectDefinition{subject: s, plural: string(s) + "s"}, len(dependencySubjects)
}

// normalizePythonName applies PEP 503: lower case, and every run of "-", "_"
// and "." collapsed into a single "-".
func normalizePythonName(name string) string {
	var normalized strings.Builder
	separator := false
	for _, r := range strings.ToLower(name) {
		if r == '-' || r == '_' || r == '.' {
			separator = true
			continue
		}
		if separator && normalized.Len() > 0 {
			normalized.WriteByte('-')
		}
		separator = false
		normalized.WriteRune(r)
	}
	return normalized.String()
}

// tagVariant returns what follows the numeric version at the start of an image
// tag: "-alpine" for "1.22-alpine", "" for "1.22". A tag that does not start
// with a version is its own variant.
func tagVariant(tag string) string {
	index := 0
	if len(tag) > 1 && (tag[0] == 'v' || tag[0] == 'V') && isASCIIDigit(tag[1]) {
		index = 1
	}

	start := index
	for index < len(tag) {
		switch {
		case isASCIIDigit(tag[index]):
			index++
		case tag[index] == '.' && index+1 < len(tag) && isASCIIDigit(tag[index+1]):
			index++
		default:
			return variantAfter(tag, start, index)
		}
	}
	return variantAfter(tag, start, index)
}

// variantAfter returns the part of tag after the version spanning start..end,
// or the whole tag when no version was found.
func variantAfter(tag string, start, end int) string {
	if end == start {
		return tag
	}
	return tag[end:]
}

// isASCIIDigit reports whether b is a decimal digit.
func isASCIIDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
