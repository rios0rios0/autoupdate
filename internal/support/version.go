package support

import (
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

// versionShape matches a version made of dotted numeric segments, with an
// optional leading "v", an optional pre-release identifier and optional build
// metadata, each captured separately.
//
// The number of segments is deliberately open: a `.ruby-version` holds three,
// a `.nvmrc` may hold one ("24"), a CI pin two ("3.13"), and a JRuby release
// four. Anything that is not shaped like that — "lts/*", "system", "stable",
// "jruby-9.4.0.0", "3.13t" — fails to match on purpose. Those are deliberate
// choices by the repository being updated, and a released version number is not
// a comparable replacement for them.
var versionShape = regexp.MustCompile(`^[vV]?(\d+(?:\.\d+)*)(?:-([^+]+))?(?:\+(.+))?$`)

// IsNewerVersion reports whether candidate names a strictly newer release than
// current.
//
// Every version pin autoupdate rewrites goes through this function, because
// "latest" is not the same thing as "newer". Release feeds answer a narrower
// question than the one a pinned repository is asking: nodejs.org/dist reports
// the newest *LTS* line, the Java feed the newest *LTS* JDK, the Python and
// Ruby feeds the newest *stable* series. A repository that has deliberately
// moved ahead of that line — Node 26 while LTS is 24, a JDK 25 early-adopter
// build while LTS is 21 — is *ahead* of "latest", and rewriting its pin with
// the fetched version silently downgrades the toolchain the project builds
// with, inside a pull request whose title claims an upgrade.
//
// Unparseable input on either side returns false: with nothing to compare, the
// only safe answer is to leave the pin alone.
func IsNewerVersion(current, candidate string) bool {
	currentRelease, currentPre, currentOK := parseVersion(current)
	candidateRelease, candidatePre, candidateOK := parseVersion(candidate)
	if !currentOK || !candidateOK {
		return false
	}

	if cmp := compareRelease(candidateRelease, currentRelease); cmp != 0 {
		return cmp > 0
	}

	return comparePrerelease(candidatePre, currentPre) > 0
}

// parseVersion splits a version into its numeric release segments and the
// pre-release identifier that follows them, reporting whether the string is
// shaped like a version at all.
//
// Build metadata is matched so that a version carrying it is still recognised,
// and then discarded: semver excludes everything after a "+" from precedence,
// so "1.0.0+build.1" and "1.0.0" name the same release and neither is an
// upgrade over the other. Reading it as a pre-release instead would get that
// wrong in both directions — it would rewrite "1.0.0+build.1" to "1.0.0" as
// though a pre-release were being promoted, and it would refuse the real
// upgrade from "1.0.0-rc.1" to "1.0.0+build.1".
func parseVersion(version string) ([]int, string, bool) {
	match := versionShape.FindStringSubmatch(strings.TrimSpace(version))
	if match == nil {
		return nil, "", false
	}

	parts := strings.Split(match[1], ".")
	segments := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			// Unreachable through versionShape, which only admits digits, but a
			// segment too large for an int would land here rather than panic.
			return nil, "", false
		}
		segments = append(segments, value)
	}

	return segments, match[2], true
}

// compareRelease orders two release segment lists, padding the shorter one with
// zeros so that "24" and "24.0.0" compare equal and "3.13" sorts below
// "3.13.2".
func compareRelease(candidate, current []int) int {
	longest := max(len(candidate), len(current))
	for i := range longest {
		candidateSegment, currentSegment := 0, 0
		if i < len(candidate) {
			candidateSegment = candidate[i]
		}
		if i < len(current) {
			currentSegment = current[i]
		}

		if candidateSegment != currentSegment {
			if candidateSegment > currentSegment {
				return 1
			}
			return -1
		}
	}

	return 0
}

// comparePrerelease orders the identifier trailing two otherwise equal
// releases, following semver precedence: a final release outranks any
// pre-release of the same version, and two pre-releases are ordered by the
// identifier rules semver defines.
//
// Build metadata never reaches here: parseVersion drops it, so two versions
// differing only after a "+" compare equal and the pin is left alone.
func comparePrerelease(candidate, current string) int {
	if candidate == current {
		return 0
	}
	if candidate == "" {
		return 1
	}
	if current == "" {
		return -1
	}

	return semver.Compare("v0.0.0-"+candidate, "v0.0.0-"+current)
}

// VersionGuardScript is the bash fragment defining the shell counterpart of
// IsNewerVersion, for the ecosystems whose pin is rewritten inside the
// generated upgrade script rather than by Go.
//
// The check has to exist on both sides. Go decides the branch name, the commit
// message, the changelog entry and the pull request title from its own answer;
// the script decides whether the file on disk is actually rewritten, and it
// re-reads the pin from the clone rather than trusting what Go saw. Guarding
// only one of the two would either downgrade the file anyway or announce an
// upgrade that never happened.
//
// It defines two functions:
//
//   - autoupdate_version_is_newer, the direct counterpart of IsNewerVersion,
//     which guards the rewrite of a version pin file;
//   - autoupdate_image_tag_is_older, which answers the same question for a
//     Dockerfile, where one file may pin an image several times and the sed
//     that rewrites them cannot compare anything itself.
func VersionGuardScript() string {
	return `# autoupdate_version_is_newer <candidate> <current>
# Succeeds only when <candidate> names a strictly newer release than <current>,
# so that a repository pinned ahead of the fetched "latest" is never rewritten
# backwards. Anything that is not a dotted numeric version — "lts/*", "system",
# "jruby-9.4.0.0" — fails the check and leaves the pin untouched.
autoupdate_version_is_newer() {
    [ -n "$1" ] && [ -n "$2" ] || return 1
    awk -v cand="$1" -v cur="$2" '
        function release(v) { sub(/^[vV]/, "", v); sub(/[-+].*$/, "", v); return v }
        function prerelease(v,   i) {
            sub(/^[vV]/, "", v)
            sub(/\+.*$/, "", v)
            i = index(v, "-")
            if (i == 0) return ""
            return substr(v, i + 1)
        }
        function numeric(v) { return v ~ /^[0-9]+(\.[0-9]+)*$/ }
        # Semver pre-release precedence: identifiers are compared left to right,
        # numeric ones numerically and the rest as ASCII, a numeric identifier
        # always ranks below an alphanumeric one, and when everything so far is
        # equal the longer set wins. A plain string comparison gets the numeric
        # cases wrong -- "rc.10" sorts below "rc.9" -- which made the script
        # refuse rewrites the Go side had already named the branch and the pull
        # request after.
        function compare_prerelease(a, b,   an, bn, ai, bi, i, n, x, y, xnum, ynum) {
            an = split(a, ai, ".")
            bn = split(b, bi, ".")
            n = (an > bn) ? an : bn
            for (i = 1; i <= n; i++) {
                if (i > an) return -1
                if (i > bn) return 1
                x = ai[i]; y = bi[i]
                xnum = (x ~ /^[0-9]+$/); ynum = (y ~ /^[0-9]+$/)
                if (xnum && ynum) {
                    if (x + 0 > y + 0) return 1
                    if (x + 0 < y + 0) return -1
                } else if (xnum != ynum) {
                    return ynum ? 1 : -1
                } else {
                    if (x > y) return 1
                    if (x < y) return -1
                }
            }
            return 0
        }
        BEGIN {
            candidate_release = release(cand)
            current_release = release(cur)
            if (!numeric(candidate_release) || !numeric(current_release)) exit 1

            candidate_count = split(candidate_release, candidate_parts, ".")
            current_count = split(current_release, current_parts, ".")
            longest = (candidate_count > current_count) ? candidate_count : current_count
            for (i = 1; i <= longest; i++) {
                candidate_segment = (i <= candidate_count) ? candidate_parts[i] + 0 : 0
                current_segment = (i <= current_count) ? current_parts[i] + 0 : 0
                if (candidate_segment > current_segment) exit 0
                if (candidate_segment < current_segment) exit 1
            }

            # Equal releases: a final release outranks any pre-release, and
            # build metadata is excluded from precedence entirely.
            candidate_pre = prerelease(cand)
            current_pre = prerelease(cur)
            if (candidate_pre == "" && current_pre != "") exit 0
            if (candidate_pre != "" && current_pre == "") exit 1
            if (compare_prerelease(candidate_pre, current_pre) > 0) exit 0
            exit 1
        }
    '
}

# autoupdate_image_tag_is_older <file> <image> <version> [tag-pattern]
# Succeeds when <file> pins <image> at a numeric tag and <version> is strictly
# newer than every tag pinned for that image there. A Dockerfile that already
# runs on a newer base image than the one being rolled out keeps it, instead of
# being rewritten backwards by a substitution that compares nothing.
#
# Lines pinning a digest are excluded before anything is compared, because the
# substitution this guards excludes them too: counting a tag the rewrite will
# not touch would answer for a line that never moves.
autoupdate_image_tag_is_older() {
    local file="$1" image="$2" version="$3" pattern="${4:-[0-9][0-9.]*}"
    local highest="" tag
    local tags
    tags="$(grep -v "` + digestPinMarker + `" "$file" 2>/dev/null \
        | grep -o "${image}:${pattern}" \
        | sed "s|^${image}:||" | sort -u || true)"
    for tag in $tags; do
        if [ -z "$highest" ] || autoupdate_version_is_newer "$tag" "$highest"; then
            highest="$tag"
        fi
    done
    [ -n "$highest" ] || return 1
    autoupdate_version_is_newer "$version" "$highest"
}

`
}
