package support

// SameMajor reports whether two versions sit on the same major line.
//
// It reuses parseVersion, so it accepts everything IsNewerVersion accepts and
// rejects everything it rejects: a value that is not shaped like a dotted
// numeric version has no major to compare, and answering "yes" for it would let
// a pin move on a comparison that never happened.
//
// A missing leading segment counts as zero, so "24" and "24.19.0" are the same
// major while "24" and "26" are not.
func SameMajor(current, candidate string) bool {
	currentRelease, _, currentOK := parseVersion(current)
	candidateRelease, _, candidateOK := parseVersion(candidate)
	if !currentOK || !candidateOK {
		return false
	}

	return majorOf(currentRelease) == majorOf(candidateRelease)
}

// majorOf returns the leading release segment, or zero when there is none.
func majorOf(release []int) int {
	if len(release) == 0 {
		return 0
	}

	return release[0]
}

// AcceptsUpgrade reports whether a run may move a pin from current to candidate.
//
// It is IsNewerVersion plus the major-version question, and exists so the two
// are asked together at every call site rather than each updater re-deriving
// the second one. "Newest" was always two questions; before this the second was
// answered per ecosystem, inconsistently -- `dockerfile` refused every major
// unconditionally, `terraform`, `pipeline`, `csharp` and `ruby` took them
// unconditionally, and none of them consulted the configuration that exists to
// decide it.
//
// allowMajorUpdates comes from entities.MajorUpdatesAllowed, which defaults to
// true: a dependency held back is a dependency whose CVEs are never remediated,
// and the pipeline the pull request runs is what catches a breaking major. Pass
// it through rather than defaulting it here, so a caller that forgets is caught
// by the compiler rather than silently getting the permissive answer.
func AcceptsUpgrade(current, candidate string, allowMajorUpdates bool) bool {
	if !IsNewerVersion(current, candidate) {
		return false
	}

	if allowMajorUpdates {
		return true
	}

	return SameMajor(current, candidate)
}
