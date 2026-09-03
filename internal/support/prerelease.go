package support

import (
	"regexp"
	"strings"
)

// prereleaseQualifiers names the suffixes a published artifact uses to say it
// is not finished. It is the single source of truth for that vocabulary: the
// Go side reads it through IsPrereleaseVersion, and the Maven side compiles it
// into the regex list passed to `-Dmaven.version.ignore`.
//
// Kept as one list because the two must agree. A qualifier recognised by Go and
// not by Maven would be filtered from a changelog entry while still being
// written into a `pom.xml`, which is a worse failure than not filtering at all:
// the pull request would claim an upgrade the file does not carry.
//
// "sp" and "final" are deliberately absent although Maven ranks them: both sort
// *above* the plain release and neither means unfinished.
var prereleaseQualifiers = []string{
	"alpha", "beta", "milestone", "cr", "rc", "preview", "pre", "dev", "snapshot",
}

// prereleaseShape matches a version whose release segments are followed by a
// qualifier from prereleaseQualifiers.
//
// The separator is optional and the qualifier may be followed by digits,
// because the ecosystems disagree about both: Maven writes `3.0.0-beta3`,
// `7.1.0-M1` and `5.5-beta2`, npm writes `1.0.0-rc.1`, and a bare `1.0.0beta`
// is legal in a `pom.xml`. Anchoring on the qualifier rather than on the
// punctuation is what lets one expression read all of them.
//
// `M` is matched only when followed by a digit. Alone it is an ordinary letter
// that appears inside real release names, and a bare `-M` suffix is not a
// convention any of these ecosystems uses.
var prereleaseShape = regexp.MustCompile(
	`(?i)^[vV]?\d+(?:\.\d+)*[-_.]?(?:` +
		`m\d|` +
		`(?:alpha|beta|milestone|cr|rc|preview|pre|dev|snapshot)` +
		`)`,
)

// IsPrereleaseVersion reports whether a version names something its publisher
// has not finished: an alpha, a beta, a milestone, a release candidate or a
// snapshot.
//
// This is not the same question as semver precedence, and the difference is the
// bug it exists for. Semver already ranks `3.0.0-beta3` below `3.0.0` — but it
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
// The expressions are matched against the whole version by the plugin, so each
// carries its own `.*` rather than relying on a partial match.
func MavenVersionIgnore() string {
	patterns := make([]string, 0, len(prereleaseQualifiers)+1)

	// Milestones are `-M1`, not `-milestone1`, and the bare letter has to stay
	// glued to a digit for the reason prereleaseShape gives.
	patterns = append(patterns, `.*[-_.]?[Mm]\d+.*`)

	for _, qualifier := range prereleaseQualifiers {
		patterns = append(patterns, `(?i).*`+qualifier+`.*`)
	}

	return strings.Join(patterns, ",")
}
