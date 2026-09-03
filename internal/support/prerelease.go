package support

import (
	"regexp"
	"strings"
)

// prereleaseQualifiers names the suffixes a published artifact uses to say it
// is not finished. It is the single source of truth for that vocabulary:
// prereleaseShape compiles it into the pattern IsPrereleaseVersion matches, and
// MavenVersionIgnore compiles it into the regex list passed to
// `-Dmaven.version.ignore`. Both are derived from this slice, so neither can
// drift from it by being edited on its own.
//
// "sp" and "final" are deliberately absent although Maven ranks them: both sort
// *above* the plain release and neither means unfinished.
//
//nolint:gochecknoglobals // read-only lookup table
var prereleaseQualifiers = []string{
	"alpha", "beta", "milestone", "cr", "rc", "preview", "pre", "dev", "snapshot",
}

// milestonePattern matches the `-M1` form, which is a convention rather than a
// word and so cannot be spelled in prereleaseQualifiers.
//
// The letter is matched only when a digit follows it. Alone it is an ordinary
// letter that appears inside real release names, and a bare `-M` suffix is not
// a convention any of these ecosystems uses.
const milestonePattern = `[Mm]\d+`

// prereleaseShape matches a version whose release segments are followed by a
// qualifier.
//
// The separator is optional and the qualifier may be followed by digits,
// because the ecosystems disagree about both: Maven writes `3.0.0-beta3`,
// `7.1.0-M1` and `5.5-beta2`, npm writes `1.0.0-rc.1`, and a bare `1.0.0beta`
// is legal in a `pom.xml`. Anchoring on the qualifier rather than on the
// punctuation is what lets one expression read all of them.
//
// gochecknoglobals whitelists a regexp.MustCompile result, which is why this
// carries no exemption and versionShape in version.go carries none either.
var prereleaseShape = regexp.MustCompile(
	`(?i)^[vV]?\d+(?:\.\d+)*[-_.]?(?:` +
		milestonePattern + `|` +
		strings.Join(prereleaseQualifiers, "|") +
		`)`,
)

// IsPrereleaseVersion reports whether a version names something its publisher
// has not finished: an alpha, a beta, a milestone, a release candidate or a
// snapshot.
//
// It has no production caller, and the doc below is not describing a filter
// that runs. It exists so the vocabulary above has one readable, directly
// testable definition, and so TestMavenVersionIgnore can check the Maven
// regexes against something other than a restatement of themselves. Neither
// updater consults it: the Go path needs no filter, because `go get -u` already
// declines pre-releases, and the Java path delegates the decision to Maven
// through MavenVersionIgnore.
//
// The distinction it captures is not semver precedence, which is the bug this
// file exists for. Semver already ranks `3.0.0-beta3` below `3.0.0` -- but it
// ranks it *above* `2.26.1`, so a comparison alone happily calls the beta an
// upgrade. Maven does not even have the concept: to it `3.0.0-beta3` is an
// ordinary release, which is why `maven-metadata.xml` reports it as `<release>`
// and why `versions:update-properties` selected it.
//
// A repository pinned to a stable line is asking for the newest *finished*
// release, and a pre-release is not a candidate for that at all, however it
// sorts.
func IsPrereleaseVersion(version string) bool {
	return prereleaseShape.MatchString(strings.TrimSpace(version))
}

// MavenVersionIgnore returns the value for `-Dmaven.version.ignore`, the
// comma-separated list of regular expressions versions-maven-plugin matches
// against every candidate version and discards on a hit.
//
// Maven has no notion of a pre-release. `3.0.0-beta3`, `7.1.0-M1`, `5.5-beta2`
// and `5.7-alpha1` are all ordinary releases to it, so `allowSnapshots=false`
// excludes none of them and `versions:use-latest-releases` treats every one as
// a candidate. This list is what supplies the missing concept.
//
// Each expression is anchored the way prereleaseShape is: the qualifier has to
// follow the numeric segments, not merely appear somewhere in the string. A
// leading `.*` would make these match any version *containing* the letters, and
// the short qualifiers are the ones that bite -- `.*cr.*` filters
// `1.0.0-incremental` and `2.0.0-macro`, `.*pre.*` filters `1.0.0-compressed`.
// Those are finished releases, and refusing them silently stops the updater
// doing its job, which is the failure this whole file is guarding against
// inverted. The trailing `.*` stays because the plugin full-matches.
//
// The value is embedded in the generated script inside single quotes, so it must
// never contain one -- TestMavenVersionIgnore asserts that.
func MavenVersionIgnore() string {
	patterns := make([]string, 0, len(prereleaseQualifiers)+1)
	patterns = append(patterns, mavenIgnorePattern(milestonePattern))

	for _, qualifier := range prereleaseQualifiers {
		patterns = append(patterns, mavenIgnorePattern(qualifier))
	}

	return strings.Join(patterns, ",")
}

// mavenIgnorePattern wraps one qualifier in the same shape prereleaseShape uses,
// so the two sides answer identically rather than only on the rows a table
// happens to list.
func mavenIgnorePattern(qualifier string) string {
	return `(?i)[vV]?\d+(\.\d+)*[-_.]?` + qualifier + `.*`
}
